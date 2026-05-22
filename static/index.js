// === 播放矩阵状态管理 ===
let currentLayout = 1;
let activeCell = 0;
let dpInstances = new Array(6).fill(null);
let cellData = new Array(6).fill(null);
let currentSelectedCam = null;
let pendingAction = null;
let compactGrid = window.innerWidth < 640;
let selectedRecordRange = {start: '', end: ''};
let selectedRecordPath = '';
const maxRecordRangeDays = 7;
const recordTimelineFallbackDurationSeconds = 5 * 60;
const recordArchiveOpenDates = new Set();
const recordArchiveViewModes = new Map();
let latestBatchCameraPreview = {valid: [], invalid: []};
let activeRecordTimeline24hDockKey = '';
let matrixToolbarTimer = null;
let configPageVisible = false;
const cameraCoverObjectURLs = new Map();
const cameraCoverRequested = new Set();
const cameraCoverFailed = new Set();
const cameraCardRenderKeys = new Map();
const authState = window.CAMKEEP_AUTH || {
    auth_enabled: false,
    can_admin: true,
    user: {role: 'admin', username: 'admin'},
    permissions: ['view', 'admin']
};

window.cameraCapabilityCache = window.cameraCapabilityCache || new Map();

function canAdmin() {
    return authState.can_admin === true || authState.user?.role === 'admin';
}

window.onload = function () {
    initThemeControls();
    initAccountMenu();
    initUpdateBadge();
    if (typeof DPlayer === 'undefined') {
        alert("播放器组件加载失败，请检查网络！");
        return;
    }
    initRecordRangeControls();
    renderRecordSelectionPrompt();
    setLayout(1);
    loadStatus();
    setInterval(loadStatus, 5000);
    window.addEventListener('pagehide', stopAllCellPlayback);
    window.addEventListener('pagehide', releaseCameraCoverObjectURLs);
    window.addEventListener('beforeunload', stopAllCellPlayback);
    window.addEventListener('resize', () => {
        const nextCompactGrid = window.innerWidth < 640;
        if (nextCompactGrid !== compactGrid) {
            compactGrid = nextCompactGrid;
            renderGrid();
        }
    });
};

function initAccountMenu() {
    const menu = document.getElementById('accountMenu');
    if (!menu) return;

    document.addEventListener('click', event => {
        if (!menu.contains(event.target)) {
            setAccountMenuOpen(false);
        }
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') {
            setAccountMenuOpen(false);
        }
    });
}

function toggleAccountMenu(event) {
    event?.stopPropagation();
    const panel = document.getElementById('accountMenuPanel');
    const open = panel ? panel.classList.contains('hidden') : false;
    setAccountMenuOpen(open);
}

function setAccountMenuOpen(open) {
    const button = document.getElementById('accountMenuButton');
    const panel = document.getElementById('accountMenuPanel');
    if (!button || !panel) return;

    panel.classList.toggle('hidden', !open);
    button.classList.toggle('is-open', open);
    button.setAttribute('aria-expanded', open ? 'true' : 'false');
}

function initThemeControls() {
    syncThemeToggleIcon();
    if (window.matchMedia) {
        const media = window.matchMedia('(prefers-color-scheme: dark)');
        media.addEventListener('change', () => {
            if (!localStorage.getItem('camkeep-theme')) {
                document.documentElement.classList.toggle('dark', media.matches);
                syncThemeToggleIcon();
            }
        });
    }
}

function toggleTheme() {
    const nextIsDark = !document.documentElement.classList.contains('dark');
    document.documentElement.classList.toggle('dark', nextIsDark);
    localStorage.setItem('camkeep-theme', nextIsDark ? 'dark' : 'light');
    syncThemeToggleIcon();
}

function syncThemeToggleIcon() {
    const isDark = document.documentElement.classList.contains('dark');
    const lightIcon = document.getElementById('themeIconLight');
    const darkIcon = document.getElementById('themeIconDark');
    const button = document.getElementById('themeToggleBtn');
    if (lightIcon) lightIcon.classList.toggle('hidden', !isDark);
    if (darkIcon) darkIcon.classList.toggle('hidden', isDark);
    if (button) button.title = isDark ? '切换到浅色模式' : '切换到深色模式';
    if (button) button.setAttribute('aria-label', button.title);
}

async function initUpdateBadge() {
    const badge = document.getElementById('updateBadge');
    if (!badge) return;
    if (!canAdmin()) return;

    try {
        const resp = await fetch('/api/update/check');
        if (!resp.ok) return;

        const data = await resp.json();
        renderUpdateBadge(data);
    } catch (e) {
        console.warn('检查更新失败:', e);
    }
}

function renderUpdateBadge(data) {
    const badge = document.getElementById('updateBadge');
    if (!badge || !data) return;

    const latest = String(data.latest_stable_version || '').trim();
    const releaseURL = String(data.release_url || 'https://github.com/r0n9/camkeep/releases/latest').trim();
    const channel = String(data.channel || '').trim().toLowerCase();

    badge.classList.add('hidden');
    badge.classList.remove('is-reference');
    badge.textContent = '';

    if (data.update_available && latest) {
        badge.href = releaseURL;
        badge.textContent = `有新版本 ${latest}`;
        badge.title = data.message || `发现新版本 ${latest}`;
        badge.classList.remove('hidden');
        return;
    }

    if ((channel === 'dev' || channel === 'test') && latest) {
        badge.href = releaseURL;
        badge.textContent = `最新稳定版 ${latest}`;
        badge.title = data.message || '当前为开发/测试版本，不参与稳定版更新判断';
        badge.classList.add('is-reference');
        badge.classList.remove('hidden');
    }
}

// --- 控制面板动作弹窗 ---
function confirmCamAction(camId, action) {
    if (!canAdmin()) return;
    pendingAction = {camId, action};
    const titleEl = document.getElementById('confirmTitle');
    const descEl = document.getElementById('confirmDesc');
    const btnEl = document.getElementById('confirmBtn');
    const iconEl = document.getElementById('confirmIcon');

    if (action === 'start') {
        titleEl.innerText = `强制开启录像 (CAM-${camId})`;
        descEl.innerHTML = `
            <p class="mb-2">此操作将<b>无视配置中的时间表</b>，立刻启用该摄像头的录制策略。</p>
            <p class="mb-2">系统仍会按照配置执行普通持续录制、动检录制或延时录像，直到您手动点击“停录”或恢复“计划”。</p>
            <p class="text-xs text-green-600 font-bold mt-3 border-t pt-2">⚡ 确认后稍后即生效</p>`;
        btnEl.className = "px-4 py-2 bg-green-600 hover:bg-green-700 text-white rounded-lg font-bold shadow transition-all";
        btnEl.innerText = "确认强录";
        iconEl.className = "w-10 h-10 rounded-full flex items-center justify-center mr-3 text-white bg-green-500 shadow-lg shadow-green-200";
        iconEl.innerHTML = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>`;
    } else if (action === 'stop') {
        titleEl.innerText = `强制停止录像 (CAM-${camId})`;
        descEl.innerHTML = `
            <p class="mb-2">此操作将<b>立即中断当前的录像任务</b>。</p>
            <p class="mb-2">摄像头将<b>一直保持不录像状态</b>（即使在计划时间内也不会录），直到您手动点击“强制录”或恢复“计划”。</p>
            <p class="text-xs text-red-500 font-bold mt-3 border-t pt-2">⚡ 确认后稍后即生效</p>`;
        btnEl.className = "px-4 py-2 bg-red-600 hover:bg-red-700 text-white rounded-lg font-bold shadow transition-all";
        btnEl.innerText = "确认停录";
        iconEl.className = "w-10 h-10 rounded-full flex items-center justify-center mr-3 text-white bg-red-500 shadow-lg shadow-red-200";
        iconEl.innerHTML = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 10a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1h-4a1 1 0 01-1-1v-4z"></path></svg>`;
    } else if (action === 'auto') {
        titleEl.innerText = `恢复自动计划 (CAM-${camId})`;
        descEl.innerHTML = `
            <p class="mb-2">解除强制状态，将摄像头的录像控制权交还给系统。</p>
            <p class="mb-2">系统将严格按照 conf.yaml 中的 <code>record_time</code> 时间表和该摄像头录制模式自动启停录像。</p>
            <p class="text-xs text-blue-500 font-bold mt-3 border-t pt-2">⚡ 确认后立即应用计划逻辑</p>`;
        btnEl.className = "px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg font-bold shadow transition-all";
        btnEl.innerText = "恢复计划";
        iconEl.className = "w-10 h-10 rounded-full flex items-center justify-center mr-3 text-white bg-blue-500 shadow-lg shadow-blue-200";
        iconEl.innerHTML = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"></path></svg>`;
    }
    document.getElementById('confirmModal').classList.remove('hidden');
}

function closeConfirm() {
    document.getElementById('confirmModal').classList.add('hidden');
    pendingAction = null;
}

async function executeConfirmAction() {
    if (!pendingAction) return;
    const {camId, action} = pendingAction;
    const btn = document.getElementById('confirmBtn');
    const originText = btn.innerText;
    btn.innerText = "执行中...";
    btn.disabled = true;

    await fetch(`/api/camera/${camId}/${action}`, {method: 'POST'});

    btn.innerText = originText;
    btn.disabled = false;
    closeConfirm();
    loadStatus();
}

// --- 系统配置相关 ---
const DEFAULT_RECORD_FORMAT = 'mp4';
let configEditMode = 'form';
let configFormState = {daily_merge: {enabled: false, time: '03:30'}, cameras: []};
let configFormInitialCameras = [];
let configFormInitialCamerasLoaded = false;
let go2rtcStreamInfoMap = new Map();
let configCameraExpandedKeys = new Set();
let configCameraUiSeq = 0;

async function openConfig() {
    if (!canAdmin()) return;
    showConfigPage();
    go2rtcStreamInfoMap = new Map();
    configCameraExpandedKeys = new Set();
    configFormInitialCameras = [];
    configFormInitialCamerasLoaded = false;
    renderConfigLoadingState();
    const yamlResp = await fetch('/api/config');
    const yamlText = await yamlResp.text();
    document.getElementById('configYaml').value = yamlText;

    try {
        const formResp = await fetch('/api/config/form');
        if (!formResp.ok) {
            const err = await formResp.json().catch(() => ({}));
            throw new Error(err.error || '无法读取表单配置');
        }
        configFormState = normalizeConfigForm(await formResp.json());
        configFormInitialCameras = cloneConfigCameraList(configFormState.cameras);
        configFormInitialCamerasLoaded = true;
        renderConfigForm();
        switchConfigMode('form', {skipSync: true});
    } catch (e) {
        switchConfigMode('yaml', {skipSync: true});
        alert('表单配置读取失败，已切换到 YAML 高级模式: ' + e.message);
    }

    void scanUnmanagedStreams();
}

function closeConfig() {
    showDashboardPage();
}

function showConfigPage() {
    if (!canAdmin()) return;
    configPageVisible = true;
    document.getElementById('dashboardPage')?.classList.add('hidden');
    document.getElementById('configPage')?.classList.remove('hidden');
    document.getElementById('userPage')?.classList.add('hidden');
    const navBtn = document.getElementById('configNavBtn');
    if (navBtn) {
        navBtn.classList.add('bg-blue-50', 'text-blue-700');
        navBtn.setAttribute('aria-current', 'page');
    }
    const userNavBtn = document.getElementById('userNavBtn');
    if (userNavBtn) {
        userNavBtn.classList.remove('bg-blue-50', 'text-blue-700');
        userNavBtn.removeAttribute('aria-current');
    }
    window.scrollTo({top: 0, behavior: 'smooth'});
}

function showDashboardPage() {
    configPageVisible = false;
    document.getElementById('configPage')?.classList.add('hidden');
    document.getElementById('userPage')?.classList.add('hidden');
    document.getElementById('dashboardPage')?.classList.remove('hidden');
    const navBtn = document.getElementById('configNavBtn');
    if (navBtn) {
        navBtn.classList.remove('bg-blue-50', 'text-blue-700');
        navBtn.removeAttribute('aria-current');
    }
    const userNavBtn = document.getElementById('userNavBtn');
    if (userNavBtn) {
        userNavBtn.classList.remove('bg-blue-50', 'text-blue-700');
        userNavBtn.removeAttribute('aria-current');
    }
    window.scrollTo({top: 0, behavior: 'smooth'});
}

function renderConfigLoadingState() {
    const list = document.getElementById('configCameraList');
    const empty = document.getElementById('configCameraEmpty');
    if (list) {
        list.innerHTML = '<div class="rounded-xl border border-dashed border-slate-200 bg-slate-50 px-4 py-8 text-center text-sm font-bold text-slate-400">正在读取配置...</div>';
    }
    if (empty) empty.classList.add('hidden');
    const restoreBtn = document.getElementById('restoreConfigCamerasBtn');
    if (restoreBtn) restoreBtn.disabled = true;
}

async function saveConfig() {
    if (!canAdmin()) return;
    if (configEditMode === 'form') {
        let payload;
        try {
            payload = collectConfigForm();
        } catch (e) {
            alert('配置表单检查失败: ' + e.message);
            return;
        }

        const resp = await fetch('/api/config/form', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(payload)
        });
        if (resp.ok) {
            const result = await resp.json().catch(() => ({}));
            if (result.yaml) document.getElementById('configYaml').value = result.yaml;
            alert('配置已生效并自动重启任务！');
            closeConfig();
            loadStatus();
        } else {
            const err = await resp.json().catch(() => ({}));
            alert('保存失败: ' + (err.error || '配置内容不合法'));
        }
        return;
    }

    const yamlText = document.getElementById('configYaml').value;
    const validation = await validateConfigYaml(yamlText);
    if (!validation.ok) {
        alert('配置格式检查失败: ' + validation.error);
        return;
    }
    const resp = await fetch('/api/config', {method: 'POST', body: yamlText});
    if (resp.ok) {
        alert('配置已生效并自动重启任务！');
        closeConfig();
        loadStatus();
    } else {
        const err = await resp.json();
        alert('保存失败: ' + err.error);
    }
}

async function switchConfigMode(mode, options = {}) {
    if (mode === 'form' && configEditMode !== 'form' && !options.skipSync) {
        const parsed = await parseConfigYamlToForm(document.getElementById('configYaml').value);
        if (!parsed.ok) {
            alert('YAML 无法转换为表单: ' + parsed.error);
            return;
        }
        configFormState = normalizeConfigForm(parsed.config);
        configCameraExpandedKeys = new Set();
        renderConfigForm();
    }
    if (mode === 'yaml' && configEditMode !== 'yaml' && !options.skipSync) {
        try {
            document.getElementById('configYaml').value = configToYaml(collectConfigForm());
        } catch (e) {
            alert('表单内容还不完整，已保留当前 YAML 内容: ' + e.message);
        }
    }

    configEditMode = mode;
    document.getElementById('configFormPanel').classList.toggle('hidden', mode !== 'form');
    document.getElementById('configYamlPanel').classList.toggle('hidden', mode !== 'yaml');
    document.getElementById('configFormTab').className = mode === 'form'
        ? 'config-tab-btn is-active'
        : 'config-tab-btn';
    document.getElementById('configYamlTab').className = mode === 'yaml'
        ? 'config-tab-btn is-active'
        : 'config-tab-btn';
}

async function validateConfigYaml(yamlText) {
    try {
        const resp = await fetch('/api/config/validate', {method: 'POST', body: yamlText});
        if (resp.ok) return {ok: true};

        const err = await resp.json().catch(() => ({}));
        return {ok: false, error: err.error || '配置内容不合法'};
    } catch (e) {
        return {ok: false, error: '无法连接配置检查接口: ' + e.message};
    }
}

async function scanUnmanagedStreams() {
    const listDiv = document.getElementById('unmanagedList');
    updateGo2rtcImportBadge(0);
    listDiv.innerHTML = '<span class="config-import-result-message">正在扫描 go2rtc 中可导入的摄像头流...</span>';
    listDiv.classList.remove('hidden');

    try {
        const resp = await fetch('/api/go2rtc/unmanaged');
        if (!resp.ok) {
            const err = await resp.json();
            throw new Error(err.error || '请求失败');
        }
        const scanPayload = await resp.json();
        const scan = normalizeGo2rtcScanPayload(scanPayload);
        go2rtcStreamInfoMap = scan.streams;
        rerenderConfigFormPreservingInput();

        if (!scan.unmanaged || scan.unmanaged.length === 0) {
            updateGo2rtcImportBadge(0);
            listDiv.innerHTML = '<span class="config-import-result-message config-import-result-message--ok">go2rtc 中的流都已经加入 CamKeep 配置，当前没有可导入项。</span>';
            return;
        }

        updateGo2rtcImportBadge(scan.unmanaged.length);
        listDiv.innerHTML = `
            <div class="config-import-result-head">
                <span class="config-import-result-count">${scan.unmanaged.length} 个可导入流</span>
            </div>
        `;
        scan.unmanaged.forEach(stream => {
            const streamID = encodeURIComponent(stream.id);
            const streamArg = escapeHtml(JSON.stringify(stream));
            const sourceLabel = stream.source_label && stream.source_label !== '未知'
                ? `<span class="config-import-source-type">${escapeHtml(stream.source_label)}</span>`
                : '<span class="config-import-source-type">go2rtc</span>';
            const tag = document.createElement('div');
            tag.id = `unmanaged-${streamID}`;
            tag.className = 'config-import-result-item';
            tag.innerHTML = `
                <div class="config-import-result-copy">
                    <span class="config-import-stream-id">${escapeHtml(stream.id)}</span>
                    ${sourceLabel}
                </div>
                <button onclick="appendStreamToConfig(${streamArg})" class="config-import-add-btn">导入</button>
            `;
            listDiv.appendChild(tag);
        });
    } catch (e) {
        updateGo2rtcImportBadge(0);
        listDiv.innerHTML = `<span class="config-import-result-message config-import-result-message--error">扫描失败: ${escapeHtml(e.message)}</span>`;
    }
}

