// --- 用户管理相关 ---
let userListState = [];
let selectedUserId = '';
let userEditorMode = 'empty';
let userEditorFeedbackTimer = null;
let userCameraOptions = [];

function isBootstrapUserSetup() {
    return authState.auth_enabled === false && userListState.length === 0;
}

async function openUsers() {
    if (!canAdmin()) return;
    if (typeof confirmLeaveConfigIfDirty === 'function' && confirmLeaveConfigIfDirty(async () => {
        showUserPage();
        await loadUsers();
    })) return;
    showUserPage();
    await loadUsers();
}

function closeUsers() {
    if (window.CamKeepMobile?.isMobileMode?.() && document.documentElement.dataset.mobileUserView === 'detail') {
        document.documentElement.dataset.mobileUserView = 'list';
        window.CamKeepMobile?.syncPageState?.();
        return;
    }
    showDashboardPage();
}

function showUserPage() {
    document.getElementById('dashboardPage')?.classList.add('hidden');
    document.getElementById('configPage')?.classList.add('hidden');
    document.getElementById('userPage')?.classList.remove('hidden');
    configPageVisible = false;

    const configNavBtn = document.getElementById('configNavBtn');
    if (configNavBtn) {
        configNavBtn.classList.remove('bg-blue-50', 'text-blue-700');
        configNavBtn.removeAttribute('aria-current');
    }
    const userNavBtn = document.getElementById('userNavBtn');
    if (userNavBtn) {
        userNavBtn.classList.add('bg-blue-50', 'text-blue-700');
        userNavBtn.setAttribute('aria-current', 'page');
    }
    document.documentElement.dataset.mobilePage = 'users';
    document.documentElement.dataset.mobileSubpage = 'users';
    document.documentElement.dataset.mobileUserView = 'list';
    window.CamKeepMobile?.syncPageState?.();
    window.dispatchEvent(new CustomEvent('camkeep:pagechange', {detail: {page: 'users'}}));
    renderUserLoadingState();
    window.scrollTo({top: 0, behavior: 'smooth'});
}

function renderUserLoadingState() {
    const list = document.getElementById('userList');
    const summary = document.getElementById('userListSummary');
    const editor = document.getElementById('userEditor');
    if (summary) summary.textContent = '正在读取用户...';
    if (list) list.innerHTML = '<div class="user-list-loading">正在读取用户...</div>';
    if (editor) editor.innerHTML = renderUserEmptyState();
}

async function loadUsers(options = {}) {
    if (!canAdmin()) return;
    try {
        const resp = await fetch('/api/users');
        if (!resp.ok) {
            const err = await resp.json().catch(() => ({}));
            throw new Error(err.error || '无法读取用户列表');
        }
        const payload = await resp.json();
        userListState = payload.users || [];
        userCameraOptions = payload.camera_options || [];
        renderUserList();
        const preferred = options.selectId || selectedUserId;
        if (preferred && userListState.some(user => user.id === preferred)) {
            selectUser(preferred);
        } else if (userEditorMode === 'create') {
            renderCreateUserForm();
        } else if (userListState.length > 0) {
            selectUser(userListState[0].id);
        } else if (isBootstrapUserSetup()) {
            selectedUserId = '';
            userEditorMode = 'create';
            renderCreateUserForm();
        } else {
            selectedUserId = '';
            userEditorMode = 'empty';
            renderUserEditor();
        }
    } catch (e) {
        const list = document.getElementById('userList');
        const summary = document.getElementById('userListSummary');
        if (summary) summary.textContent = '读取失败';
        if (list) list.innerHTML = `<div class="user-list-error">${escapeHtml(e.message)}</div>`;
    }
}

function renderUserList() {
    const list = document.getElementById('userList');
    const summary = document.getElementById('userListSummary');
    if (summary) {
        const enabledCount = userListState.filter(user => user.enabled).length;
        summary.textContent = `${userListState.length} 个账号，${enabledCount} 个启用`;
    }
    if (!list) return;
    if (userListState.length === 0) {
        const emptyText = authState.auth_enabled === false
            ? '暂无本地用户；创建第一个管理员后会启用登录保护。'
            : '暂无本地用户。';
        list.innerHTML = `<div class="user-list-empty">${emptyText}</div>`;
        return;
    }
    list.innerHTML = userListState.map(user => renderUserListItem(user)).join('');
}