function updateGo2rtcImportBadge(count) {
    const badge = document.getElementById('go2rtcImportBadge');
    const card = document.querySelector('.config-discovery-card');
    if (!badge) return;
    const safeCount = Number(count) || 0;
    badge.textContent = safeCount > 0 ? `${safeCount} 个可导入` : '';
    badge.classList.toggle('hidden', safeCount <= 0);
    card?.classList.toggle('has-importable-streams', safeCount > 0);
}

function normalizeGo2rtcScanPayload(payload) {
    if (Array.isArray(payload)) {
        const unmanaged = payload.map(stream => normalizeGo2rtcStreamInfo(stream)).filter(stream => stream.id);
        return {
            streams: new Map(unmanaged.map(stream => [stream.id, stream])),
            unmanaged
        };
    }

    const streams = new Map();
    const rawStreams = payload?.streams || {};
    Object.entries(rawStreams).forEach(([id, info]) => {
        const stream = normalizeGo2rtcStreamInfo(info, id);
        if (stream.id) streams.set(stream.id, stream);
    });

    const unmanaged = (payload?.unmanaged || []).map(stream => {
        const normalized = normalizeGo2rtcStreamInfo(stream);
        return streams.get(normalized.id) || normalized;
    }).filter(stream => stream.id);

    return {streams, unmanaged};
}

function normalizeGo2rtcStreamInfo(stream, fallbackID = '') {
    if (typeof stream === 'string') {
        return {
            id: stream,
            source_label: '',
            managed: false
        };
    }

    return {
        id: String(readConfigValue(stream, ['id', 'ID'], fallbackID) || ''),
        source_label: readConfigValue(stream, ['source_label', 'SourceLabel'], ''),
        managed: Boolean(readConfigValue(stream, ['managed', 'Managed'], false))
    };
}

function rerenderConfigFormPreservingInput() {
    if (configEditMode !== 'form') return;
    if (!configPageVisible) return;

    try {
        syncConfigCameraExpandedStateFromDom();
        configFormState = collectConfigForm({allowEmptyID: true});
    } catch (e) {
        // 表单刚初始化或正在切换模式时，保留现有状态即可。
    }
    renderConfigForm();
}

async function parseConfigYamlToForm(yamlText) {
    try {
        const resp = await fetch('/api/config/form/parse', {method: 'POST', body: yamlText});
        if (resp.ok) return {ok: true, config: await resp.json()};

        const err = await resp.json().catch(() => ({}));
        return {ok: false, error: err.error || '配置内容不合法'};
    } catch (e) {
        return {ok: false, error: '无法连接配置转换接口: ' + e.message};
    }
}

function normalizeConfigForm(cfg) {
    const cameras = (readConfigValue(cfg, ['cameras', 'Cameras'], []) || [])
        .map((cam, index) => ({cam: normalizeConfigCamera(cam), index}))
        .sort((left, right) => {
            const orderDelta = (Number(left.cam.order) || 0) - (Number(right.cam.order) || 0);
            if (orderDelta !== 0) return orderDelta;
            return left.index - right.index;
        })
        .map(item => item.cam);

    return {
        daily_merge: {
            enabled: Boolean(readConfigValue(cfg.daily_merge, ['enabled', 'Enabled'], false)),
            time: readConfigValue(cfg.daily_merge, ['time', 'Time'], '03:30') || '03:30'
        },
        cameras
    };
}

function normalizeConfigCamera(cam) {
    const streamURL = readConfigValue(cam, ['stream_url', 'StreamURL'], '');
    const legacyRTSPURL = readConfigValue(cam, ['rtsp_url', 'RTSPUrl'], '');
    const effectiveStreamURL = String(streamURL || '').trim() !== '' ? streamURL : legacyRTSPURL;
    const segmentDuration = readConfigNumber(cam, ['segment_duration', 'SegmentDuration'], 600);
    const minSizeKb = readConfigNumber(cam, ['min_size_kb', 'MinSizeKb'], 1024);
    const captureInterval = readConfigNumber(cam, ['capture_interval', 'CaptureInterval'], 1);
    const motionRatio = readConfigNumber(cam, ['motionDetectRatioThreshold', 'MotionDetectRatioThreshold'], 0.01);

    return {
        id: readConfigValue(cam, ['id', 'ID'], ''),
        order: readConfigNumber(cam, ['order', 'Order'], 0),
        stream_url: effectiveStreamURL,
        motion_url: readConfigValue(cam, ['motion_url', 'MotionURL'], ''),
        retention_days: readConfigNumber(cam, ['retention_days', 'RetentionDays'], 7),
        segment_duration: segmentDuration > 0 ? segmentDuration : 600,
        format: readConfigValue(cam, ['format', 'Format'], DEFAULT_RECORD_FORMAT) || DEFAULT_RECORD_FORMAT,
        min_size_kb: minSizeKb > 0 ? minSizeKb : 1024,
        record_time: readConfigValue(cam, ['record_time', 'RecordTime'], '00:00-23:59') || '00:00-23:59',
        mode: readConfigValue(cam, ['mode', 'Mode'], 'normal') || 'normal',
        capture_interval: captureInterval > 0 ? captureInterval : 1,
        motion_detect: Boolean(readConfigValue(cam, ['motion_detect', 'MotionDetect'], false)),
        motionDetectRatioThreshold: motionRatio > 0 ? motionRatio : 0.01,
        auto_discovered: isManagedByGo2rtcURL(effectiveStreamURL) || Boolean(readConfigValue(cam, ['auto_discovered', 'AutoDiscovered'], false))
    };
}

function readConfigValue(source, keys, fallback) {
    if (!source) return fallback;
    for (const key of keys) {
        if (Object.prototype.hasOwnProperty.call(source, key) && source[key] !== null && source[key] !== undefined) {
            return source[key];
        }
    }
    return fallback;
}

function readConfigNumber(source, keys, fallback) {
    const value = readConfigValue(source, keys, fallback);
    if (value === '') return fallback;
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : fallback;
}

function isManagedByGo2rtcURL(streamURL) {
    return String(streamURL || '').trim() === 'managed_by_go2rtc';
}

function cloneConfigCameraList(cameras) {
    return (cameras || []).map(cam => ({...cam}));
}

function ensureConfigCameraUiKey(cam, fallback = '') {
    if (!cam) return '';
    if (cam.__uiKey) return cam.__uiKey;
    const key = fallback || `camera-ui-${Date.now()}-${++configCameraUiSeq}`;
    Object.defineProperty(cam, '__uiKey', {
        value: key,
        enumerable: false,
        configurable: true,
        writable: true
    });
    return key;
}

function renderConfigForm() {
    document.getElementById('dailyMergeEnabled').checked = Boolean(configFormState.daily_merge.enabled);
    document.getElementById('dailyMergeTime').value = configFormState.daily_merge.time || '03:30';

    const list = document.getElementById('configCameraList');
    const empty = document.getElementById('configCameraEmpty');
    list.innerHTML = '';
    empty.classList.toggle('hidden', configFormState.cameras.length > 0);

    configFormState.cameras.forEach((cam, index) => {
        const uiKey = ensureConfigCameraUiKey(cam);
        const expanded = configCameraExpandedKeys.has(uiKey);
        const managedClass = isManagedByGo2rtcURL(cam.stream_url) ? ' is-go2rtc-managed' : '';
        const card = document.createElement('div');
        card.className = `config-camera-card${managedClass} ${expanded ? 'is-expanded' : 'is-collapsed'} rounded-xl border border-slate-200 bg-gradient-to-br from-white to-slate-50 p-3 shadow-sm`;
        card.dataset.index = String(index);
        card.dataset.uiKey = uiKey;
        card.innerHTML = renderConfigCameraCard(cam, index, expanded);
        list.appendChild(card);
    });

    const restoreBtn = document.getElementById('restoreConfigCamerasBtn');
    if (restoreBtn) {
        restoreBtn.disabled = !configFormInitialCamerasLoaded;
        restoreBtn.title = configFormInitialCamerasLoaded ? '恢复打开配置时的摄像头配置' : '暂无可恢复的摄像头配置';
    }
}

function renderConfigCameraCard(cam, index, expanded = false) {
    const modeValue = String(cam.mode || 'normal').toLowerCase();
    const normalMode = modeValue !== 'timelapse';
    const managedByGo2rtc = isManagedByGo2rtcURL(cam.stream_url);
    const motionEnabled = normalMode && Boolean(cam.motion_detect);
    const motionDetectAttrs = normalMode ? 'onchange="refreshConfigFormFromDom()"' : 'disabled';
    const motionUrlAttrs = normalMode ? '' : 'disabled title="延时模式不使用动检流"';
    const motionThresholdAttrs = motionEnabled ? '' : 'disabled title="先启用动检录制后再调整阈值"';
    const captureIntervalAttrs = normalMode ? 'disabled title="仅延时模式生效"' : '';
    const motionSectionTitle = normalMode ? '动检录制' : '延时摄影';
    const motionSectionDesc = normalMode
        ? '启用后仅事件片段会触发录像，阈值越低越敏感。'
        : '仅在延时模式下生效，动检项会被忽略。';
    const sourceHint = managedByGo2rtc ? '使用 go2rtc 同名流' : 'CamKeep 会把接入源注册到 go2rtc';
    const motionHint = normalMode ? '动检开启后仅事件录像' : '延时录像模式会忽略动检';
    return `
        <div class="config-camera-card-shell">
            <div class="config-camera-card-head">
                <button onclick="toggleConfigCamera(${index})" class="config-camera-card-summary" type="button" aria-expanded="${expanded ? 'true' : 'false'}">
                    <div class="config-camera-card-index">#${index + 1}</div>
                    <div class="min-w-0 flex-1 text-left">
                        <div class="config-camera-title-row">
                            <h4 class="truncate text-sm font-extrabold text-slate-800">${escapeHtml(cam.id || '未命名摄像头')}</h4>
                            ${managedByGo2rtc ? '<span class="config-camera-chip config-camera-chip--go2rtc">go2rtc 接管</span>' : ''}
                            ${motionEnabled ? '<span class="config-camera-chip config-camera-chip--motion">动检</span>' : ''}
                            <span class="config-camera-chip config-camera-chip--mode">${escapeHtml(modeValue || 'normal')} / ${escapeHtml(cam.format || DEFAULT_RECORD_FORMAT)}</span>
                        </div>
                        <p class="mt-1 truncate text-[11px] font-medium text-slate-500">${sourceHint}；${motionHint}</p>
                    </div>
                    <span class="config-camera-collapse-icon" aria-hidden="true">
                        <svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.4" d="M19 9l-7 7-7-7"></path></svg>
                    </span>
                </button>
                <button onclick="removeConfigCamera(${index})" class="config-camera-delete-btn" type="button">删除</button>
            </div>
            <div class="config-camera-card-body">
                ${configFormSection('基础信息', '识别、模式和录像时间，改完后立即影响当前卡片展示。', `
                    ${configTextInput('摄像头 ID', 'id', cam.id, 'front-door', true)}
                    ${configNumberInput('排序 order', 'order', cam.order, '0，数字越小越靠前')}
                    ${configSelectInput('模式', 'mode', cam.mode, [['normal', '普通'], ['timelapse', '延时']], `onchange="refreshConfigFormFromDom()"`)}
                    ${configSelectInput('格式', 'format', cam.format, [['ts', 'ts'], ['mp4', 'mp4']])}
                    ${configTextInput('录制时间', 'record_time', cam.record_time, '00:00-23:59')}
                `, 'config-form-section--identity')}
                ${configFormSection('接入源', '会注册到 go2rtc 的源地址，可填写 RTSP、ONVIF、FFmpeg 或其他 go2rtc 支持的 URL。', `
                    ${managedByGo2rtc ? configManagedStreamField(cam.id) : configTextInput('接入源 stream_url', 'stream_url', cam.stream_url, 'rtsp://... / onvif://... / ffmpeg:...', true, 'config-field-wide')}
                `, managedByGo2rtc ? 'config-form-section--stream is-go2rtc-managed' : 'config-form-section--stream')}
                ${configFormSection('录像策略', '控制保留、切片和磁盘占用。', `
                    ${configNumberInput('保留天数', 'retention_days', cam.retention_days, '0 不清理')}
                    ${configNumberInput('切片秒数', 'segment_duration', cam.segment_duration, '300 / 600')}
                    ${configNumberInput('最小文件 KB', 'min_size_kb', cam.min_size_kb, '1024')}
                `, 'config-form-section--storage')}
                ${configFormSection(motionSectionTitle, motionSectionDesc, normalMode ? `
                    ${configCheckboxInput('动检录制', 'motion_detect', motionEnabled, motionDetectAttrs)}
                    ${configTextInput('动检流 motion_url', 'motion_url', cam.motion_url, '可选，低码率子码流，仅用于识别', false, 'config-field-wide', motionUrlAttrs)}
                    ${configHiddenInput('capture_interval', cam.capture_interval)}
                    ${configNumberInput('动检阈值', 'motionDetectRatioThreshold', cam.motionDetectRatioThreshold, '0.01 表示 1%', '0.001', '', motionThresholdAttrs)}
                ` : `
                    ${configNumberInput('延时抓拍间隔', 'capture_interval', cam.capture_interval, '仅延时生效', '1', 'config-field-wide', captureIntervalAttrs)}
                    ${configHiddenInput('motion_url', cam.motion_url)}
                    ${configHiddenInput('motionDetectRatioThreshold', cam.motionDetectRatioThreshold)}
                    <input data-field="motion_detect" type="checkbox" ${cam.motion_detect ? 'checked' : ''} hidden>
                    <div class="config-form-section-note config-field-wide">延时模式不使用动检录像，保留接入源与抓拍间隔即可。</div>
                `, normalMode ? 'config-form-section--motion' : 'config-form-section--motion config-form-section--timelapse is-muted')}
            </div>
        </div>
    `;
}

function configFormSection(title, desc, content, extraClass = '') {
    return `
        <section class="config-form-section ${extraClass}">
            <div class="config-form-section-header">
                <div class="min-w-0">
                    <h5 class="config-form-section-title">${escapeHtml(title)}</h5>
                    <p class="config-form-section-desc">${escapeHtml(desc)}</p>
                </div>
            </div>
            <div class="config-form-section-grid">${content}</div>
        </section>
    `;
}

function configHiddenInput(field, value) {
    return `<input data-field="${field}" type="hidden" value="${escapeHtml(value ?? '')}">`;
}

function configFieldLabel(label, required = false) {
    const tagText = required ? '必填' : '选填';
    const tagClass = required ? 'config-field-tag--required' : 'config-field-tag--optional';
    return `
        <span class="config-field-label">
            <span>${escapeHtml(label)}</span>
            <span class="config-field-tag ${tagClass}">${tagText}</span>
        </span>
    `;
}

function configTextInput(label, field, value, placeholder, required = false, extraClass = '', attrs = '') {
    return `
        <label class="config-field ${extraClass}">
            ${configFieldLabel(label, required)}
            <input data-field="${field}" type="text" value="${escapeHtml(value || '')}" placeholder="${escapeHtml(placeholder || '')}" ${required ? 'required' : ''} ${attrs} class="w-full rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-medium text-slate-700 outline-none transition-all focus:border-blue-500 focus:ring-4 focus:ring-blue-500/10">
        </label>
    `;
}

function configManagedStreamField(camID) {
    const desc = configManagedStreamDesc(camID);
    return `
        <label class="config-field config-field-wide">
            ${configFieldLabel('接入源', true)}
            <input data-field="stream_url" type="hidden" value="managed_by_go2rtc">
            <div class="config-managed-rtsp">
                <span class="config-managed-rtsp-title">go2rtc 已接管</span>
                <span class="config-managed-rtsp-desc">${escapeHtml(desc)}</span>
            </div>
        </label>
    `;
}

function configManagedStreamDesc(camID) {
    const streamInfo = go2rtcStreamInfoMap.get(String(camID || ''));
    const sourceLabel = streamInfo?.source_label || '未知';
    return `接入方式：${sourceLabel}`;
}

function configNumberInput(label, field, value, placeholder, step = '1', extraClass = '', attrs = '') {
    return `
        <label class="config-field ${extraClass}">
            ${configFieldLabel(label, false)}
            <input data-field="${field}" type="number" step="${step}" value="${escapeHtml(value ?? '')}" placeholder="${escapeHtml(placeholder || '')}" ${attrs} class="w-full rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-medium text-slate-700 outline-none transition-all focus:border-blue-500 focus:ring-4 focus:ring-blue-500/10">
        </label>
    `;
}

function configSelectInput(label, field, value, options, attrs = '', required = true) {
    const opts = options.map(([optionValue, optionLabel]) => {
        const selected = String(value || '') === optionValue ? 'selected' : '';
        return `<option value="${optionValue}" ${selected}>${optionLabel}</option>`;
    }).join('');
    return `
        <label class="config-field">
            ${configFieldLabel(label, required)}
            <select data-field="${field}" ${required ? 'required' : ''} ${attrs} class="w-full rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-bold text-slate-700 outline-none transition-all focus:border-blue-500 focus:ring-4 focus:ring-blue-500/10">${opts}</select>
        </label>
    `;
}

function configCheckboxInput(label, field, checked, extraAttrs = '') {
    return `
        <label class="config-field">
            ${configFieldLabel(label, false)}
            <span class="config-toggle-control">
                <span class="text-xs font-extrabold text-slate-700">启用</span>
                <span class="relative inline-flex h-5 w-9 shrink-0 items-center">
                    <input data-field="${field}" type="checkbox" ${checked ? 'checked' : ''} ${extraAttrs} class="peer sr-only">
                    <span class="config-toggle-track absolute inset-0 rounded-full transition-colors peer-disabled:opacity-50"></span>
                    <span class="config-toggle-thumb absolute left-0.5 h-4 w-4 rounded-full shadow transition-transform peer-checked:translate-x-4"></span>
                </span>
            </span>
        </label>
    `;
}

function refreshConfigFormFromDom() {
    syncConfigCameraExpandedStateFromDom();
    configFormState = collectConfigForm({allowEmptyID: true});
    renderConfigForm();
}

function restoreConfigCameras() {
    if (!configFormInitialCamerasLoaded) return;
    syncConfigCameraExpandedStateFromDom();
    configFormState = collectConfigForm({allowEmptyID: true});
    configFormState.cameras = cloneConfigCameraList(configFormInitialCameras);
    configCameraExpandedKeys = new Set();
    renderConfigForm();
}

function collectConfigForm(options = {}) {
    const cfg = {
        daily_merge: {
            enabled: document.getElementById('dailyMergeEnabled').checked,
            time: document.getElementById('dailyMergeTime').value || '03:30'
        },
        cameras: []
    };

    document.querySelectorAll('.config-camera-card').forEach((card, index) => {
        const mode = readCardField(card, 'mode') || 'normal';
        const cam = {
            id: readCardField(card, 'id').trim(),
            order: readCardNumber(card, 'order', 0),
            stream_url: readCardField(card, 'stream_url').trim(),
            motion_url: readCardField(card, 'motion_url').trim(),
            retention_days: readCardNumber(card, 'retention_days', 0),
            segment_duration: readCardNumber(card, 'segment_duration', 0),
            format: readCardField(card, 'format') || DEFAULT_RECORD_FORMAT,
            min_size_kb: readCardNumber(card, 'min_size_kb', 0),
            record_time: readCardField(card, 'record_time').trim() || '00:00-23:59',
            mode,
            capture_interval: readCardNumber(card, 'capture_interval', 0),
            motion_detect: mode === 'normal' && readCardCheckbox(card, 'motion_detect'),
            motionDetectRatioThreshold: readCardFloat(card, 'motionDetectRatioThreshold', 0),
            auto_discovered: isManagedByGo2rtcURL(readCardField(card, 'stream_url'))
        };
        ensureConfigCameraUiKey(cam, card.dataset.uiKey);
        if (!cam.id && !options.allowEmptyID) throw new Error(`第 ${index + 1} 个摄像头 ID 不能为空`);
        cfg.cameras.push(cam);
    });
    return cfg;
}

function readCardField(card, field) {
    return card.querySelector(`[data-field="${field}"]`)?.value || '';
}

function readCardNumber(card, field, fallback) {
    const value = readCardField(card, field);
    if (value === '') return fallback;
    const parsed = Number.parseInt(value, 10);
    return Number.isFinite(parsed) ? parsed : fallback;
}

function readCardFloat(card, field, fallback) {
    const value = readCardField(card, field);
    if (value === '') return fallback;
    const parsed = Number.parseFloat(value);
    return Number.isFinite(parsed) ? parsed : fallback;
}

function readCardCheckbox(card, field) {
    return Boolean(card.querySelector(`[data-field="${field}"]`)?.checked);
}

function addConfigCamera(seed = {}) {
    syncConfigCameraExpandedStateFromDom();
    configFormState = collectConfigForm({allowEmptyID: true});
    const nextCam = normalizeConfigCamera({
        id: '',
        order: 0,
        stream_url: '',
        motion_url: '',
        retention_days: 7,
        segment_duration: 600,
        format: DEFAULT_RECORD_FORMAT,
        min_size_kb: 1024,
        record_time: '00:00-23:59',
        mode: 'normal',
        capture_interval: 1,
        motion_detect: false,
        motionDetectRatioThreshold: 0.01,
        auto_discovered: false,
        ...seed
    });
    const uiKey = ensureConfigCameraUiKey(nextCam);
    configFormState.cameras.unshift(nextCam);
    configCameraExpandedKeys.add(uiKey);
    renderConfigForm();
}

function toggleBatchCameraPanel(forceOpen = null) {
    const panel = document.getElementById('batchCameraPanel');
    const input = document.getElementById('batchCameraInput');
    if (!panel) return;

    const open = forceOpen === null ? panel.classList.contains('hidden') : Boolean(forceOpen);
    panel.classList.toggle('hidden', !open);
    if (open) {
        renderBatchCameraPreview();
        requestAnimationFrame(() => input?.focus());
    }
}

function renderBatchCameraPreview() {
    const input = document.getElementById('batchCameraInput');
    const summary = document.getElementById('batchCameraSummary');
    const preview = document.getElementById('batchCameraPreview');
    const applyBtn = document.getElementById('batchCameraApplyBtn');
    if (!input || !summary || !preview || !applyBtn) return;

    latestBatchCameraPreview = parseBatchCameraInput(input.value);
    const validCount = latestBatchCameraPreview.valid.length;
    const invalidCount = latestBatchCameraPreview.invalid.length;

    applyBtn.disabled = validCount === 0 || invalidCount > 0;
    summary.innerHTML = renderBatchCameraSummary(validCount, invalidCount);

    preview.classList.toggle('hidden', validCount === 0 && invalidCount === 0);
    preview.innerHTML = renderBatchCameraPreviewMarkup(latestBatchCameraPreview);
}

function renderBatchCameraSummary(validCount, invalidCount) {
    if (validCount === 0 && invalidCount === 0) {
        return `
            <strong>等待输入</strong>
            <span>粘贴多行接入源后会实时解析。</span>
        `;
    }
    return `
        <strong>${validCount} 个可添加</strong>
        <span>${invalidCount ? `${invalidCount} 行需要修正后才能添加` : '检查无误，确认后追加到列表顶部。'}</span>
    `;
}

function renderBatchCameraPreviewMarkup(result) {
    const validItems = result.valid.map(item => `
        <div class="config-batch-camera-preview-item">
            <span class="config-batch-camera-preview-line">L${item.line}</span>
            <div class="min-w-0">
                <strong>${escapeHtml(item.id)}</strong>
                <em>${item.generated ? '<span class="config-batch-camera-generated">自动 ID</span>' : ''}${escapeHtml(item.stream_url)}</em>
            </div>
        </div>
    `).join('');
    const invalidItems = result.invalid.map(item => `
        <div class="config-batch-camera-preview-item is-error">
            <span class="config-batch-camera-preview-line">L${item.line}</span>
            <div class="min-w-0">
                <strong>${escapeHtml(item.error)}</strong>
                <em>${escapeHtml(item.raw)}</em>
            </div>
        </div>
    `).join('');
    const validMarkup = validItems
        ? `<div class="config-batch-camera-preview-group"><div class="config-batch-camera-preview-title">可添加</div>${validItems}</div>`
        : '';
    const invalidMarkup = invalidItems
        ? `<div class="config-batch-camera-preview-group"><div class="config-batch-camera-preview-title is-error">需要修正</div>${invalidItems}</div>`
        : '';
    return validMarkup + invalidMarkup;
}

function clearBatchCameraInput() {
    const input = document.getElementById('batchCameraInput');
    const feedback = document.getElementById('batchCameraFeedback');
    if (input) input.value = '';
    if (feedback) feedback.classList.add('hidden');
    renderBatchCameraPreview();
}

function parseBatchCameraInput(text) {
    let existingIDs = new Set();
    try {
        const cfg = collectConfigForm({allowEmptyID: true});
        existingIDs = new Set(cfg.cameras.map(cam => cam.id).filter(Boolean));
    } catch (e) {
        existingIDs = new Set((configFormState.cameras || []).map(cam => cam.id).filter(Boolean));
    }

    const usedIDs = new Set(existingIDs);
    const result = {valid: [], invalid: []};
    String(text || '').split(/\r?\n/).forEach((rawLine, index) => {
        const line = index + 1;
        const raw = rawLine.trim();
        if (!raw || raw.startsWith('#')) return;

        const parsed = parseBatchCameraLine(raw);
        if (!parsed.stream_url) {
            result.invalid.push({line, raw, error: parsed.error || '缺少接入源'});
            return;
        }

        const baseID = parsed.id || cameraIDFromSource(parsed.stream_url, line);
        const id = uniqueCameraID(baseID, usedIDs);
        usedIDs.add(id);
        result.valid.push({
            line,
            id,
            stream_url: parsed.stream_url,
            generated: !parsed.id
        });
    });
    return result;
}

function parseBatchCameraLine(raw) {
    const firstSpace = raw.search(/\s/);
    if (firstSpace < 0) {
        if (looksLikeCameraSource(raw)) return {id: '', stream_url: raw};
        return {id: raw, stream_url: '', error: '缺少接入源'};
    }

    const first = raw.slice(0, firstSpace).trim();
    const rest = raw.slice(firstSpace).trim();
    if (looksLikeCameraSource(first)) {
        return {id: '', stream_url: raw};
    }
    return {id: normalizeBatchCameraID(first), stream_url: rest};
}

function looksLikeCameraSource(value) {
    const text = String(value || '').trim().toLowerCase();
    return text === 'managed_by_go2rtc'
        || /^[a-z][a-z0-9+.-]*:/.test(text)
        || text.includes('://');
}

function normalizeBatchCameraID(value) {
    return String(value || '')
        .trim()
        .replace(/\s+/g, '-')
        .replace(/[^\w.-]+/g, '-')
        .replace(/^-+|-+$/g, '');
}

function cameraIDFromSource(source, line) {
    const text = String(source || '').trim();
    const host = extractHostFromCameraSource(text);
    const base = normalizeBatchCameraID(host || `cam-${line}`).toLowerCase();
    return base.startsWith('cam-') ? base : `cam-${base}`;
}