function renderUserListItem(user) {
    const active = user.id === selectedUserId && userEditorMode !== 'create';
    const loginText = renderUserLoginMeta(user, true);
    return `
        <button class="user-list-item ${active ? 'is-active' : ''}" onclick="selectUser('${escapeHtml(user.id)}')" type="button">
            <span class="user-list-avatar">${escapeHtml(userInitials(user))}</span>
            <span class="user-list-copy">
                <strong>${escapeHtml(user.display_name || user.username)}</strong>
                <em>${escapeHtml(user.username)}</em>
            </span>
            <span class="user-list-badges">
                <span class="user-role-badge user-role-badge--${escapeHtml(user.role)}">${userRoleLabel(user.role)}</span>
                <span class="user-camera-scope-badge">${escapeHtml(userCameraScopeLabel(user))}</span>
                <span class="user-status-badge ${user.enabled ? 'is-enabled' : 'is-disabled'}">${user.enabled ? '启用' : '停用'}</span>
                <span class="user-session-badge ${user.online ? 'is-online' : 'is-offline'}">${user.online ? '在线' : '离线'}</span>
                ${user.is_current ? '<span class="user-current-badge">当前登录</span>' : ''}
            </span>
            ${loginText ? `<span class="user-list-login-meta">${escapeHtml(loginText)}</span>` : ''}
        </button>
    `;
}

function selectUser(id) {
    selectedUserId = id;
    userEditorMode = 'edit';
    clearUserEditorFeedbackTimer();
    renderUserList();
    renderUserEditor();
    if (window.CamKeepMobile?.isMobileMode?.()) {
        document.documentElement.dataset.mobileUserView = 'detail';
        window.CamKeepMobile?.syncPageState?.();
        window.scrollTo({top: 0, behavior: 'smooth'});
    }
}

function showCreateUserForm() {
    if (!canAdmin()) return;
    selectedUserId = '';
    userEditorMode = 'create';
    clearUserEditorFeedbackTimer();
    renderUserList();
    renderCreateUserForm();
    if (window.CamKeepMobile?.isMobileMode?.()) {
        document.documentElement.dataset.mobileUserView = 'detail';
        window.CamKeepMobile?.syncPageState?.();
        window.scrollTo({top: 0, behavior: 'smooth'});
    }
}

function selectedUser() {
    return userListState.find(user => user.id === selectedUserId) || null;
}

function renderUserEditor() {
    const editor = document.getElementById('userEditor');
    if (!editor) return;
    const user = selectedUser();
    if (!user) {
        editor.innerHTML = renderUserEmptyState();
        return;
    }
    const editable = user.editable === true;
    const passwordManaged = user.password_managed === true;
    editor.innerHTML = `
        <div class="user-editor-head">
            <div>
                <div class="user-editor-kicker">LOCAL USER</div>
                <h3>${escapeHtml(user.display_name || user.username)}</h3>
                <p>${renderUserDetailMeta(user)}</p>
            </div>
            <span class="user-source-badge">本地用户</span>
        </div>
        <div class="user-form-grid">
            ${userTextField('显示名', 'display_name', user.display_name || '', !editable, '用于界面展示')}
            ${userTextField('用户名', 'username', user.username || '', true, '创建后不可修改')}
            ${userRoleField(user.role, !editable)}
            ${userEnabledField(user.enabled, !editable)}
        </div>
        ${userCameraScopePanel(user.role, user.camera_access_all, user.camera_ids || [], !editable)}
        ${editable ? `
            <div class="user-editor-actions">
                <button id="saveSelectedUserBtn" onclick="saveSelectedUser()" class="config-page-primary-btn config-page-primary-btn--compact" type="button">保存修改</button>
                ${user.can_delete ? '<button onclick="deleteSelectedUser()" class="user-danger-btn" type="button">删除用户</button>' : ''}
                <span id="userEditorFeedback" class="user-editor-feedback hidden" role="status" aria-live="polite"></span>
            </div>
        ` : `
            <div class="user-editor-note">该账号不能在页面中修改或删除。</div>
        `}
        ${passwordManaged ? `
            <div class="user-password-panel">
                <div>
                    <h4>重置密码</h4>
                    <p>更新后该用户已有会话会失效。</p>
                </div>
                <div class="user-form-grid">
                    ${userPasswordField('新密码', 'password')}
                    ${userPasswordField('确认密码', 'password_confirm')}
                </div>
                <button onclick="resetSelectedUserPassword()" class="config-page-secondary-btn config-page-secondary-btn--compact" type="button">更新密码</button>
            </div>
        ` : ''}
    `;
}