function extractHostFromCameraSource(source) {
    const text = String(source || '').trim();
    const nestedURL = text.match(/[a-z][a-z0-9+.-]*:\/\/[^#\s]+/i)?.[0] || text;
    try {
        const parsed = new URL(nestedURL);
        if (parsed.hostname) return parsed.hostname;
    } catch (e) {
    }

    const afterAuth = text.includes('@') ? text.split('@').pop() : text;
    const hostMatch = afterAuth.match(/([a-z0-9.-]+\.[a-z]{2,}|\d{1,3}(?:\.\d{1,3}){3})/i);
    return hostMatch ? hostMatch[1] : '';
}

function uniqueCameraID(baseID, usedIDs) {
    const base = normalizeBatchCameraID(baseID) || 'cam';
    if (!usedIDs.has(base)) return base;
    let index = 2;
    while (usedIDs.has(`${base}-${index}`)) {
        index++;
    }
    return `${base}-${index}`;
}

function applyBatchAddCameras() {
    const input = document.getElementById('batchCameraInput');
    if (!input) return;

    const parsed = parseBatchCameraInput(input.value);
    latestBatchCameraPreview = parsed;
    renderBatchCameraPreview();
    if (parsed.valid.length === 0) return;

    syncConfigCameraExpandedStateFromDom();
    configFormState = collectConfigForm({allowEmptyID: true});
    const nextCameras = parsed.valid.map(item => normalizeConfigCamera({
        id: item.id,
        order: 0,
        stream_url: item.stream_url,
        motion_url: '',
        retention_days: 7,
        segment_duration: 600,
        format: DEFAULT_RECORD_FORMAT,
        min_size_kb: 1024,
        record_time: '00:00-23:59',
        mode: 'normal',
        capture_interval: 1,
        motion_detect: false,
        motionDetectRatioThreshold: 0.01,
        auto_discovered: false
    }));

    nextCameras.forEach(cam => ensureConfigCameraUiKey(cam));
    configFormState.cameras = [...nextCameras, ...configFormState.cameras];
    configCameraExpandedKeys = new Set();
    renderConfigForm();

    input.value = '';
    latestBatchCameraPreview = {valid: [], invalid: []};
    showBatchCameraFeedback(`已添加 ${nextCameras.length} 个摄像头，保存并应用后生效。`);
    renderBatchCameraPreview();
}

function showBatchCameraFeedback(message) {
    const feedback = document.getElementById('batchCameraFeedback');
    if (!feedback) return;
    feedback.textContent = message;
    feedback.classList.remove('hidden');
}

function removeConfigCamera(index) {
    syncConfigCameraExpandedStateFromDom();
    const removedKey = document.querySelector(`.config-camera-card[data-index="${index}"]`)?.dataset.uiKey;
    configFormState = collectConfigForm({allowEmptyID: true});
    configFormState.cameras.splice(index, 1);
    if (removedKey) configCameraExpandedKeys.delete(removedKey);
    renderConfigForm();
}

function toggleConfigCamera(index) {
    syncConfigCameraExpandedStateFromDom();
    const card = document.querySelector(`.config-camera-card[data-index="${index}"]`);
    const uiKey = card?.dataset.uiKey;
    if (!uiKey) return;
    if (configCameraExpandedKeys.has(uiKey)) {
        configCameraExpandedKeys.delete(uiKey);
    } else {
        configCameraExpandedKeys.add(uiKey);
    }
    configFormState = collectConfigForm({allowEmptyID: true});
    renderConfigForm();
}

function syncConfigCameraExpandedStateFromDom() {
    const next = new Set();
    document.querySelectorAll('.config-camera-card.is-expanded').forEach(card => {
        if (card.dataset.uiKey) next.add(card.dataset.uiKey);
    });
    configCameraExpandedKeys = next;
}

function configToYaml(cfg) {
    const lines = [
        'daily_merge:',
        `  enabled: ${cfg.daily_merge.enabled ? 'true' : 'false'}`,
        `  time: ${yamlScalar(cfg.daily_merge.time || '03:30')}`,
        '',
        'cameras:'
    ];
    cfg.cameras.forEach(cam => {
        lines.push(`  - id: ${yamlScalar(cam.id)}`);
        lines.push(`    order: ${cam.order || 0}`);
        lines.push(`    stream_url: ${yamlScalar(cam.stream_url)}`);
        lines.push(`    motion_url: ${yamlScalar(cam.motion_url)}`);
        lines.push(`    retention_days: ${cam.retention_days}`);
        lines.push(`    segment_duration: ${cam.segment_duration}`);
        lines.push(`    format: ${yamlScalar(cam.format)}`);
        lines.push(`    min_size_kb: ${cam.min_size_kb}`);
        lines.push(`    record_time: ${yamlScalar(cam.record_time)}`);
        lines.push(`    mode: ${yamlScalar(cam.mode)}`);
        lines.push(`    capture_interval: ${cam.capture_interval}`);
        lines.push(`    motion_detect: ${cam.motion_detect ? 'true' : 'false'}`);
        lines.push(`    motionDetectRatioThreshold: ${cam.motionDetectRatioThreshold}`);
        if (cam.auto_discovered) lines.push('    auto_discovered: true');
    });
    return lines.join('\n') + '\n';
}

function yamlScalar(value) {
    return JSON.stringify(String(value ?? ''));
}

function appendStreamToConfig(stream) {
    const streamInfo = normalizeGo2rtcStreamInfo(stream);
    const streamId = streamInfo.id;
    if (!streamId) return;
    go2rtcStreamInfoMap.set(streamId, streamInfo);

    const nextOrder = nextConfigCameraOrder();
    if (configEditMode === 'form') {
        addConfigCamera({
            id: streamId,
            order: nextOrder,
            stream_url: 'managed_by_go2rtc',
            auto_discovered: true
        });
        finishAppendStream(streamId);
        return;
    }

    const textArea = document.getElementById('configYaml');
    let content = textArea.value;

    let listIndent = "  ";
    let propIndent = "    ";
    const indentMatch = content.match(/^(\s+)-\s/m);
    if (indentMatch) {
        listIndent = indentMatch[1];
        propIndent = listIndent + "  ";
    }

    const newCamYaml = [`${listIndent}- id: "${streamId}"`, `${propIndent}order: ${nextOrder}`, `${propIndent}stream_url: "managed_by_go2rtc"`, `${propIndent}motion_url: ""`, `${propIndent}auto_discovered: true`, `${propIndent}retention_days: 7`, `${propIndent}segment_duration: 600`, `${propIndent}format: ${DEFAULT_RECORD_FORMAT}`, `${propIndent}min_size_kb: 1024`, `${propIndent}record_time: "00:00-23:59"`, `${propIndent}mode: normal`, `${propIndent}motion_detect: false`, `${propIndent}motionDetectRatioThreshold: 0.01`].join('\n') + '\n';

    if (content.trim() === '') {
        content = 'cameras:\n';
    } else {
        if (!content.endsWith('\n')) content += '\n';
        if (!content.includes('cameras:')) content += 'cameras:\n';
    }

    textArea.value = content + newCamYaml;

    finishAppendStream(streamId);

    textArea.classList.add('ring-2', 'ring-emerald-400', 'transition-all', 'duration-300');
    setTimeout(() => textArea.classList.remove('ring-2', 'ring-emerald-400'), 800);
}

function nextConfigCameraOrder() {
    try {
        const cfg = collectConfigForm({allowEmptyID: true});
        return cfg.cameras.reduce((maxOrder, cam) => Math.max(maxOrder, Number(cam.order) || 0), 0) + 1;
    } catch (e) {
        return 0;
    }
}

function finishAppendStream(streamId) {
    const tag = document.getElementById(`unmanaged-${encodeURIComponent(streamId)}`);
    if (tag) tag.remove();
    const listDiv = document.getElementById('unmanagedList');
    const remaining = listDiv.querySelectorAll('.config-import-result-item').length;
    updateGo2rtcImportBadge(remaining);
    const head = listDiv.querySelector('.config-import-result-head');
    if (remaining > 0) {
        const count = head?.querySelector('.config-import-result-count');
        if (count) count.textContent = `${remaining} 个可导入流`;
        return;
    }
    if (head) head.remove();
    if (!listDiv.querySelector('.config-import-result-message')) {
        listDiv.innerHTML = '<span class="config-import-result-message config-import-result-message--ok">已导入当前扫描到的全部流，检查参数后保存并应用。</span>';
    }
}

function buildCameraCoverURL(camId) {
    return cameraCoverObjectURLs.get(camId) || '';
}

function escapeCssURL(value) {
    return String(value || '').replace(/["\\\n\r\f]/g, char => ({
        '"': '\\"',
        '\\': '\\\\',
        '\n': '\\A ',
        '\r': '\\D ',
        '\f': '\\C '
    })[char]);
}

function buildCameraCoverMarkup(camId, cam, streamState) {
    const coverURL = buildCameraCoverURL(camId);
    const hasCover = Boolean(coverURL);

    const imageMarkup = hasCover
        ? `<img src="${escapeHtml(coverURL)}" alt="${escapeHtml(camId)} 封面" loading="lazy" decoding="async" class="h-full w-full object-cover transition-transform duration-500 group-hover:scale-[1.03]">`
        : `<div class="flex h-full w-full items-center justify-center bg-[radial-gradient(circle_at_top_left,_rgba(148,163,184,0.18),_rgba(15,23,42,0.08)_70%)] text-[10px] font-bold text-slate-400">
                <svg class="h-5 w-5 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M3 7.5A2.5 2.5 0 015.5 5h13A2.5 2.5 0 0121 7.5v9a2.5 2.5 0 01-2.5 2.5h-13A2.5 2.5 0 013 16.5v-9z"></path>
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M8 9.75h.01M7 16l3.6-3.6a1 1 0 011.4 0L17 17"></path>
                </svg>
            </div>`;

    return `
        <button type="button"
                data-cam-id="${escapeHtml(camId)}"
                onclick="event.stopPropagation(); previewLive(this.dataset.camId)"
                class="camera-node-cover group/cover relative aspect-video w-[88px] shrink-0 overflow-hidden rounded-md border border-slate-200 bg-slate-100 text-white ring-1 ring-white/70 transition-all hover:border-blue-300 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400 sm:w-[96px] lg:w-[78px]"
                title="点击预览直播"
                aria-label="预览 ${escapeHtml(camId)} 直播">
            ${imageMarkup}
            <span class="camera-node-live-btn pointer-events-none absolute left-1/2 top-1/2 flex h-9 w-9 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border border-white/14 bg-white/8 text-white shadow-[0_10px_26px_-18px_rgba(15,23,42,0.76)] backdrop-blur-[2px] transition-all duration-200 group-hover/cover:scale-105 group-hover/cover:border-white/24 group-hover/cover:bg-white/14 group-active/cover:scale-95">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-slate-950/22 ring-1 ring-white/10 shadow-inner">
                    <svg class="h-3.5 w-3.5 translate-x-[1px]" fill="currentColor" viewBox="0 0 24 24">
                        <path d="M8 5.5v13l10-6.5-10-6.5z"></path>
                    </svg>
                </span>
            </span>
        </button>
    `;
}

async function ensureCameraCoverLoaded(camId, cam) {
    if (cam.cover_ready !== true || cameraCoverObjectURLs.has(camId) || cameraCoverRequested.has(camId)) {
        return;
    }

    cameraCoverRequested.add(camId);
    try {
        const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/cover`);
        if (!resp.ok) throw new Error(`cover status=${resp.status}`);

        const blob = await resp.blob();
        if (blob.size === 0) throw new Error('empty cover');

        cameraCoverObjectURLs.set(camId, URL.createObjectURL(blob));
        loadStatus();
    } catch (e) {
        cameraCoverFailed.add(camId);
        loadStatus();
        console.warn(`加载摄像头封面失败: ${camId}`, e);
    }
}

function releaseCameraCoverObjectURLs() {
    cameraCoverObjectURLs.forEach(url => URL.revokeObjectURL(url));
    cameraCoverObjectURLs.clear();
    cameraCoverRequested.clear();
    cameraCoverFailed.clear();
    cameraCardRenderKeys.clear();
}

function buildCameraCardView(id, cam) {
    ensureCameraCoverLoaded(id, cam);
    const coverURL = buildCameraCoverURL(id);

    const recordState = cam.record_state || (cam.is_running ? 'recording' : 'idle');
    const overrideState = normalizeRecordOverride(cam.record_override);
    const isSelected = currentSelectedCam === id;
    const streamState = cam.stream_state || 'offline';
    const recordSchedule = buildRecordScheduleDisplay(cam.record_time, overrideState);
    const recordStateView = buildRecordStateView(recordState, overrideState);
    const startActionClass = overrideState === 'start' ? 'is-active' : '';
    const stopActionClass = overrideState === 'stop' ? 'is-active' : '';
    const autoActionClass = overrideState === 'auto' ? 'is-active' : '';
    const adminActions = canAdmin() ? `
            <div class="camera-node-card-actions grid shrink-0 grid-cols-3 overflow-hidden rounded-md border border-slate-200 bg-white shadow-[inset_0_1px_0_rgba(255,255,255,0.85)]">
                <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'start')"
                        class="camera-node-action-btn camera-node-action-btn--start ${startActionClass} flex h-6 w-6 items-center justify-center border-r border-slate-200 text-emerald-600 transition-all hover:bg-emerald-500 hover:text-white active:scale-95"
                        title="强制录制"
                        aria-label="强制录制">
                    <svg class="h-3 w-3" fill="currentColor" viewBox="0 0 24 24">
                        <circle cx="12" cy="12" r="5"></circle>
                    </svg>
                    <span class="camera-node-action-label">强录</span>
                </button>
                <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'stop')"
                        class="camera-node-action-btn camera-node-action-btn--stop ${stopActionClass} flex h-6 w-6 items-center justify-center border-r border-slate-200 text-rose-600 transition-all hover:bg-rose-500 hover:text-white active:scale-95"
                        title="强制停止"
                        aria-label="强制停止">
                    <svg class="h-3 w-3" fill="currentColor" viewBox="0 0 24 24">
                        <rect x="7" y="7" width="10" height="10" rx="1.5"></rect>
                    </svg>
                    <span class="camera-node-action-label">停录</span>
                </button>
                <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'auto')"
                        class="camera-node-action-btn camera-node-action-btn--auto ${autoActionClass} flex h-6 w-6 items-center justify-center text-indigo-600 transition-all hover:bg-indigo-500 hover:text-white active:scale-95"
                        title="恢复计划"
                        aria-label="恢复计划">
                    <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.3">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M7 7h10v10H7z"></path>
                        <path stroke-linecap="round" stroke-linejoin="round" d="M9 3v4M15 3v4M7 11h10"></path>
                    </svg>
                    <span class="camera-node-action-label">计划</span>
                </button>
            </div>
    ` : '';
    let streamLight, streamText;

    if (streamState === 'online') {
        streamLight = 'bg-green-500 shadow-[0_0_5px_#22c55e]';
        streamText = '<span class="text-[8px] leading-none text-green-600 font-bold">在线</span>';
    } else if (streamState === 'idle') {
        streamLight = 'bg-blue-400 shadow-[0_0_5px_#60a5fa]';
        streamText = '<span class="text-[8px] leading-none text-blue-500 font-bold">待机</span>';
    } else {
        streamLight = 'bg-red-500 shadow-[0_0_5px_#ef4444]';
        streamText = '<span class="text-[8px] leading-none text-red-500 font-bold">断线</span>';
    }

    const modeValue = (cam.mode || 'normal').toLowerCase();
    const modeDisplay = modeValue === 'motion'
        ? '动检'
        : modeValue === 'timelapse'
            ? '延时'
            : '常规';
    const modeBadgeClass = modeValue === 'motion'
        ? 'bg-amber-100 text-amber-700'
        : modeValue === 'timelapse'
            ? 'bg-sky-100 text-sky-700'
            : 'bg-slate-100 text-slate-500';

    const coverBackgroundClass = coverURL ? 'has-cover-background' : '';
    const className = `camera-node-card ${coverBackgroundClass} ${recordStateView.cardClass} ${isSelected ? 'is-selected' : ''} overflow-hidden rounded-md border cursor-pointer transition-all group`;
    const style = coverURL ? `--camera-node-cover-bg: url("${escapeCssURL(coverURL)}");` : '';
    const html = `
        <div class="flex items-center gap-1 p-1">
            ${buildCameraCoverMarkup(id, cam, streamState)}
            <div class="flex min-w-0 flex-1 flex-col justify-center gap-0.5 py-0.5">
                <div class="min-w-0">
                    <div class="flex min-w-0 items-center gap-1">
                        <span class="truncate text-[11px] font-extrabold leading-3 tracking-tight text-gray-800">${id}</span>
                        <span class="shrink-0 rounded ${modeBadgeClass} px-1 py-0.5 text-[7px] font-bold leading-none">${modeDisplay}</span>
                        <span class="camera-node-record-chip ${recordStateView.chipClass}" title="本地录制状态: ${escapeHtml(recordStateView.title)}">${recordStateView.label}</span>
                    </div>
                    <div class="mt-0.5 flex flex-wrap items-center gap-0.5">
                        <span class="inline-flex h-3.5 items-center rounded bg-slate-50 px-1 ring-1 ring-slate-100" title="摄像机实时流状态: ${escapeHtml(streamState)}">
                            <span class="mr-0.5 h-1.5 w-1.5 shrink-0 rounded-full ${streamLight}"></span>
                            ${streamText}
                        </span>
                    </div>
                </div>
            </div>
            ${adminActions}
        </div>

        <div class="camera-node-card-footer camera-node-schedule-pill ${recordSchedule.pillClass} flex min-w-0 items-center gap-1 border-t px-2 py-1"
             title="${escapeHtml(recordSchedule.title)}">
                <svg class="camera-node-schedule-icon h-2.5 w-2.5 shrink-0 ${recordSchedule.iconClass}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6l4 2"></path>
                    <circle cx="12" cy="12" r="9"></circle>
                </svg>
                <span class="camera-node-schedule-badge shrink-0 text-[7px] font-bold leading-none ${recordSchedule.badgeClass}">${recordSchedule.badge}</span>
                <span class="camera-node-schedule-text min-w-0 flex-1 truncate font-mono text-[8px] font-semibold leading-none ${recordSchedule.textClass}">${escapeHtml(recordSchedule.text)}</span>
        </div>
    `;

    return {className, html, style};
}

function buildRecordStateView(recordState, overrideState) {
    const overrideClass = buildRecordOverrideCardClass(overrideState);

    if (recordState === 'motion_recording') {
        return {
            cardClass: `is-motion-recording ${overrideClass}`,
            chipClass: 'camera-node-record-chip--motion-recording',
            label: '动检录制',
            title: '动检录制中'
        };
    }

    if (recordState === 'motion_detecting') {
        return {
            cardClass: `is-motion-detecting ${overrideClass}`,
            chipClass: 'camera-node-record-chip--motion-detecting',
            label: '动检中',
            title: '动检中'
        };
    }

    if (recordState === 'recording') {
        return {
            cardClass: `is-recording ${overrideClass}`,
            chipClass: 'camera-node-record-chip--recording',
            label: '录制中',
            title: '录制中'
        };
    }

    return {
        cardClass: `is-idle ${overrideClass}`,
        chipClass: 'camera-node-record-chip--idle',
        label: '未录像',
        title: '未录像'
    };
}

function buildRecordOverrideCardClass(overrideState) {
    if (overrideState === 'start') return 'is-override-start';
    if (overrideState === 'stop') return 'is-override-stop';
    return 'is-override-auto';
}

// --- 状态加载 ---
async function loadStatus() {
    try {
        const resp = await fetch('/api/status');
        const data = await resp.json();
        const list = document.getElementById('camList');
        const cameras = Object.entries(data || {});
        updateCameraStats(cameras);
        const visibleCamIds = new Set(cameras.map(([id]) => id));
        if (currentSelectedCam && !visibleCamIds.has(currentSelectedCam)) {
            currentSelectedCam = null;
            setSelectedRecordPath('');
            renderRecordSelectionPrompt('当前账号不可访问原选中的摄像头');
        }

        if (cameras.length === 0) {
            list.innerHTML = `
                <div class="rounded-lg border border-dashed border-slate-200 bg-slate-50 px-3 py-8 text-center text-sm font-bold text-slate-400">
                    暂无可访问的实时节点
                </div>
            `;
            if (!currentSelectedCam) renderRecordSelectionPrompt('当前账号暂无可访问摄像头');
            return;
        }

        const seenCamIds = new Set();
        Array.from(list.children).forEach(child => {
            if (!child.dataset.camId) child.remove();
        });

        cameras.forEach(([id, cam], index) => {
            seenCamIds.add(id);
            window.cameraCapabilityCache.set(id, {
                onvif_enabled: cam.onvif_enabled === true,
                capability_state: cam.onvif_capability_state || cam.capability_state || '',
                ptz_state: cam.ptz_state || '',
                imaging_state: cam.imaging_state || ''
            });

            const view = buildCameraCardView(id, cam);
            let item = Array.from(list.children).find(child => child.dataset.camId === id);
            if (!item) {
                item = document.createElement('div');
                item.dataset.camId = id;
                item.onclick = () => selectCamera(id);
            }
            item.className = view.className;
            if (view.style) {
                item.setAttribute('style', view.style);
            } else {
                item.removeAttribute('style');
            }
            if (cameraCardRenderKeys.get(id) !== view.html) {
                item.innerHTML = view.html;
                cameraCardRenderKeys.set(id, view.html);
            }
            const desiredIndex = list.children[index]?.dataset?.camId === id ? index : -1;
            if (desiredIndex === -1) {
                list.insertBefore(item, list.children[index] || null);
            }
        });

        Array.from(list.querySelectorAll('[data-cam-id]')).forEach(item => {
            if (!seenCamIds.has(item.dataset.camId)) {
                cameraCardRenderKeys.delete(item.dataset.camId);
                item.remove();
            }
        });

        refreshPTZPanel();
    } catch (e) {
        updateCameraStats([], true);
        console.error("同步状态失败:", e);
    }
}

function updateCameraStats(cameras, failed = false) {
    const summary = document.getElementById('camStatsSummary');
    const totalEl = document.getElementById('camStatsTotal');
    const onlineEl = document.getElementById('camStatsOnline');
    const idleEl = document.getElementById('camStatsIdle');
    const recordingEl = document.getElementById('camStatsRecording');
    const offlineEl = document.getElementById('camStatsOffline');

    if (failed) {
        if (summary) summary.innerText = '同步失败';
        [totalEl, onlineEl, idleEl, recordingEl, offlineEl].forEach(el => {
            if (el) el.innerText = '-';
        });
        return;
    }

    const stats = cameras.reduce((result, [, cam]) => {
        const streamState = cam.stream_state || 'offline';
        const recordState = cam.record_state || (cam.is_running ? 'recording' : 'idle');
        result.total += 1;
        if (streamState === 'online') result.online += 1;
        if (streamState === 'idle') result.idle += 1;
        if (streamState === 'offline') result.offline += 1;
        if (recordState === 'recording' || recordState === 'motion_detecting' || recordState === 'motion_recording') result.recording += 1;
        return result;
    }, {total: 0, online: 0, idle: 0, recording: 0, offline: 0});

    if (summary) summary.innerText = `${stats.total} 节点`;
    if (totalEl) totalEl.innerText = stats.total;
    if (onlineEl) onlineEl.innerText = stats.online;
    if (idleEl) idleEl.innerText = stats.idle;
    if (recordingEl) recordingEl.innerText = stats.recording;
    if (offlineEl) offlineEl.innerText = stats.offline;
}

function buildRecordScheduleDisplay(recordTime, override) {
    const rawValue = String(recordTime || '').trim();
    const ranges = parseRecordTimeRanges(rawValue);
    const hasValidRanges = ranges.length > 0;
    const overrideState = normalizeRecordOverride(override);

    if (!rawValue) {
        const base = {
            badge: '全天',
            text: '未配置，按全天录制',
            title: '录制计划: 未配置，系统按全天录制'
        };
        return applyRecordOverrideDisplay(base, overrideState, 'full');
    }

    if (!hasValidRanges) {
        const base = {
            badge: '缺省',
            text: '按全天录制',
            title: `录制计划: ${rawValue} (未识别，系统按全天录制)`
        };
        return applyRecordOverrideDisplay(base, overrideState, 'fallback');
    }

    const text = ranges.map(formatRecordRangeText).join(' / ');
    const fullDay = isFullDayRecordSchedule(ranges);
    const now = new Date();
    const nowMinutes = now.getHours() * 60 + now.getMinutes();
    const inSchedule = fullDay || ranges.some(range => isMinuteInRecordRange(nowMinutes, range.start, range.end));

    if (fullDay) {
        const base = {
            badge: '全天',
            text: '全天录制',
            title: `录制计划: ${text}`
        };
        return applyRecordOverrideDisplay(base, overrideState, 'full');
    }

    const base = {
        badge: inSchedule ? '计划内' : '计划外',
        text,
        title: `录制计划: ${text}，当前${inSchedule ? '在' : '不在'}计划时间内`
    };
    return applyRecordOverrideDisplay(base, overrideState, inSchedule ? 'active' : 'inactive');
}

function applyRecordOverrideDisplay(base, overrideState, scheduleState) {
    if (overrideState === 'start') {
        return {
            badge: '强制录',
            text: formatScheduleTextWithState(base),
            title: `${base.title}。当前手动覆盖: 强制录制，record_time 不会限制启动`,
            ...recordScheduleClasses('forced-start')
        };
    }

    if (overrideState === 'stop') {
        return {
            badge: '强制停',
            text: formatScheduleTextWithState(base),
            title: `${base.title}。当前手动覆盖: 强制停止，即使在计划内也不会录像`,
            ...recordScheduleClasses('forced-stop')
        };
    }

    return {
        badge: '计划录',
        text: formatScheduleTextWithState(base),
        title: `${base.title}。当前手动覆盖: 按计划录制`,
        ...recordScheduleClasses(scheduleState)
    };
}

function formatScheduleTextWithState(base) {
    if (base.badge === '全天') return base.text;
    return `${base.badge} · ${base.text}`;
}

function recordScheduleClasses(state) {
    if (state === 'forced-start') {
        return {
            pillClass: 'camera-node-schedule-pill--forced-start',
            iconClass: 'text-emerald-600',
            badgeClass: 'text-emerald-700',
            textClass: 'text-slate-600'
        };
    }

    if (state === 'forced-stop') {
        return {
            pillClass: 'camera-node-schedule-pill--forced-stop',
            iconClass: 'text-rose-500',
            badgeClass: 'text-rose-700',
            textClass: 'text-rose-700'
        };
    }

    if (state === 'full' || state === 'active') {
        return {
            pillClass: 'camera-node-schedule-pill--active',
            iconClass: 'text-emerald-500',
            badgeClass: 'text-emerald-700',
            textClass: 'text-slate-600'
        };
    }

    if (state === 'fallback') {
        return {
            pillClass: 'camera-node-schedule-pill--fallback',
            iconClass: 'text-amber-500',
            badgeClass: 'text-amber-700',
            textClass: 'text-amber-700'
        };
    }

    return {
        pillClass: 'camera-node-schedule-pill--inactive',
        iconClass: 'text-slate-400',
        badgeClass: 'text-slate-500',
        textClass: 'text-slate-500'
    };
}

function normalizeRecordOverride(override) {
    const value = String(override || '').trim().toLowerCase();
    return value === 'start' || value === 'stop' ? value : 'auto';
}

function parseRecordTimeRanges(recordTime) {
    return String(recordTime || '')
        .split(/[,;，；\n\r]+/)
        .map(item => item.trim())
        .filter(Boolean)
        .map(item => {
            const parts = item.split('-').map(part => part.trim());
            if (parts.length !== 2) return null;

            const start = parseClockMinutes(parts[0]);
            const end = parseClockMinutes(parts[1]);
            if (start === null || end === null) return null;

            return {start, end};
        })
        .filter(Boolean);
}

function parseClockMinutes(clock) {
    const match = String(clock || '').match(/^(\d{1,2}):([0-5]\d)$/);
    if (!match) return null;

    const hour = Number(match[1]);
    const minute = Number(match[2]);
    if (hour < 0 || hour > 24 || (hour === 24 && minute !== 0)) return null;

    return hour * 60 + minute;
}

function isMinuteInRecordRange(minute, start, end) {
    if (start <= end) return minute >= start && minute <= end;
    return minute >= start || minute <= end;
}

function isFullDayRecordSchedule(ranges) {
    return ranges.some(range => range.start === 0 && range.end >= 1439);
}

function formatRecordRangeText(range) {
    return `${formatClockText(range.start)}-${formatClockText(range.end)}`;
}

function formatClockText(minutes) {
    if (minutes === 1440) return '24:00';
    const hour = Math.floor(minutes / 60);
    const minute = minutes % 60;
    return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
}

function escapeHtml(value) {
    return String(value).replace(/[&<>"']/g, char => ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;'
    })[char]);
}

// --- 宫格矩阵与播放逻辑 ---
function setLayout(layoutCount) {
    currentLayout = layoutCount;
    if (activeCell >= layoutCount) activeCell = 0;
    syncSelectedRecordFromActiveCell();

    [1, 4, 6].forEach(num => {
        const btn = document.getElementById(`btn-layout-${num}`);
        if (num === layoutCount) {
            btn.classList.add('bg-blue-600/50', 'text-white');
            btn.classList.remove('text-gray-400');
        } else {
            btn.classList.remove('bg-blue-600/50', 'text-white');
            btn.classList.add('text-gray-400');
        }
    });
    renderGrid();
}

function renderGrid() {
    const grid = document.getElementById('video-grid');
    grid.className = 'min-w-0 flex-1 h-full p-1 bg-black grid gap-1 transition-all duration-300 ' + (currentLayout === 1 ? 'grid-cols-1 grid-rows-1' : currentLayout === 4 ? 'grid-cols-2 grid-rows-2' : compactGrid ? 'grid-cols-2 grid-rows-3' : 'grid-cols-3 grid-rows-2');

    stopAllCellPlayback();
    grid.innerHTML = '';

    for (let i = 0; i < currentLayout; i++) {
        const isFocused = i === activeCell;
        const activeCellClass = isFocused ? 'matrix-active-cell' : '';
        const cellFocusClass = currentLayout === 1
            ? 'border-gray-800'
            : (isFocused ? 'border-blue-500 shadow-[inset_0_0_20px_rgba(59,130,246,0.3)]' : 'border-gray-800 hover:border-gray-600');
        const liveIframeClass = currentLayout === 1
            ? 'w-full h-full border-0 hidden'
            : 'w-full h-full border-0 hidden pointer-events-none';
        const cellHtml = `
            <div id="cell-${i}" onclick="setActiveCell(${i})" ondblclick="toggleCellFullscreen(${i})" class="relative w-full h-full bg-gray-900 border-[2px] transition-colors overflow-hidden group cursor-pointer ${activeCellClass} ${cellFocusClass}">
                <iframe id="live-iframe-${i}" class="${liveIframeClass}" allow="autoplay; fullscreen; microphone; camera"></iframe>
                <div id="dplayer-${i}" class="w-full h-full hidden"></div>
                <video id="native-player-${i}" class="w-full h-full object-contain hidden bg-black" playsinline controls></video>
                <div id="empty-state-${i}" class="absolute inset-0 flex flex-col items-center justify-center text-gray-700 pointer-events-none group-hover:text-gray-500 transition-colors">
                    <svg class="w-8 h-8 mb-2 opacity-30" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                    <span class="text-xs font-bold tracking-wider uppercase opacity-50">窗口 ${i + 1}</span>
                </div>
                <div class="absolute top-2 left-1/2 z-10 max-w-[80%] -translate-x-1/2 bg-black/35 text-white/80 px-2.5 py-1 text-[10px] rounded backdrop-blur-sm border border-white/5 hidden pointer-events-none truncate opacity-55 transition-all duration-200 group-hover:bg-black/70 group-hover:text-white group-hover:border-white/10 group-hover:opacity-100" id="label-${i}"></div>
                <button onclick="event.stopPropagation(); clearCell(${i})" class="absolute top-2 right-2 z-20 hidden h-7 w-7 items-center justify-center rounded bg-black/65 text-white/80 border border-white/10 backdrop-blur-md opacity-0 pointer-events-none transition-all duration-200 group-hover:opacity-100 group-hover:pointer-events-auto hover:bg-red-500 hover:text-white" id="close-cell-${i}" title="关闭该窗口">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path></svg>
                </button>
            </div>
        `;
        grid.insertAdjacentHTML('beforeend', cellHtml);

        if (cellData[i]) {
            if (cellData[i].codecNotice) {
                showCodecNoticeInCell(i, cellData[i]);
            } else {
                executePlayInCell(
                    i,
                    cellData[i].source,
                    cellData[i].isLive,
                    cellData[i].title,
                    Boolean(cellData[i].forceNative),
                    cellData[i].warningMsg || null,
                    {seekSeconds: cellData[i].seekSeconds}
                );
            }
        }
    }
    updateFocusUI();
    refreshPTZPanel();
}

function closeActiveCell() {
    clearCell(activeCell);
}

function clearCell(index) {
    stopCellPlayback(index);
    cellData[index] = null;
    if (index === activeCell) {
        syncSelectedRecordFromActiveCell();
    }

    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const dplayerContainer = document.getElementById(`dplayer-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);
    const emptyState = document.getElementById(`empty-state-${index}`);
    const label = document.getElementById(`label-${index}`);
    const closeBtn = document.getElementById(`close-cell-${index}`);

    if (liveIframe) liveIframe.classList.add('hidden');
    if (dplayerContainer) dplayerContainer.classList.add('hidden');
    if (nativePlayer) nativePlayer.classList.add('hidden');
    if (emptyState) {
        resetEmptyState(index);
        emptyState.classList.remove('hidden');
        emptyState.classList.add('pointer-events-none');
    }
    if (label) {
        label.classList.add('hidden');
        label.innerText = '';
    }
    if (closeBtn) {
        closeBtn.classList.add('hidden');
        closeBtn.classList.remove('flex');
    }
    updateFocusUI();
    refreshPTZPanel();
}

function clearCurrentRecordPlayback() {
    let targetCell = -1;
    const activeData = cellData[activeCell];

    if (activeData && !activeData.isLive) {
        targetCell = activeCell;
    } else if (selectedRecordPath) {
        targetCell = cellData.findIndex(data => data && !data.isLive && data.recordPath === selectedRecordPath);
    }

    if (targetCell >= 0) {
        clearCell(targetCell);
        setSelectedRecordPath('');
        return true;
    }

    setSelectedRecordPath('');
    return false;
}

function stopCellPlayback(index) {
    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);

    if (dpInstances[index]) {
        dpInstances[index].destroy();
        dpInstances[index] = null;
    }
    if (nativePlayer) {
        nativePlayer.pause();
        nativePlayer.removeAttribute('src');
        nativePlayer.load();
    }
    if (liveIframe) {
        closeLiveIframe(liveIframe);
    }
}

function stopAllCellPlayback() {
    for (let i = 0; i < dpInstances.length; i++) {
        stopCellPlayback(i);
    }
}

function closeLiveIframe(liveIframe) {
    try {
        if (liveIframe.contentWindow) {
            liveIframe.contentWindow.location.replace('about:blank');
        }
    } catch (e) {
        // 如果浏览器阻止访问 iframe window，继续用 src 导航触发卸载。
    }
    liveIframe.src = 'about:blank';
}

function setActiveCell(index) {
    activeCell = index;
    syncSelectedRecordFromActiveCell();
    updateFocusUI();
    refreshPTZPanel();
}

function updateFocusUI() {
    for (let i = 0; i < currentLayout; i++) {
        const cell = document.getElementById(`cell-${i}`);
        if (cell) {
            if (currentLayout === 1) {
                cell.classList.remove('border-blue-500', 'shadow-[inset_0_0_20px_rgba(59,130,246,0.3)]', 'hover:border-gray-600');
                cell.classList.add('border-gray-800');
            } else if (i === activeCell) {
                cell.classList.add('border-blue-500', 'shadow-[inset_0_0_20px_rgba(59,130,246,0.3)]');
                cell.classList.remove('border-gray-800');
            } else {
                cell.classList.remove('border-blue-500', 'shadow-[inset_0_0_20px_rgba(59,130,246,0.3)]');
                cell.classList.add('border-gray-800');
            }
        }
    }
    const focusTitle = cellData[activeCell] ? cellData[activeCell].title : '空闲';
    document.getElementById('currentCam').innerText = `[窗口 ${activeCell + 1}] ${focusTitle}`;
}

function selectCamera(camId) {
    currentSelectedCam = camId;
    loadStatus();
    loadRecords(camId);
}

function previewLive(camId) {
    currentSelectedCam = camId;
    loadStatus();
    playVideo(camId, true, `🟢 直播: ${camId}`);
    loadRecords(camId);
}

function playVideo(source, isLive, title) {
    cellData[activeCell] = {source, isLive, title, camId: isLive ? source : ''};
    if (isLive) {
        setSelectedRecordPath('');
    }
    executePlayInCell(activeCell, source, isLive, title);

    if (isLive && currentLayout > 1) {
        let nextCell = (activeCell + 1) % currentLayout;
        setActiveCell(nextCell);
    } else {
        updateFocusUI();
    }
    refreshPTZPanel(isLive);
}

function getActiveLiveCamId() {
    if (currentLayout !== 1) return '';
    const data = cellData[activeCell];
    if (!data || !data.isLive) return '';
    return String(data.camId || data.source || '').trim();
}

async function refreshPTZPanel(force = false) {
    if (!canAdmin()) {
        window.PTZ?.hidePanel?.();
        return;
    }
    if (window.PTZ && typeof window.PTZ.refreshPanel === 'function') {
        await window.PTZ.refreshPanel({force});
    }
}

function getRecordPath(file) {
    return String((file && (file.path || file.url || file.name)) || '').trim();
}

function setSelectedRecordPath(path) {
    selectedRecordPath = String(path || '').trim();
    applySelectedRecordCardStyles();
}

function syncSelectedRecordFromActiveCell() {
    const activeData = cellData[activeCell];
    const path = activeData && !activeData.isLive ? activeData.recordPath : '';
    setSelectedRecordPath(path || '');
}

function isCurrentRecordPlaybackRequest(index, recordPath) {
    const data = cellData[index];
    return Boolean(data && !data.isLive && data.recordPath === recordPath);
}

function applySelectedRecordCardStyles() {
    document.querySelectorAll('[data-record-path]').forEach(item => {
        setRecordItemSelected(item, selectedRecordPath !== '' && item.dataset.recordPath === selectedRecordPath);
    });
    applySelectedRecordAxisStyles();
}

function applySelectedRecordAxisStyles() {
    document.querySelectorAll('[data-record-axis-path]').forEach(axis => {
        axis.classList.toggle('is-selected', selectedRecordPath !== '' && axis.dataset.recordAxisPath === selectedRecordPath);
    });
}

function setRecordItemSelected(item, isSelected) {
    item.classList.toggle('border-slate-200', !isSelected);
    item.classList.toggle('bg-white', !isSelected);
    item.classList.toggle('border-emerald-400', isSelected);
    item.classList.toggle('bg-emerald-50/70', isSelected);
    item.classList.toggle('ring-2', isSelected);
    item.classList.toggle('ring-emerald-200/70', isSelected);
    item.classList.toggle('shadow-[0_8px_20px_-12px_rgba(16,185,129,0.7)]', isSelected);
    item.setAttribute('aria-current', isSelected ? 'true' : 'false');

    const indicator = item.querySelector('[data-record-playing]');
    if (indicator) indicator.classList.toggle('hidden', !isSelected);
}

async function playRecord(file, title, options = {}) {
    const targetCell = activeCell;
    const recordPath = getRecordPath(file);
    const seekSeconds = normalizeSeekSeconds(options.seekSeconds);
    const lowerFileName = file.name.toLowerCase();
    const isMP4Record = lowerFileName.endsWith('.mp4');
    const isMergedMP4 = isMP4Record && file.name.includes('_merged');
    setSelectedRecordPath(recordPath);
    showProbeLoadingInCell(targetCell, title, recordPath);

    try {
        // 1. 先探测编码。合并 MP4 也可能是 H.265，不能无条件走原生播放。
        const resp = await fetch(`/api/record/probe?path=${encodeURIComponent(file.path)}`);
        const probe = await resp.json();
        if (!isCurrentRecordPlaybackRequest(targetCell, recordPath)) return;

        if (isMergedMP4) {
            if (probe.can_probe && probe.is_h265 && !browserSupportsHEVC()) {
                showRecordPlaybackRestricted(
                    targetCell,
                    title,
                    recordPath,
                    '当前设备或浏览器不支持 H.265 合并录像播放。'
                );
                return;
            }

            playMergedRecordNative(targetCell, file, title, recordPath, seekSeconds);
            return;
        }

        // 2. 普通 MP4 碎片已经按 faststart 点播文件输出，支持 HEVC 时直接交给原生播放器。
        if (probe.can_probe && probe.is_h265 && isMP4Record) {
            if (!browserSupportsHEVC()) {
                showRecordPlaybackRestricted(
                    targetCell,
                    title,
                    recordPath,
                    '当前设备或浏览器不支持 H.265 MP4 录像播放。'
                );
                return;
            }

            playRecordNative(targetCell, file.url, title, recordPath, seekSeconds);
            return;
        }

        // 3. H.265 TS/其他碎片需要重封装或拦截
        if (probe.can_probe && probe.is_h265) {
            const isAppleNative = isAppleNativePlayback();
            const supportHEVC = browserSupportsHEVC();

            if (isAppleNative || !supportHEVC) {
                // 直接拦截并抛出不支持的 UI 提示，不再走转码逻辑
                showRecordPlaybackRestricted(
                    targetCell,
                    title,
                    recordPath,
                    '当前设备或浏览器不支持 H.265 录像片段播放。'
                );
                return;
            }

            // 设备支持 H.265 时，强制走 fMP4 重封装！
            const remuxUrl = `/play_remux/${encodeURI(file.path)}`;

            // 传 true 强制使用原生的 <video> 标签播放 mp4 流
            const warningMsg = "当前为H.265片段的实时重封装播放，浏览器内不支持拖拽定位。";
            cellData[targetCell] = {source: remuxUrl, isLive: false, title, recordPath, seekSeconds, forceNative: true, warningMsg};
            executePlayInCell(targetCell, remuxUrl, false, title, true, warningMsg, {seekSeconds});
            updateFocusUI();
            return; // 直接返回，拦截默认的 TS 播放逻辑
        }
    } catch (e) {
        console.warn('编码探测失败，尝试直接播放:', e);
    }
    if (!isCurrentRecordPlaybackRequest(targetCell, recordPath)) return;

    if (isMergedMP4) {
        playMergedRecordNative(targetCell, file, title, recordPath, seekSeconds);
        return;
    }

    // 非 H.265 的 .ts 碎片，走默认播放逻辑 (HLS 或 mpegts)
    cellData[targetCell] = {source: file.url, isLive: false, title, recordPath, seekSeconds};
    // 不强制使用原生播放器
    executePlayInCell(targetCell, file.url, false, title, false, null, {seekSeconds});
    updateFocusUI();
}