function renderCreateUserForm() {
    const editor = document.getElementById('userEditor');
    if (!editor) return;
    const bootstrap = isBootstrapUserSetup();
    editor.innerHTML = `
        <div class="user-editor-head">
            <div>
                <div class="user-editor-kicker">${bootstrap ? 'BOOTSTRAP ADMIN' : 'NEW USER'}</div>
                <h3>${bootstrap ? '创建管理员账号' : '新增用户'}</h3>
                <p>${bootstrap ? '第一个本地账号固定为 admin 管理员，创建后会启用登录保护。' : '创建一个可登录 CamKeep 的本地账号。'}</p>
            </div>
        </div>
        <div class="user-form-grid">
            ${userTextField('用户名', 'username', bootstrap ? 'admin' : '', bootstrap, '3-32 位字母、数字、点、下划线或短横线')}
            ${userTextField('显示名', 'display_name', '', false, '选填')}
            ${userRoleField(bootstrap ? 'admin' : 'viewer', bootstrap)}
            ${userEnabledField(true, bootstrap)}
            ${userPasswordField('密码', 'password')}
            ${userPasswordField('确认密码', 'password_confirm')}
        </div>
        ${bootstrap ? '' : userCameraScopePanel('viewer', true, [], false)}
        <div class="user-editor-actions">
            <button onclick="createUser()" class="config-page-primary-btn config-page-primary-btn--compact" type="button">${bootstrap ? '创建管理员' : '创建用户'}</button>
            <button onclick="renderUserEditor()" class="config-page-secondary-btn config-page-secondary-btn--compact" type="button">取消</button>
        </div>
    `;
}

function renderUserEmptyState() {
    return `
        <div class="user-editor-empty">
            <div class="user-editor-empty-icon">
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 12a4 4 0 100-8 4 4 0 000 8z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 21a8 8 0 0116 0"></path></svg>
            </div>
            <h3>选择一个用户</h3>
            <p>查看账号状态、调整角色或重置密码。</p>
        </div>
    `;
}

function userTextField(label, field, value, disabled, placeholder = '') {
    return `
        <label class="user-form-field">
            <span>${label}</span>
            <input data-user-field="${field}" type="text" value="${escapeHtml(value || '')}" placeholder="${escapeHtml(placeholder)}" ${disabled ? 'disabled' : ''}>
        </label>
    `;
}

function userPasswordField(label, field) {
    return `
        <label class="user-form-field">
            <span>${label}</span>
            <input data-user-field="${field}" type="password" autocomplete="new-password" placeholder="至少 8 位">
        </label>
    `;
}

function userRoleField(value, disabled) {
    return `
        <label class="user-form-field">
            <span>角色</span>
            <select data-user-field="role" onchange="syncUserCameraScopeVisibility()" ${disabled ? 'disabled' : ''}>
                <option value="viewer" ${value === 'viewer' ? 'selected' : ''}>只读用户</option>
                <option value="admin" ${value === 'admin' ? 'selected' : ''}>管理员</option>
            </select>
        </label>
    `;
}

function userCameraScopePanel(role, accessAll, cameraIds, disabled) {
    const isViewer = role !== 'admin';
    const selected = new Set(cameraIds || []);
    const allChecked = accessAll !== false;
    const optionMarkup = userCameraOptions.length > 0
        ? userCameraOptions.map(option => {
            const id = option.id || option;
            const optionChecked = allChecked || selected.has(id);
            return `
                <label class="user-camera-option">
                    <input data-user-camera-id="${escapeHtml(id)}" type="checkbox" ${optionChecked ? 'checked' : ''} ${disabled || allChecked ? 'disabled' : ''}>
                    <span>${escapeHtml(id)}</span>
                </label>
            `;
        }).join('')
        : '<div class="user-camera-empty">当前配置里没有可分配的摄像头。</div>';

    return `
        <div class="user-camera-scope-panel ${isViewer ? '' : 'hidden'}" data-user-camera-scope>
            <div class="user-camera-scope-head">
                <strong>可访问摄像头</strong>
                <label class="user-camera-all-toggle">
                    <input data-user-camera-all type="checkbox" onchange="syncUserCameraScopeVisibility()" ${allChecked ? 'checked' : ''} ${disabled ? 'disabled' : ''}>
                    <span>全部摄像头</span>
                </label>
            </div>
            <div class="user-camera-option-list">
                ${optionMarkup}
            </div>
        </div>
    `;
}

function syncUserCameraScopeVisibility() {
    const role = readUserField('role') || 'viewer';
    const panel = document.querySelector('[data-user-camera-scope]');
    if (!panel) return;
    const isViewer = role !== 'admin';
    panel.classList.toggle('hidden', !isViewer);

    const allToggle = panel.querySelector('[data-user-camera-all]');
    const accessAll = allToggle ? allToggle.checked : true;
    panel.querySelectorAll('[data-user-camera-id]').forEach(input => {
        if (accessAll) {
            input.checked = true;
        }
        input.disabled = !isViewer || accessAll || Boolean(allToggle?.disabled);
    });
}

function userEnabledField(checked, disabled) {
    return `
        <label class="user-form-field user-form-field--switch">
            <span>账号状态</span>
            <span class="config-toggle-control">
                <span class="text-xs font-extrabold text-slate-700">启用</span>
                <span class="relative inline-flex h-5 w-9 shrink-0 items-center">
                    <input data-user-field="enabled" type="checkbox" ${checked ? 'checked' : ''} ${disabled ? 'disabled' : ''} class="peer sr-only">
                    <span class="config-toggle-track absolute inset-0 rounded-full transition-colors peer-disabled:opacity-50"></span>
                    <span class="config-toggle-thumb absolute left-0.5 h-4 w-4 rounded-full shadow transition-transform peer-checked:translate-x-4"></span>
                </span>
            </span>
        </label>
    `;
}

function readUserField(field) {
    return document.querySelector(`[data-user-field="${field}"]`)?.value || '';
}

function readUserChecked(field) {
    return Boolean(document.querySelector(`[data-user-field="${field}"]`)?.checked);
}

function readUserCameraScope(role) {
    if (role === 'admin') {
        return {camera_access_all: true, camera_ids: []};
    }
    const allToggle = document.querySelector('[data-user-camera-all]');
    const accessAll = allToggle ? allToggle.checked : true;
    if (accessAll) {
        return {camera_access_all: true, camera_ids: []};
    }
    const cameraIds = Array.from(document.querySelectorAll('[data-user-camera-id]:checked'))
        .map(input => input.dataset.userCameraId)
        .filter(Boolean);
    return {camera_access_all: false, camera_ids: cameraIds};
}

function clearUserEditorFeedbackTimer() {
    if (!userEditorFeedbackTimer) return;
    clearTimeout(userEditorFeedbackTimer);
    userEditorFeedbackTimer = null;
}

function setUserEditorFeedback(message, type = 'info') {
    clearUserEditorFeedbackTimer();
    const feedback = document.getElementById('userEditorFeedback');
    if (!feedback) return;

    feedback.textContent = message || '';
    feedback.className = `user-editor-feedback ${message ? `is-${type}` : 'hidden'}`;
    if (message && type !== 'info') {
        userEditorFeedbackTimer = setTimeout(() => {
            feedback.textContent = '';
            feedback.className = 'user-editor-feedback hidden';
            userEditorFeedbackTimer = null;
        }, 2200);
    }
}

async function createUser() {
    const bootstrap = isBootstrapUserSetup();
    const password = readUserField('password');
    const confirm = readUserField('password_confirm');
    if (password !== confirm) {
        alert('两次输入的密码不一致');
        return;
    }
    const role = bootstrap ? 'admin' : (readUserField('role') || 'viewer');
    const payload = {
        username: bootstrap ? 'admin' : readUserField('username').trim(),
        display_name: bootstrap ? (readUserField('display_name').trim() || 'admin') : readUserField('display_name').trim(),
        role,
        password,
        enabled: bootstrap ? true : readUserChecked('enabled'),
        ...readUserCameraScope(role)
    };
    const created = await sendUserRequest('/api/users', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(payload)
    });
    if (created) {
        if (bootstrap) {
            window.location.reload();
            return;
        }
        userEditorMode = 'edit';
        selectedUserId = created.id;
        await loadUsers({selectId: created.id});
    }
}