function playMergedRecordNative(targetCell, file, title, recordPath, seekSeconds) {
    playRecordNative(targetCell, file.url, title, recordPath, seekSeconds);
}

function playRecordNative(targetCell, source, title, recordPath, seekSeconds) {
    cellData[targetCell] = {source, isLive: false, title, recordPath, seekSeconds, forceNative: true};
    executePlayInCell(targetCell, source, false, title, true, null, {seekSeconds});
    updateFocusUI();
}

function showRecordPlaybackRestricted(targetCell, title, recordPath, message) {
    stopCellPlayback(targetCell);
    cellData[targetCell] = {source: '', isLive: false, title: `${title} · 播放受限`, recordPath};

    const emptyState = document.getElementById(`empty-state-${targetCell}`);
    const label = document.getElementById(`label-${targetCell}`);
    const closeBtn = document.getElementById(`close-cell-${targetCell}`);

    if (label) {
        label.classList.remove('hidden');
        label.innerText = `${title} · 限制`;
    }
    if (closeBtn) {
        closeBtn.classList.remove('hidden');
        closeBtn.classList.add('flex');
    }
    if (emptyState) {
        emptyState.classList.remove('hidden');
        emptyState.classList.remove('pointer-events-none');
        emptyState.innerHTML = `
            <div class="max-w-[86%] rounded-lg border border-red-500/30 bg-black/70 px-4 py-3 text-center text-white shadow-xl backdrop-blur-md">
                <div class="text-sm font-bold mb-1 text-red-400">⚠️ 播放受限</div>
                <div class="text-xs leading-relaxed text-white/80">
                    ${message}
                </div>
            </div>
        `;
    }
    updateFocusUI();
}

function playRecordAtTime(file, title, offsetSeconds = 0) {
    return playRecord(file, title, {seekSeconds: offsetSeconds});
}

function normalizeSeekSeconds(value) {
    const seconds = Number(value);
    if (!Number.isFinite(seconds) || seconds <= 0) return 0;
    return Math.max(0, Math.floor(seconds));
}

function browserSupportsHEVC() {
    const video = document.createElement('video');
    const candidates = ['video/mp4; codecs="hvc1.1.6.L93.B0"', 'video/mp4; codecs="hev1.1.6.L93.B0"'];
    return candidates.some(type => video.canPlayType(type) !== '');
}

function isAppleNativePlayback() {
    const platform = navigator.platform || '';
    const userAgent = navigator.userAgent || '';
    const maxTouchPoints = navigator.maxTouchPoints || 0;

    const isIOSDevice = /^(iPhone|iPad|iPod)$/.test(platform) || (platform === 'MacIntel' && maxTouchPoints > 1);
    if (isIOSDevice) return true;

    const isMacOS = /^Mac/.test(platform) || /Macintosh|Mac OS X/i.test(userAgent);
    const isSafari = /Safari/i.test(userAgent) && !/(Chrome|Chromium|CriOS|FxiOS|Edg|OPR|OPiOS)/i.test(userAgent);
    return isMacOS && isSafari;
}

function isMobilePlayback() {
    return window.matchMedia('(max-width: 767px), (pointer: coarse)').matches;
}

function showProbeLoadingInCell(index, title, recordPath = '') {
    stopCellPlayback(index);
    cellData[index] = {source: '', isLive: false, title: `检测编码: ${title}`, recordPath};

    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const dplayerContainer = document.getElementById(`dplayer-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);
    const emptyState = document.getElementById(`empty-state-${index}`);
    const label = document.getElementById(`label-${index}`);
    const closeBtn = document.getElementById(`close-cell-${index}`);

    if (liveIframe) liveIframe.classList.add('hidden');
    if (dplayerContainer) dplayerContainer.classList.add('hidden');
    if (nativePlayer) nativePlayer.classList.add('hidden');
    if (label) {
        label.classList.remove('hidden');
        label.innerText = title;
    }
    if (closeBtn) {
        closeBtn.classList.remove('hidden');
        closeBtn.classList.add('flex');
    }
    if (emptyState) {
        emptyState.classList.remove('hidden');
        emptyState.classList.add('pointer-events-none');
        emptyState.innerHTML = `
            <div class="rounded-lg border border-white/10 bg-black/45 px-4 py-2 text-xs font-bold text-white/80 backdrop-blur-md">
                正在检测编码...
            </div>
        `;
    }
    updateFocusUI();
}

function showCodecNoticeInCell(index, data) {
    stopCellPlayback(index);

    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const dplayerContainer = document.getElementById(`dplayer-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);
    const emptyState = document.getElementById(`empty-state-${index}`);
    const label = document.getElementById(`label-${index}`);
    const closeBtn = document.getElementById(`close-cell-${index}`);

    if (!emptyState) return;

    if (liveIframe) liveIframe.classList.add('hidden');
    if (dplayerContainer) dplayerContainer.classList.add('hidden');
    if (nativePlayer) nativePlayer.classList.add('hidden');
    if (label) {
        label.classList.remove('hidden');
        label.innerText = `${data.title} · H.265`;
    }
    if (closeBtn) {
        closeBtn.classList.remove('hidden');
        closeBtn.classList.add('flex');
    }

    emptyState.classList.remove('hidden');
    emptyState.classList.remove('pointer-events-none');
    // 判断浏览器原生是否支持硬解 H.265
    const supportHEVC = browserSupportsHEVC();

    emptyState.innerHTML = `
        <div class="max-w-[86%] rounded-lg border border-white/10 bg-black/55 px-4 py-3 text-center text-white shadow-xl backdrop-blur-md">
            <div class="text-sm font-bold mb-1">当前录像为 H.265 原画</div>
            <div class="text-[10px] leading-relaxed text-white/75 mb-3">
                ${supportHEVC ? '✨ 您的设备支持 H.265 硬件解码，推荐使用原画播放，CPU 零损耗。' : '⚠️ 当前浏览器不支持直接解码。建议使用按需转码(消耗服务器性能)。'}
            </div>
            <div class="flex flex-col space-y-2 items-center">
                ${supportHEVC ? `
                <button onclick="event.stopPropagation(); playRemuxRecord(${index})" class="w-full rounded-md bg-green-600 px-3 py-1.5 text-xs font-bold text-white shadow hover:bg-green-500 active:scale-95 transition-all">
                    ▶ 原画播放 (硬解推荐)
                </button>
                ` : ''}
                ${data.canTranscode ? `
                <button onclick="event.stopPropagation(); playTranscodedRecord(${index})" class="${supportHEVC ? 'bg-gray-600 hover:bg-gray-500' : 'bg-blue-600 hover:bg-blue-500'} w-full rounded-md px-3 py-1.5 text-xs font-bold text-white shadow active:scale-95 transition-all">
                    ${supportHEVC ? '备用：服务器转码播放' : '▶ 兼容模式 (服务器转码)'}
                </button>
                ` : ''}
            </div>
        </div>
    `;
}

// 增加对应的播放函数
function playRemuxRecord(index) {
    const data = cellData[index];
    if (!data || !data.file) return;

    activeCell = index;
    // 指向新的 fMP4 重封装后端接口
    const remuxUrl = `/play_remux/${encodeURI(data.file.path)}`;
    const title = `硬解播放: ${data.title}`;
    cellData[index] = {source: remuxUrl, isLive: false, title};

    // 这种模式下，直接交给原生 <video> 标签播放
    executePlayInCell(index, remuxUrl, false, title, true);
    updateFocusUI();
}

function playTranscodedRecord(index) {
    const data = cellData[index];
    if (!data || !data.file) return;

    activeCell = index;
    const transcodeUrl = `/play_transcode/${encodeURI(data.file.path)}`;
    const title = `转码播放: ${data.title}`;
    cellData[index] = {source: transcodeUrl, isLive: false, title};
    executePlayInCell(index, transcodeUrl, false, title);
    updateFocusUI();
}