async function saveSelectedUser() {
    const user = selectedUser();
    if (!user || user.editable !== true) return;
    const button = document.getElementById('saveSelectedUserBtn');
    if (button?.disabled) return;

    if (button) {
        button.disabled = true;
        button.textContent = '保存中...';
    }
    setUserEditorFeedback('正在保存...', 'info');

    const role = readUserField('role') || 'viewer';
    const updated = await sendUserRequest(`/api/users/${encodeURIComponent(user.id)}`, {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
            display_name: readUserField('display_name').trim(),
            role,
            enabled: readUserChecked('enabled'),
            ...readUserCameraScope(role)
        })
    });
    if (updated) {
        await loadUsers({selectId: updated.id});
        const currentButton = document.getElementById('saveSelectedUserBtn');
        if (currentButton) {
            currentButton.disabled = false;
            currentButton.textContent = '保存修改';
        }
        setUserEditorFeedback('已保存', 'success');
        return;
    }

    if (button) {
        button.disabled = false;
        button.textContent = '保存修改';
    }
    setUserEditorFeedback('保存失败', 'error');
}

async function resetSelectedUserPassword() {
    const user = selectedUser();
    if (!user || user.password_managed !== true) return;
    const password = readUserField('password');
    const confirm = readUserField('password_confirm');
    if (password !== confirm) {
        alert('两次输入的密码不一致');
        return;
    }
    const ok = await sendUserRequest(`/api/users/${encodeURIComponent(user.id)}/password`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({password})
    });
    if (ok) {
        alert('密码已更新');
        renderUserEditor();
    }
}

async function deleteSelectedUser() {
    const user = selectedUser();
    if (!user || user.can_delete !== true) return;
    if (!confirm(`确定删除用户 ${user.username}？该用户将无法登录，已有会话会失效。`)) return;
    const ok = await sendUserRequest(`/api/users/${encodeURIComponent(user.id)}`, {method: 'DELETE'});
    if (ok) {
        selectedUserId = '';
        await loadUsers();
    }
}

async function sendUserRequest(url, options) {
    try {
        const resp = await fetch(url, options);
        const payload = await resp.json().catch(() => ({}));
        if (!resp.ok) {
            alert(payload.error || '用户操作失败');
            return null;
        }
        return payload;
    } catch (e) {
        alert('网络请求失败: ' + e.message);
        return null;
    }
}

function userRoleLabel(role) {
    return role === 'admin' ? '管理员' : '只读用户';
}

function userCameraScopeLabel(user) {
    if (user.role === 'admin' || user.camera_access_all !== false) return '全部摄像头';
    const count = Array.isArray(user.camera_ids) ? user.camera_ids.length : 0;
    if (count === 0) return '无摄像头';
    return `${count} 个摄像头`;
}

function userInitials(user) {
    const text = String(user.display_name || user.username || '?').trim();
    return (text[0] || '?').toUpperCase();
}

function renderUserLoginMeta(user, brief) {
    const parts = [];
    if (user.online) {
        const onlineText = user.active_sessions?.length > 1
            ? `在线 · ${user.active_sessions.length} 个会话`
            : '在线';
        parts.push(onlineText);
        const currentSession = user.active_sessions?.find(session => session.current) || user.active_sessions?.[0];
        if (currentSession?.ip) {
            parts.push(`IP ${currentSession.ip}`);
        }
    } else if (user.last_login_at) {
        const timeText = formatUserLoginTime(user.last_login_at);
        const ipText = user.last_login_ip ? `IP ${user.last_login_ip}` : '';
        parts.push(`最近登录 ${timeText}${ipText ? ` · ${ipText}` : ''}`);
    } else {
        parts.push('从未登录');
    }
    if (!brief && user.is_current) {
        parts.unshift('当前账号');
    }
    return parts.join(' · ');
}

function renderUserDetailMeta(user) {
    const parts = [
        user.username,
        userRoleLabel(user.role),
        userCameraScopeLabel(user),
        user.enabled ? '已启用' : '已停用'
    ];
    const loginMeta = renderUserLoginMeta(user, false);
    if (loginMeta) parts.push(loginMeta);
    return escapeHtml(parts.join(' · '));
}

function formatUserLoginTime(value) {
    const date = value ? new Date(value) : null;
    if (!date || Number.isNaN(date.getTime())) return '';
    return date.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit'
    });
}