function executePlayInCell(index, source, isLive, title, forceNative = false, warningMsg = null, options = {}) {
    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const dplayerContainer = document.getElementById(`dplayer-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);
    const emptyState = document.getElementById(`empty-state-${index}`);
    const label = document.getElementById(`label-${index}`);
    const closeBtn = document.getElementById(`close-cell-${index}`);
    const seekSeconds = normalizeSeekSeconds(options.seekSeconds);

    if (!liveIframe) return;

    // 警告提示挂载逻辑
    let existingWarning = document.getElementById(`cell-warning-${index}`);
    if (existingWarning) existingWarning.remove(); // 清除之前的残留警告

    if (warningMsg) {
        const cell = document.getElementById(`cell-${index}`);
        const warningEl = document.createElement('div');
        warningEl.id = `cell-warning-${index}`;
        // 居中显示在顶部，加入 Tailwind 动画和毛玻璃效果，鼠标穿透(pointer-events-none)不阻挡点击
        warningEl.className = 'absolute top-5 left-1/2 -translate-x-1/2 z-30 max-w-[calc(100%-2rem)] bg-amber-500/90 text-white px-3 py-1.5 text-xs rounded-full shadow-[0_0_15px_rgba(245,158,11,0.4)] font-bold inline-flex items-center justify-center text-center leading-snug pointer-events-none transition-all duration-1000';
        warningEl.innerHTML = `
            <svg class="w-4 h-4 mr-1.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
            </svg>
            <span class="whitespace-normal text-center sm:whitespace-nowrap">${warningMsg}</span>
        `;
        cell.appendChild(warningEl);

        // 3 秒后自动触发淡出动画并移除，避免永久遮挡画面
        setTimeout(() => {
            const el = document.getElementById(`cell-warning-${index}`);
            if (el) {
                el.classList.add('opacity-0', '-translate-y-2');
                setTimeout(() => el.remove(), 1000);
            }
        }, 3000);
    }

    stopCellPlayback(index);
    resetEmptyState(index);
    emptyState.classList.add('hidden');
    emptyState.classList.add('pointer-events-none');
    label.classList.remove('hidden');
    label.innerText = title;
    if (closeBtn) {
        closeBtn.classList.remove('hidden');
        closeBtn.classList.add('flex');
    }

    if (isLive) {
        dplayerContainer.classList.add('hidden');
        nativePlayer.classList.add('hidden');
        liveIframe.classList.remove('hidden');
        liveIframe.src = `/stream.html?src=${encodeURIComponent(source)}`;
    } else {
        liveIframe.classList.add('hidden');
        liveIframe.src = '';

        const isAppleNative = isAppleNativePlayback();
        const isTranscodedStream = source.startsWith('/play_transcode/');
        const isRemuxStream = source.startsWith('/play_remux/');

        // 如果强制原生，或是转码/重封装流，或在苹果上，都用原生播放器
        if (forceNative || isRemuxStream || (isAppleNative && !isTranscodedStream)) {
            dplayerContainer.classList.add('hidden');
            nativePlayer.classList.remove('hidden');
            let playUrl = source;
            // 只有原始的 TS 文件才去走 HLS，转码和重封装的流绝对不能改前缀
            if (source.endsWith('.ts') && !isRemuxStream && !isTranscodedStream) {
                playUrl = source.replace('/play/', '/play_hls/');
            }
            nativePlayer.src = playUrl;
            if (seekSeconds > 0) {
                nativePlayer.addEventListener('loadedmetadata', function handleSeek() {
                    nativePlayer.removeEventListener('loadedmetadata', handleSeek);
                    try {
                        const duration = Number.isFinite(nativePlayer.duration) ? nativePlayer.duration : 0;
                        nativePlayer.currentTime = duration > 0
                            ? Math.min(seekSeconds, Math.max(0, duration - 0.5))
                            : seekSeconds;
                    } catch (e) {
                        console.log('seek failed', e);
                    }
                });
            }
            nativePlayer.play().catch(e => console.log("等待交互播放"));
        } else {
            nativePlayer.classList.add('hidden');
            dplayerContainer.classList.remove('hidden');
            let videoType = source.endsWith('.ts') ? 'customTs' : 'normal';

            dpInstances[index] = new DPlayer({
                container: dplayerContainer, video: {
                    url: source, type: videoType, customType: {
                        customTs: function (video, player) {
                            const tsPlayer = mpegts.createPlayer({type: 'm2ts', isLive: false, url: video.src});
                            tsPlayer.attachMediaElement(video);
                            tsPlayer.load();
                            tsPlayer.play();
                            player.events.on('destroy', () => {
                                tsPlayer.destroy();
                            });
                        }
                    }
                }
            });
            if (seekSeconds > 0) {
                const trySeek = () => {
                    const instance = dpInstances[index];
                    const video = instance && instance.video;
                    if (!video || !Number.isFinite(video.duration)) return false;
                    const nextTime = Math.min(Math.max(0, seekSeconds), Math.max(0, video.duration - 0.5));
                    try {
                        video.currentTime = nextTime;
                        return true;
                    } catch (e) {
                        return false;
                    }
                };
                const timer = setInterval(() => {
                    if (!dpInstances[index]) {
                        clearInterval(timer);
                        return;
                    }
                    if (trySeek()) clearInterval(timer);
                }, 120);
                setTimeout(() => clearInterval(timer), 4000);
            }
        }
    }
    refreshPTZPanel();
}

function resetEmptyState(index) {
    const emptyState = document.getElementById(`empty-state-${index}`);
    if (!emptyState) return;

    emptyState.innerHTML = `
        <svg class="w-8 h-8 mb-2 opacity-30" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
        <span class="text-xs font-bold tracking-wider uppercase opacity-50">窗口 ${index + 1}</span>
    `;
}

// --- 历史录像逻辑 ---
function initRecordRangeControls() {
    const startInput = document.getElementById('recordStartDate');
    const endInput = document.getElementById('recordEndDate');
    if (!startInput || !endInput) return;

    startInput.addEventListener('change', syncRecordRangeLimits);
    endInput.addEventListener('change', syncRecordRangeLimits);
    syncRecordRangeLimits();
    updateRecordRangeStatus();
}

function syncRecordRangeLimits() {
    const startInput = document.getElementById('recordStartDate');
    const endInput = document.getElementById('recordEndDate');
    if (!startInput || !endInput) return;

    const todayKey = formatDateKey(new Date());
    startInput.removeAttribute('min');
    endInput.removeAttribute('min');
    startInput.max = todayKey;
    endInput.max = todayKey;

    const startDate = parseLocalDate(startInput.value);
    const endDate = parseLocalDate(endInput.value);

    if (startDate) {
        endInput.min = startInput.value;
        const maxEndKey = formatDateKey(addDays(startDate, maxRecordRangeDays - 1));
        endInput.max = minDateKey(maxEndKey, todayKey);
    }
    if (endDate) {
        const minStartKey = formatDateKey(addDays(endDate, -(maxRecordRangeDays - 1)));
        startInput.min = minStartKey;
        startInput.max = minDateKey(endInput.value, todayKey);
    }
}

function applyRecordRange() {
    const startInput = document.getElementById('recordStartDate');
    const endInput = document.getElementById('recordEndDate');
    if (!startInput || !endInput) return;

    const start = startInput.value;
    const end = endInput.value;
    const validationError = validateRecordRange(start, end);
    if (validationError) {
        alert(validationError);
        return;
    }

    selectedRecordRange = {start, end};
    recordArchiveOpenDates.clear();
    syncRecordRangeLimits();
    updateRecordRangeStatus();
    if (currentSelectedCam) {
        loadRecords(currentSelectedCam);
    } else {
        renderRecordSelectionPrompt('请选择一个实时节点后再查询录像');
    }
}

function resetRecordRange() {
    const startInput = document.getElementById('recordStartDate');
    const endInput = document.getElementById('recordEndDate');
    selectedRecordRange = {start: '', end: ''};
    recordArchiveOpenDates.clear();

    if (startInput) startInput.value = '';
    if (endInput) endInput.value = '';
    syncRecordRangeLimits();
    updateRecordRangeStatus();

    if (currentSelectedCam) {
        loadRecords(currentSelectedCam);
    } else {
        renderRecordSelectionPrompt();
    }
}

function renderRecordSelectionPrompt(message = '请先选择一个实时节点') {
    const list = document.getElementById('recordList');
    const countBadge = document.getElementById('recordCount');
    if (countBadge) countBadge.innerText = '未选择设备';
    if (!list) return;

    list.className = 'space-y-2';
    list.innerHTML = `
        <div class="rounded-lg border border-dashed border-slate-200 bg-slate-50 px-4 py-10 text-center">
            <div class="mx-auto mb-2 flex h-9 w-9 items-center justify-center rounded-full bg-white text-slate-300 shadow-sm">
                <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7h8M8 11h8m-8 4h5M5 4h14a2 2 0 012 2v12a2 2 0 01-2 2H5a2 2 0 01-2-2V6a2 2 0 012-2z"></path>
                </svg>
            </div>
            <div class="text-sm font-bold text-slate-600">${escapeHtml(message)}</div>
            <div class="mt-1 text-xs font-medium text-slate-400">选中左侧设备后，这里会显示对应的历史录像档案。</div>
        </div>
    `;
}

function validateRecordRange(start, end) {
    if (!start || !end) return '请选择起止日期';

    const startDate = parseLocalDate(start);
    const endDate = parseLocalDate(end);
    if (!startDate || !endDate) return '日期格式有误';
    if (endDate < startDate) return '结束日期不能早于开始日期';
    if (recordRangeDaySpan(start, end) > maxRecordRangeDays) {
        return `日期范围最多支持连续 ${maxRecordRangeDays} 天`;
    }
    return '';
}

function updateRecordRangeStatus() {
    const status = document.getElementById('recordRangeStatus');
    if (!status) return;
    if (selectedRecordRange.start && selectedRecordRange.end) {
        status.innerText = selectedRecordRange.start === selectedRecordRange.end
            ? selectedRecordRange.start
            : `${selectedRecordRange.start} 至 ${selectedRecordRange.end}`;
    } else {
        status.innerText = '最近7个录像日';
    }
}

function buildRecordsUrl(camId) {
    const params = new URLSearchParams();
    if (selectedRecordRange.start && selectedRecordRange.end) {
        params.set('start', selectedRecordRange.start);
        params.set('end', selectedRecordRange.end);
    }
    const query = params.toString();
    return `/api/records/${encodeURIComponent(camId)}${query ? `?${query}` : ''}`;
}

function getRecordArchiveGroupKey(camId, date) {
    return `${camId}:${date}`;
}

function getRecordArchiveViewMode(camId, date) {
    return recordArchiveViewModes.get(getRecordArchiveGroupKey(camId, date)) || 'cards';
}

function setRecordArchiveViewMode(camId, date, mode) {
    const nextMode = ['cards', 'timeline'].includes(mode) ? mode : 'cards';
    recordArchiveViewModes.set(getRecordArchiveGroupKey(camId, date), nextMode);
}

function createRecordViewSwitch(camId, date, onChange) {
    const switcher = document.createElement('div');
    switcher.className = 'flex h-7 overflow-hidden rounded-md border border-slate-200 bg-white p-0.5 shadow-sm';
    switcher.dataset.recordViewSwitch = 'true';

    [
        {mode: 'cards', label: '卡片列表', title: '卡片列表'},
        {mode: 'timeline', label: '时间轴列表', title: '时间轴列表'}
    ].forEach(option => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.dataset.recordViewMode = option.mode;
        btn.className = 'rounded px-2 text-[11px] font-bold leading-none transition-colors';
        btn.textContent = option.label;
        btn.title = option.title;
        btn.onclick = (event) => {
            event.stopPropagation();
            setRecordArchiveViewMode(camId, date, option.mode);
            onChange();
        };
        switcher.appendChild(btn);
    });

    updateRecordViewSwitchButtons(switcher, getRecordArchiveViewMode(camId, date));
    return switcher;
}

function createRecordTimeline24hAction(camId, date, entries) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'record24h-dock-action';
    btn.dataset.record24hActionKey = getRecordArchiveGroupKey(camId, date);
    btn.title = '将 24H 时间轴吸附到播放器下方';
    btn.innerHTML = `
        <svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M4 6h16M4 12h16M4 18h16"></path>
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M8 4v16"></path>
        </svg>
        <span>24H</span>
    `;
    const updateState = () => {
        const active = activeRecordTimeline24hDockKey === getRecordArchiveGroupKey(camId, date);
        btn.classList.toggle('is-active', active);
        btn.title = active ? '24H 时间轴已吸附，点击查看' : '将 24H 时间轴吸附到播放器下方';
        btn.querySelector('span').textContent = active ? '24H 已吸附' : '24H';
    };
    btn.onclick = (event) => {
        event.stopPropagation();
        if (!window.RecordTimeline24h) {
            alert('24H 时间轴组件加载失败');
            return;
        }
        if (activeRecordTimeline24hDockKey === getRecordArchiveGroupKey(camId, date)) {
            snapRecordTimeline24hDockBelowPlayer();
            return;
        }
        renderRecordTimeline24hDock(camId, date, entries, selectedRecordPath);
        updateState();
    };
    updateState();
    return btn;
}

function updateRecordViewSwitchButtons(switcher, activeMode) {
    switcher.querySelectorAll('[data-record-view-mode]').forEach(btn => {
        const active = btn.dataset.recordViewMode === activeMode;
        btn.classList.toggle('bg-slate-800', active);
        btn.classList.toggle('text-white', active);
        btn.classList.toggle('shadow-sm', active);
        btn.classList.toggle('text-slate-500', !active);
        btn.classList.toggle('hover:bg-slate-100', !active);
        btn.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
}

function getRecordTimeline24hDock() {
    return document.getElementById('recordTimeline24hDock');
}

function clearRecordTimeline24hDock(groupKey = '') {
    const dock = getRecordTimeline24hDock();
    if (!dock) return;
    if (groupKey && activeRecordTimeline24hDockKey !== groupKey) return;

    dock.innerHTML = '';
    dock.classList.add('hidden');
    activeRecordTimeline24hDockKey = '';
    refreshRecordTimeline24hActionStates();
}

function renderRecordTimeline24hDock(camId, date, entries, selectedRecordPath) {
    const dock = getRecordTimeline24hDock();
    if (!dock || !window.RecordTimeline24h) return false;

    const groupKey = getRecordArchiveGroupKey(camId, date);
    dock.innerHTML = '';
    dock.classList.remove('hidden');
    dock.appendChild(window.RecordTimeline24h.create({
        camId,
        date,
        entries,
        selectedRecordPath,
        initialViewportWidth: dock.getBoundingClientRect().width || dock.clientWidth || 0,
        onPlayAtTime: ({entry, offsetSeconds, timeLabel}) => {
            if (!entry || !entry.file) return;
            playRecordAtTime(entry.file, `回放: ${camId} (${timeLabel || entry.meta.timeDisplay})`, offsetSeconds);
        },
        onClearPlayback: () => {
            clearCurrentRecordPlayback();
        }
    }));
    activeRecordTimeline24hDockKey = groupKey;
    refreshRecordTimeline24hActionStates();
    requestAnimationFrame(snapRecordTimeline24hDockBelowPlayer);
    return true;
}

function refreshRecordTimeline24hActionStates() {
    document.querySelectorAll('[data-record24h-action-key]').forEach(btn => {
        const active = btn.dataset.record24hActionKey === activeRecordTimeline24hDockKey;
        btn.classList.toggle('is-active', active);
        btn.title = active ? '24H 时间轴已吸附，点击查看' : '将 24H 时间轴吸附到播放器下方';
        const label = btn.querySelector('span');
        if (label) label.textContent = active ? '24H 已吸附' : '24H';
    });
}

function snapRecordTimeline24hDockBelowPlayer() {
    const dock = getRecordTimeline24hDock();
    const wrapper = document.getElementById('video-wrapper');
    if (!dock || !wrapper || dock.classList.contains('hidden')) return;

    const dockRect = dock.getBoundingClientRect();
    const viewportHeight = window.innerHeight || document.documentElement.clientHeight || 0;
    if (dockRect.top >= 0 && dockRect.bottom <= viewportHeight) return;

    const targetTop = window.scrollY + wrapper.getBoundingClientRect().top - 8;
    window.scrollTo({
        top: Math.max(0, targetTop),
        behavior: 'smooth'
    });
}

function renderRecordDateContent(content, camId, date, entries, viewMode, onUpdate = () => {}) {
    content.innerHTML = '';
    if (viewMode === 'timeline') {
        content.className = 'record-watermark-window bg-slate-50/70 p-2 custom-scrollbar';
        if (!window.RecordTimeline) {
            const error = document.createElement('div');
            error.className = 'rounded-lg border border-red-100 bg-red-50 px-4 py-8 text-center text-sm font-bold text-red-400';
            error.textContent = '时间轴组件加载失败';
            content.appendChild(error);
            return;
        }
        if (activeRecordTimeline24hDockKey === getRecordArchiveGroupKey(camId, date)) {
            renderRecordTimeline24hDock(camId, date, entries, selectedRecordPath);
        }
        content.appendChild(window.RecordTimeline.create({
            camId,
            date,
            entries,
            selectedRecordPath,
            onUpdate,
            renderItem: (entry, options = {}) => createRecordItem(camId, entry.file, entry.meta, {
                explicitActions: true,
                timeline: Boolean(options.timeline)
            })
        }));
        return;
    }
    if (activeRecordTimeline24hDockKey === getRecordArchiveGroupKey(camId, date)) {
        renderRecordTimeline24hDock(camId, date, entries, selectedRecordPath);
    }
    content.className = 'record-watermark-window max-h-[360px] overflow-y-auto bg-slate-50/60 p-2 custom-scrollbar sm:max-h-[460px]';
    const fileGrid = document.createElement('div');
    fileGrid.className = 'relative z-[1] grid grid-cols-1 gap-1.5 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5';

    entries.forEach(entry => {
        fileGrid.appendChild(createRecordItem(camId, entry.file, entry.meta));
    });
    content.appendChild(fileGrid);
}

function addDays(date, days) {
    const next = new Date(date);
    next.setDate(next.getDate() + days);
    return next;
}

function minDateKey(a, b) {
    return a <= b ? a : b;
}

function recordRangeDaySpan(start, end) {
    const startUTC = dateKeyToUTC(start);
    const endUTC = dateKeyToUTC(end);
    return Math.floor((endUTC - startUTC) / 86400000) + 1;
}

function dateKeyToUTC(dateKey) {
    const parts = dateKey.split('-').map(Number);
    return Date.UTC(parts[0], parts[1] - 1, parts[2]);
}

async function loadRecords(camId) {
    const list = document.getElementById('recordList');
    const countBadge = document.getElementById('recordCount');
    clearRecordTimeline24hDock();
    updateRecordRangeStatus();
    list.className = 'space-y-2';
    list.innerHTML = `
        <div class="rounded-lg border border-slate-200 bg-white px-4 py-8 text-center text-sm font-medium text-slate-400 shadow-sm">
            正在检索历史录像...
        </div>
    `;

    try {
        const resp = await fetch(buildRecordsUrl(camId));
        if (!resp.ok) {
            let message = '获取录像列表失败';
            try {
                const err = await resp.json();
                if (err && err.error) message = err.error;
            } catch (_) {
            }
            throw new Error(message);
        }
        const files = await resp.json();
        list.innerHTML = '';

        if (!files || files.length === 0) {
            countBadge.innerText = '0 个文件';
            const emptyTitle = selectedRecordRange.start && selectedRecordRange.end ? '该日期范围暂无录像' : '该设备暂无历史录像';
            list.innerHTML = `
                <div class="rounded-lg border border-dashed border-slate-200 bg-slate-50 px-4 py-10 text-center">
                    <div class="mx-auto mb-2 flex h-9 w-9 items-center justify-center rounded-full bg-white text-slate-300 shadow-sm">
                        <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"></path>
                        </svg>
                    </div>
                    <div class="text-sm font-bold text-slate-500">${emptyTitle}</div>
                </div>
            `;
            return;
        }

        const groups = groupRecordsByDate(files);
        const totalBytes = files.reduce((sum, file) => sum + parseRecordSizeBytes(file.size), 0);
        countBadge.innerText = `${files.length} 个文件 · ${formatRecordSize(totalBytes)}`;

        const sortedDates = Object.keys(groups).sort((a, b) => b.localeCompare(a));
        const hasOpenDate = sortedDates.some(date => recordArchiveOpenDates.has(`${camId}:${date}`));
        sortedDates.forEach((date, index) => {
            const entries = groups[date].sort((a, b) => a.meta.sortKey.localeCompare(b.meta.sortKey));
            const groupKey = getRecordArchiveGroupKey(camId, date);
            const isOpen = recordArchiveOpenDates.has(groupKey) || (index === 0 && !hasOpenDate);
            if (isOpen) recordArchiveOpenDates.add(groupKey);

            const groupDiv = document.createElement('div');
            groupDiv.className = 'overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm transition-shadow hover:shadow-md';

            const dateBytes = entries.reduce((sum, entry) => sum + parseRecordSizeBytes(entry.file.size), 0);
            const header = document.createElement('div');
            header.className = 'flex w-full items-stretch justify-between gap-2 border-b border-slate-100 transition-colors hover:bg-slate-50';

            const summaryBtn = document.createElement('button');
            summaryBtn.type = 'button';
            summaryBtn.className = 'flex min-w-0 flex-1 items-center gap-3 px-3 py-2 text-left';
            summaryBtn.innerHTML = `
                <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                        <span class="text-[13px] font-extrabold tracking-tight text-slate-800">${archiveDateTitle(date)}</span>
                        <span class="rounded-full bg-blue-50 px-2 py-0.5 text-[10px] font-bold text-blue-600 ring-1 ring-blue-100">${entries.length} 段</span>
                    </div>
                    <div class="mt-0.5 text-[11px] font-medium text-slate-400">${archiveDateSubTitle(date)} · ${formatRecordSize(dateBytes)}</div>
                </div>
            `;

            const content = document.createElement('div');
            let viewSwitch;
            const redrawDateContent = () => {
                const keepHidden = content.classList.contains('hidden');
                renderRecordDateContent(content, camId, date, entries, getRecordArchiveViewMode(camId, date), redrawDateContent);
                if (keepHidden) content.classList.add('hidden');
                if (viewSwitch) updateRecordViewSwitchButtons(viewSwitch, getRecordArchiveViewMode(camId, date));
                applySelectedRecordCardStyles();
            };

            viewSwitch = createRecordViewSwitch(camId, date, redrawDateContent);
            const timeline24hAction = createRecordTimeline24hAction(camId, date, entries);

            const collapseBtn = document.createElement('button');
            collapseBtn.type = 'button';
            collapseBtn.className = 'flex h-full items-center px-2 text-slate-400 transition-colors hover:text-slate-700';
            collapseBtn.title = isOpen ? '收起该日录像' : '展开该日录像';
            collapseBtn.innerHTML = `
                <svg class="h-4 w-4 shrink-0 transition-transform ${isOpen ? 'rotate-90' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path>
                </svg>
            `;

            const rightTools = document.createElement('div');
            rightTools.className = 'flex shrink-0 items-center gap-1 pr-2';
            rightTools.appendChild(viewSwitch);
            rightTools.appendChild(timeline24hAction);
            rightTools.appendChild(collapseBtn);

            const setOpen = (nextOpen) => {
                content.classList.toggle('hidden', !nextOpen);
                collapseBtn.querySelector('svg').classList.toggle('rotate-90', nextOpen);
                collapseBtn.title = nextOpen ? '收起该日录像' : '展开该日录像';
                if (nextOpen) {
                    recordArchiveOpenDates.add(groupKey);
                    redrawDateContent();
                } else {
                    recordArchiveOpenDates.delete(groupKey);
                    clearRecordTimeline24hDock(groupKey);
                }
            };
            summaryBtn.onclick = () => setOpen(content.classList.contains('hidden'));
            collapseBtn.onclick = (event) => {
                event.stopPropagation();
                setOpen(content.classList.contains('hidden'));
            };

            header.appendChild(summaryBtn);
            header.appendChild(rightTools);
            if (isOpen) {
                redrawDateContent();
            } else {
                content.classList.add('hidden');
            }

            groupDiv.appendChild(header);
            groupDiv.appendChild(content);
            list.appendChild(groupDiv);
        });
        applySelectedRecordCardStyles();
    } catch (e) {
        countBadge.innerText = '加载失败';
        list.innerHTML = '';
        const errorDiv = document.createElement('div');
        errorDiv.className = 'rounded-xl border border-red-100 bg-red-50 px-5 py-10 text-center text-sm font-bold text-red-400';
        errorDiv.textContent = e.message || '获取录像列表失败';
        list.appendChild(errorDiv);
    }
}

function groupRecordsByDate(files) {
    return files.reduce((groups, file) => {
        const meta = parseRecordMeta(file);
        if (!groups[meta.date]) groups[meta.date] = [];
        groups[meta.date].push({file, meta});
        return groups;
    }, {});
}

function parseRecordMeta(file) {
    const name = file.name || '';
    const path = file.path || name;
    const dateMatch = path.match(/\d{4}-\d{2}-\d{2}/);
    const date = dateMatch ? dateMatch[0] : '其他归档';
    const timeParts = parseRecordTimeParts(name, path);
    const ext = (name.split('.').pop() || '').toUpperCase();
    const isMotion = /_motion(?:\.|_merged)/i.test(name);
    const isMerged = /_merged\./i.test(name);
    const isTimelapse = /_timelapse\./i.test(name);
    const timeDisplay = timeParts && timeParts.start ? `${timeParts.start.hourText}:${timeParts.start.minuteText}:${timeParts.start.secondText}` : (isMerged ? name : '整段录像');
    const sortKey = timeParts && timeParts.start ? `${timeParts.start.hourText}${timeParts.start.minuteText}${timeParts.start.secondText}_${name}` : name;
    const kind = isMotion ? '动检' : isTimelapse ? '延时' : isMerged ? '合并' : '切片';
    const kindClass = isMotion
        ? 'bg-amber-50 text-amber-700 ring-amber-100'
        : isTimelapse
            ? 'bg-purple-50 text-purple-700 ring-purple-100'
            : isMerged
                ? 'bg-emerald-50 text-emerald-700 ring-emerald-100'
                : 'bg-slate-100 text-slate-500 ring-slate-200';
    const iconClass = isMotion
        ? 'bg-amber-50 text-amber-600 ring-amber-100'
        : isTimelapse
            ? 'bg-purple-50 text-purple-600 ring-purple-100'
            : isMerged
                ? 'bg-emerald-50 text-emerald-600 ring-emerald-100'
                : 'bg-blue-50 text-blue-600 ring-blue-100';

    return {
        date,
        timeDisplay,
        sortKey,
        ext,
        kind,
        kindClass,
        iconClass,
        hasStartTime: Boolean(timeParts && timeParts.start),
        startSeconds: timeParts && timeParts.start ? timeParts.start.startSeconds : null,
        hasEndTime: Boolean(timeParts && timeParts.end),
        endSeconds: timeParts && timeParts.end ? timeParts.end.startSeconds : null,
        timelineEndSeconds: timeParts ? timeParts.timelineEndSeconds : null,
        estimatedEndSeconds: timeParts ? timeParts.estimatedEndSeconds : null
    };
}

function parseRecordTimeParts(name, path) {
    const text = `${path || ''}/${name || ''}`;

    // 新格式: CamID_YYYYMMDD_HHMMSS_HHMMSS.ext
    const newFormat = text.match(/_(\d{8})_(\d{2})(\d{2})(\d{2})_(\d{6}|[a-z]+)(?:\.|_)/i);
    if (newFormat) {
        const start = normalizeRecordTimeParts(newFormat[2], newFormat[3], newFormat[4]);
        const end = parseRecordTimeToken(newFormat[5]);
        return start ? {
            start,
            end,
            timelineEndSeconds: normalizeTimelineEndSeconds(start.startSeconds, end ? end.startSeconds : null),
            estimatedEndSeconds: end ? end.startSeconds : start.startSeconds + recordTimelineFallbackDurationSeconds
        } : null;
    }

    // 旧格式: YYYY-MM-DD_HH-MM-SS
    const dashed = text.match(/\d{4}-\d{2}-\d{2}_(\d{2})-(\d{2})-(\d{2})/);
    if (dashed) {
        const start = normalizeRecordTimeParts(dashed[1], dashed[2], dashed[3]);
        return start ? {
            start,
            end: null,
            timelineEndSeconds: normalizeTimelineEndSeconds(start.startSeconds, null),
            estimatedEndSeconds: start.startSeconds + recordTimelineFallbackDurationSeconds
        } : null;
    }

    // 旧格式: YYYY-MM-DD_HHMMSS
    const compact = text.match(/\d{4}-\d{2}-\d{2}_(\d{2})(\d{2})(\d{2})/);
    if (compact) {
        const start = normalizeRecordTimeParts(compact[1], compact[2], compact[3]);
        return start ? {
            start,
            end: null,
            timelineEndSeconds: normalizeTimelineEndSeconds(start.startSeconds, null),
            estimatedEndSeconds: start.startSeconds + recordTimelineFallbackDurationSeconds
        } : null;
    }

    // 旧格式: YYYY-MM-DD_HH (小时合并)
    const hourOnly = text.match(/\d{4}-\d{2}-\d{2}_(\d{2})(?:_|\.|$)/);
    if (hourOnly) {
        const start = normalizeRecordTimeParts(hourOnly[1], '00', '00');
        return start ? {
            start,
            end: null,
            timelineEndSeconds: normalizeTimelineEndSeconds(start.startSeconds, null),
            estimatedEndSeconds: start.startSeconds + recordTimelineFallbackDurationSeconds
        } : null;
    }

    return null;
}

function parseRecordTimeToken(token) {
    const text = String(token || '').trim();
    if (/^\d{6}$/.test(text)) {
        return normalizeRecordTimeParts(text.slice(0, 2), text.slice(2, 4), text.slice(4, 6));
    }
    return null;
}

function normalizeTimelineEndSeconds(startSeconds, endSeconds) {
    const fallbackEnd = startSeconds + recordTimelineFallbackDurationSeconds;
    const rawEnd = Number.isFinite(endSeconds) ? endSeconds : fallbackEnd;
    if (rawEnd <= startSeconds) return 86400;
    return Math.min(86400, rawEnd);
}

function normalizeRecordTimeParts(hourText, minuteText, secondText) {
    const hour = Number(hourText);
    const minute = Number(minuteText);
    const second = Number(secondText);
    if (!Number.isInteger(hour) || !Number.isInteger(minute) || !Number.isInteger(second)) return null;
    if (hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59) return null;
    return {
        hourText: String(hour).padStart(2, '0'),
        minuteText: String(minute).padStart(2, '0'),
        secondText: String(second).padStart(2, '0'),
        startSeconds: hour * 3600 + minute * 60 + second
    };
}

function createRecordItem(camId, file, meta, options = {}) {
    const item = document.createElement('div');
    const recordPath = getRecordPath(file);
    const explicitActions = Boolean(options.explicitActions);
    const timeline = Boolean(options.timeline);
    const clickPlaysRecord = !explicitActions || timeline;
    const cursorClass = clickPlaysRecord ? 'cursor-pointer' : 'cursor-default';
    const actionVisibilityClass = explicitActions
        ? 'opacity-100'
        : 'opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100';
    item.className = timeline
        ? `group record-timeline-card ${cursorClass} rounded-lg border border-slate-200 bg-white shadow-sm transition-all hover:border-blue-300 hover:shadow active:scale-[0.99]`
        : `group flex ${cursorClass} items-center justify-between gap-2 rounded-lg border border-slate-200 bg-white px-2 py-1.5 shadow-sm transition-all hover:border-blue-300 hover:shadow active:scale-[0.99]`;
    item.dataset.recordPath = recordPath;
    item.onclick = () => {
        if (!clickPlaysRecord) {
            setSelectedRecordPath(recordPath);
            return;
        }
        playRecord(file, `回放: ${camId} (${meta.timeDisplay})`);
    };

    const fileIcon = `
        <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M7 3.75h7.25L19 8.5V18.5A1.75 1.75 0 0117.25 20.25H7A1.75 1.75 0 015.25 18.5v-13A1.75 1.75 0 017 3.75z"></path>
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M14.25 3.75V8.5H19"></path>
            <path fill="currentColor" stroke="none" d="M10.25 11.05a.55.55 0 01.83-.47l3.35 2.02a.55.55 0 010 .94l-3.35 2.02a.55.55 0 01-.83-.47v-4.04z"></path>
        </svg>
    `;
    const playAction = explicitActions && !timeline ? `
        <button data-record-action="play" class="rounded-md p-1.5 text-slate-300 transition-colors hover:bg-emerald-50 hover:text-emerald-600" title="播放该录像" aria-label="播放该录像">
            <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 5v14l11-7-11-7z"></path>
            </svg>
        </button>
    ` : '';
    const downloadAction = `
        <button data-record-action="download" class="${timeline ? 'record-timeline-card-action record-timeline-card-action-download' : 'rounded-md p-1.5 text-slate-300 transition-colors hover:bg-blue-50 hover:text-blue-600'}" title="下载该录像" aria-label="下载该录像">
            <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1M8 12l4 4m0 0l4-4m-4 4V4"></path>
            </svg>
        </button>
    `;
    const deleteAction = canAdmin() ? `
        <button data-record-action="delete" class="${timeline ? 'record-timeline-card-action record-timeline-card-action-delete' : 'rounded-md p-1.5 text-slate-300 transition-colors hover:bg-red-50 hover:text-red-500'}" title="永久删除该录像" aria-label="永久删除该录像">
            <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path>
            </svg>
        </button>
    ` : '';

    item.innerHTML = timeline ? `
        <div class="record-timeline-card-head">
            <div class="record-timeline-card-icon ${meta.iconClass}">
                ${fileIcon}
            </div>
            <div class="record-timeline-card-time">${meta.timeDisplay}</div>
            <div class="record-timeline-card-actions">
                ${playAction}
                ${downloadAction}
                ${deleteAction}
            </div>
        </div>
        <div class="record-timeline-card-meta">
            <span class="record-timeline-card-kind ${meta.kindClass}">${meta.kind}</span>
            <span data-record-playing class="hidden record-timeline-card-playing rounded-full bg-emerald-100 px-1.5 py-0.5 text-[9px] font-bold leading-none text-emerald-700 ring-1 ring-emerald-200">播放中</span>
            <span class="record-timeline-card-detail">${meta.ext}</span>
            <span class="record-timeline-card-detail">${file.size}</span>
        </div>
    ` : `
        <div class="flex min-w-0 flex-1 items-center gap-2">
            <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md ring-1 shadow-sm ${meta.iconClass}">
                ${fileIcon}
            </div>
            <div class="min-w-0 flex-1">
                <div class="truncate font-mono text-[12px] font-extrabold leading-4 text-slate-800">${meta.timeDisplay}</div>
                <div class="mt-0.5 flex flex-wrap items-center gap-1">
                    <span class="rounded-full px-1.5 py-0.5 text-[9px] font-bold leading-none ring-1 ${meta.kindClass}">${meta.kind}</span>
                    <span data-record-playing class="hidden rounded-full bg-emerald-100 px-1.5 py-0.5 text-[9px] font-bold leading-none text-emerald-700 ring-1 ring-emerald-200">播放中</span>
                    <span class="font-mono text-[9px] font-medium leading-none text-slate-400">${meta.ext}</span>
                    <span class="font-mono text-[9px] font-medium leading-none text-slate-400">${file.size}</span>
                </div>
            </div>
        </div>
        <div class="flex shrink-0 items-center gap-0.5 ${actionVisibilityClass}">
            ${playAction}
            ${downloadAction}
            ${deleteAction}
        </div>
    `;

    const playBtn = item.querySelector('[data-record-action="play"]');
    const downloadBtn = item.querySelector('[data-record-action="download"]');
    const deleteBtn = item.querySelector('[data-record-action="delete"]');
    if (playBtn) {
        playBtn.onclick = (event) => {
            event.stopPropagation();
            playRecord(file, `回放: ${camId} (${meta.timeDisplay})`);
        };
    }
    downloadBtn.onclick = (event) => downloadRecord(event, file.path);
    if (deleteBtn) deleteBtn.onclick = (event) => deleteRecord(event, camId, file.path);
    setRecordItemSelected(item, selectedRecordPath !== '' && selectedRecordPath === recordPath);
    return item;
}

function archiveDateTitle(date) {
    if (date === '其他归档') return date;
    const today = new Date();
    const current = parseLocalDate(date);
    if (!current) return date;
    const todayKey = formatDateKey(today);
    const yesterday = new Date(today);
    yesterday.setDate(today.getDate() - 1);
    if (date === todayKey) return `${date} · 今天`;
    if (date === formatDateKey(yesterday)) return `${date} · 昨天`;
    return date;
}

function archiveDateSubTitle(date) {
    if (date === '其他归档') return '未识别日期';
    const current = parseLocalDate(date);
    if (!current) return '录像归档';
    const weekdays = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
    return weekdays[current.getDay()];
}

function parseLocalDate(date) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return null;
    const parts = date.split('-').map(Number);
    if (parts.length !== 3 || parts.some(Number.isNaN)) return null;
    const parsed = new Date(parts[0], parts[1] - 1, parts[2]);
    if (formatDateKey(parsed) !== date) return null;
    return parsed;
}

function formatDateKey(date) {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
}

function parseRecordSizeBytes(sizeText) {
    const match = String(sizeText || '').match(/([\d.]+)\s*(KB|MB|GB)/i);
    if (!match) return 0;
    const value = Number(match[1]);
    const unit = match[2].toUpperCase();
    if (Number.isNaN(value)) return 0;
    if (unit === 'GB') return value * 1024 * 1024 * 1024;
    if (unit === 'MB') return value * 1024 * 1024;
    return value * 1024;
}

function formatRecordSize(bytes) {
    if (!bytes) return '0 MB';
    if (bytes >= 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function downloadRecord(e, filePath) {
    e.stopPropagation();
    const link = document.createElement('a');
    link.href = `/api/record/download?path=${encodeURIComponent(filePath)}`;
    link.download = filePath.split('/').pop() || 'record';
    document.body.appendChild(link);
    link.click();
    link.remove();
}

async function deleteRecord(e, camId, filePath) {
    e.stopPropagation();
    if (!confirm('确定要永久删除此录像文件吗？释放的空间不可恢复。')) return;

    try {
        const resp = await fetch(`/api/record?path=${encodeURIComponent(filePath)}`, {method: 'DELETE'});
        if (resp.ok) {
            loadRecords(camId);
        } else {
            const err = await resp.json();
            alert('操作失败: ' + err.error);
        }
    } catch (err) {
        alert('网络请求失败，请检查连接状态');
    }
}

// 1. 矩阵整体全屏
function toggleMatrixFullscreen() {
    const wrapper = document.getElementById("video-wrapper");
    if (!document.fullscreenElement && !document.webkitFullscreenElement) {
        if (wrapper.requestFullscreen) {
            wrapper.requestFullscreen();
        } else if (wrapper.webkitRequestFullscreen) {
            wrapper.webkitRequestFullscreen();
        }
    } else {
        if (document.exitFullscreen) {
            document.exitFullscreen();
        } else if (document.webkitExitFullscreen) {
            document.webkitExitFullscreen();
        }
    }
}

// 2. 单个格子双击独立全屏
function toggleCellFullscreen(index) {
    const cell = document.getElementById(`cell-${index}`);
    // 阻止事件冒泡防止触发点击的焦点切换
    if (!document.fullscreenElement && !document.webkitFullscreenElement) {
        if (cell.requestFullscreen) cell.requestFullscreen(); else if (cell.webkitRequestFullscreen) cell.webkitRequestFullscreen();
    } else {
        if (document.exitFullscreen) document.exitFullscreen(); else if (document.webkitExitFullscreen) document.webkitExitFullscreen();
    }
}

function scheduleMatrixToolbarAutoHide(wrapper) {
    clearTimeout(matrixToolbarTimer);
    matrixToolbarTimer = setTimeout(() => {
        wrapper.classList.remove('matrix-toolbar-visible');
        wrapper.classList.add('matrix-toolbar-idle');
    }, 1600);
}

function revealMatrixToolbar() {
    const wrapper = document.getElementById('video-wrapper');
    if ((document.fullscreenElement || document.webkitFullscreenElement) !== wrapper) return;
    wrapper.classList.remove('matrix-toolbar-idle');
    wrapper.classList.add('matrix-toolbar-visible');
    scheduleMatrixToolbarAutoHide(wrapper);
}

// 3. 监听全局全屏状态变化，自动去圆角、去边框、切图标，达到完美沉浸感
['fullscreenchange', 'webkitfullscreenchange'].forEach(eventType => {
    document.addEventListener(eventType, () => {
        const enterIcon = document.getElementById('icon-fullscreen-enter');
        const exitIcon = document.getElementById('icon-fullscreen-exit');
        const fullscreenButton = document.getElementById('btn-matrix-fullscreen');
        const wrapper = document.getElementById('video-wrapper');
        const stage = document.getElementById('video-stage');
        const grid = document.getElementById('video-grid');
        const toolbar = document.getElementById('video-toolbar');

        const fullscreenElement = document.fullscreenElement || document.webkitFullscreenElement;
        const matrixFullscreen = fullscreenElement === wrapper;
        // 顶部按钮只代表多宫格全屏；单个播放窗口全屏时不切换这个图标。
        if (matrixFullscreen) {
            enterIcon.classList.add('hidden');
            exitIcon.classList.remove('hidden');
            fullscreenButton.title = '退出全屏 (Esc)';
            fullscreenButton.setAttribute('aria-label', '退出全屏');

            // 去除父容器圆角和边框，贴合显示器物理边缘
            wrapper.classList.remove('rounded-xl', 'border');
            wrapper.classList.add('rounded-none', 'border-0', 'h-screen', 'max-h-none');
            stage.classList.add('h-full');
            grid.classList.remove('p-1', 'gap-1');
            grid.classList.add('p-0', 'gap-0');
            wrapper.classList.remove('matrix-toolbar-idle');
            wrapper.classList.add('matrix-toolbar-visible');
            wrapper.addEventListener('mousemove', revealMatrixToolbar);
            toolbar.addEventListener('mouseenter', revealMatrixToolbar);
            toolbar.addEventListener('mouseleave', revealMatrixToolbar);
            toolbar.addEventListener('focusin', revealMatrixToolbar);
            scheduleMatrixToolbarAutoHide(wrapper);
        } else {
            clearTimeout(matrixToolbarTimer);
            enterIcon.classList.remove('hidden');
            exitIcon.classList.add('hidden');
            fullscreenButton.title = '多宫格全屏 (Esc退出)';
            fullscreenButton.setAttribute('aria-label', '多宫格全屏');

            wrapper.classList.add('rounded-xl', 'border');
            wrapper.classList.remove('rounded-none', 'border-0', 'h-screen', 'max-h-none');
            wrapper.classList.remove('matrix-toolbar-visible', 'matrix-toolbar-idle');
            wrapper.removeEventListener('mousemove', revealMatrixToolbar);
            toolbar.removeEventListener('mouseenter', revealMatrixToolbar);
            toolbar.removeEventListener('mouseleave', revealMatrixToolbar);
            toolbar.removeEventListener('focusin', revealMatrixToolbar);
            stage.classList.remove('h-full');
            grid.classList.add('p-1', 'gap-1');
            grid.classList.remove('p-0', 'gap-0');
        }
    });
});
