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
const recordMarkersByDateCache = new Map();
const recordMarkersRequestCache = new Map();
let latestBatchCameraPreview = {valid: [], invalid: []};
let activeRecordTimeline24hDockKey = '';
let recordTimeline24hRenderSeq = 0;
let matrixToolbarTimer = null;
let configPageVisible = false;
let recordListHeightLockToken = 0;
const authState = window.CAMKEEP_AUTH || {
    auth_enabled: false,
    can_admin: true,
    user: {role: 'admin', username: 'admin'},
    permissions: ['view', 'admin']
};

window.cameraCapabilityCache = window.cameraCapabilityCache || new Map();
window.cameraOnvifEventSummaryCache = window.cameraOnvifEventSummaryCache || new Map();
window.cameraOnvifEventHistoryCache = window.cameraOnvifEventHistoryCache || new Map();
window.cameraOnvifEventOverlayNoticeCache = window.cameraOnvifEventOverlayNoticeCache || new Map();
window.onvifEventOverlayEnabledCameras = window.onvifEventOverlayEnabledCameras || new Set();
window.onvifEventOverlayClientId = window.onvifEventOverlayClientId || createOnvifEventOverlayClientId();
const ONVIF_EVENT_OVERLAY_POLL_INTERVAL_MS = 3000;
const ONVIF_EVENT_OVERLAY_VISIBLE_MS = 15000;
const ONVIF_EVENT_OVERLAY_FADE_MS = 700;
const ONVIF_EVENT_OVERLAY_MAX_ITEMS = 3;
const ONVIF_EVENT_OVERLAY_NOTICE_VISIBLE_MS = 10000;
let onvifEventOverlayPollTimer = null;
let onvifEventOverlayPollCamId = '';
let onvifEventOverlayHideTimer = null;
let onvifEventOverlayFadeTimer = null;

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
    window.addEventListener('pagehide', () => stopOnvifEventSummaryPolling({beacon: true}));
    window.addEventListener('pagehide', releaseCameraCoverObjectURLs);
    window.addEventListener('beforeunload', stopAllCellPlayback);
    window.addEventListener('beforeunload', () => stopOnvifEventSummaryPolling({beacon: true}));
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

// 主题有两条独立的轴：
//   模式 mode  -> html.dark 类，取值 light / dark / system(跟随系统)
//   风格 skin  -> html[data-skin] 属性，取值 neu(新拟态) / classic(原始)，
//                classic 时整张禁用 neumorphism.css。
// 预绘制脚本（模板 <head>）已在首帧前应用初始值，这里只负责运行时切换与控件同步。
function getStoredMode() {
    const saved = localStorage.getItem('camkeep-theme');
    return saved === 'dark' || saved === 'light' ? saved : 'system';
}

function getStoredSkin() {
    return document.documentElement.dataset.skin === 'classic' ? 'classic' : 'neu';
}

function applyMode(mode) {
    const root = document.documentElement;
    if (mode === 'system') {
        localStorage.removeItem('camkeep-theme');
        const prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
        root.classList.toggle('dark', !!prefersDark);
    } else {
        const isDark = mode === 'dark';
        root.classList.toggle('dark', isDark);
        localStorage.setItem('camkeep-theme', isDark ? 'dark' : 'light');
    }
}

function applySkin(skin) {
    const normalized = skin === 'classic' ? 'classic' : 'neu';
    document.documentElement.dataset.skin = normalized;
    const link = document.getElementById('neuStyle');
    if (link) link.disabled = normalized === 'classic';
    localStorage.setItem('camkeep-skin', normalized);
}

function setMode(mode) {
    applyMode(mode);
    syncThemeSegments();
}

function setSkin(skin) {
    applySkin(skin);
    syncThemeSegments();
}

function syncThemeSegments() {
    const mode = getStoredMode();
    const skin = getStoredSkin();
    document.querySelectorAll('#themeMenuPanel .theme-seg-btn').forEach(btn => {
        const active = btn.dataset.mode === mode || btn.dataset.skin === skin;
        btn.classList.toggle('is-active', active);
        btn.setAttribute('aria-checked', active ? 'true' : 'false');
    });
}

function toggleThemeMenu(event) {
    event?.stopPropagation();
    const panel = document.getElementById('themeMenuPanel');
    const willOpen = panel ? panel.classList.contains('hidden') : false;
    setThemeMenuOpen(willOpen);
}

function setThemeMenuOpen(open) {
    const button = document.getElementById('themeMenuButton');
    const panel = document.getElementById('themeMenuPanel');
    if (!button || !panel) return;
    panel.classList.toggle('hidden', !open);
    button.classList.toggle('is-open', open);
    button.setAttribute('aria-expanded', open ? 'true' : 'false');
}

function initThemeControls() {
    syncThemeSegments();

    const menu = document.getElementById('themeMenu');
    if (menu) {
        document.addEventListener('click', event => {
            if (!menu.contains(event.target)) setThemeMenuOpen(false);
        });
        document.addEventListener('keydown', event => {
            if (event.key === 'Escape') setThemeMenuOpen(false);
        });
    }

    // 仅在「跟随系统」时响应系统配色变化。
    if (window.matchMedia) {
        const media = window.matchMedia('(prefers-color-scheme: dark)');
        media.addEventListener('change', () => {
            if (getStoredMode() === 'system') {
                document.documentElement.classList.toggle('dark', media.matches);
            }
        });
    }
}

async function initUpdateBadge() {
    const badge = document.getElementById('versionStatusBadge');
    if (!badge) return;
    const current = document.getElementById('appVersionText')?.textContent?.trim() || '';
    renderVersionStatus({
        status: canAdmin() ? 'checking' : 'current',
        current
    });
    if (!canAdmin()) return;

    try {
        const resp = await fetch('/api/update/check');
        if (!resp.ok) {
            renderVersionStatus({status: 'unknown', current});
            return;
        }

        const data = await resp.json();
        renderVersionStatus(data);
    } catch (e) {
        renderVersionStatus({status: 'unknown', current});
        console.warn('检查更新失败:', e);
    }
}

function renderVersionStatus(data) {
    const badge = document.getElementById('versionStatusBadge');
    if (!badge || !data) return;

    const current = String(data.current_version || data.currentVersion || document.getElementById('appVersionText')?.textContent || '').trim();
    const latest = String(data.latest_stable_version || '').trim();
    const releaseURL = String(data.release_url || 'https://github.com/r0n9/camkeep/releases/latest').trim();
    const channel = String(data.channel || '').trim().toLowerCase();
    const statusText = document.getElementById('versionStatusText');

    badge.href = releaseURL;
    badge.classList.remove('is-checking', 'is-current', 'is-update', 'is-reference', 'is-unknown');
    if (current) {
        const versionText = document.getElementById('appVersionText');
        if (versionText) versionText.textContent = current;
    }

    if (data.update_available && latest) {
        if (statusText) statusText.textContent = `可更新到 ${latest}`;
        badge.title = data.message || `发现新版本 ${latest}`;
        badge.classList.add('is-update');
        return;
    }

    if ((channel === 'dev' || channel === 'test') && latest) {
        if (statusText) statusText.textContent = `稳定版 ${latest}`;
        badge.title = data.message || '当前为开发/测试版本，不参与稳定版更新判断';
        badge.classList.add('is-reference');
        return;
    }

    if (data.status === 'checking') {
        if (statusText) statusText.textContent = '检查中';
        badge.title = '正在检查最新版本';
        badge.classList.add('is-checking');
        return;
    }

    if (data.status === 'unknown') {
        if (statusText) statusText.textContent = latest ? `最新 ${latest}` : '更新未知';
        badge.title = '暂时无法确认最新版本';
        badge.classList.add('is-unknown');
        return;
    }

    if (statusText) statusText.textContent = '最新版';
    badge.title = data.message || (latest ? `当前已是最新稳定版 ${latest}` : '当前已是最新版本');
    badge.classList.add('is-current');
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
const DEFAULT_CAMERA_MODE = 'normal';
const DEFAULT_RECORD_TIME = '00:00-23:59';
const DEFAULT_RETENTION_DAYS = 7;
const DEFAULT_SEGMENT_DURATION = 600;
const DEFAULT_MIN_SIZE_KB = 1024;
const DEFAULT_CAPTURE_INTERVAL = 1;
const DEFAULT_MOTION_RATIO_THRESHOLD = 0.01;
const DEFAULT_MOTION_EVENT_SOURCE = 'frame_diff';
const DEFAULT_MOTION_MARK_EVENT_SOURCE = 'auto';
const MOTION_EVENT_SOURCES = new Set(['frame_diff', 'onvif', 'auto']);
let configEditMode = 'form';
let configFormState = {daily_merge: {enabled: false, time: '03:30', merge_motion_records: false}, cameras: []};
let configFormInitialCameras = [];
let configFormInitialCamerasLoaded = false;
let go2rtcStreamInfoMap = new Map();
let configCameraExpandedKeys = new Set();
let configCameraUiSeq = 0;
let configOnvifDiagnosticsOpenKeys = new Set();
let onvifDiagnosticsCache = new Map();
let onvifDiagnosticsLoading = new Set();
let onvifEventTestState = new Map();
let onvifEventTestTimers = new Map();

async function openConfig() {
    if (!canAdmin()) return;
    showConfigPage();
    go2rtcStreamInfoMap = new Map();
    configCameraExpandedKeys = new Set();
    configOnvifDiagnosticsOpenKeys = new Set();
    onvifDiagnosticsCache = new Map();
    onvifDiagnosticsLoading = new Set();
    onvifEventTestTimers.forEach(timer => clearTimeout(timer));
    onvifEventTestTimers = new Map();
    onvifEventTestState = new Map();
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
        configOnvifDiagnosticsOpenKeys = new Set();
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
            managed: false,
            onvif_enabled: false
        };
    }

    return {
        id: String(readConfigValue(stream, ['id', 'ID'], fallbackID) || ''),
        source_label: readConfigValue(stream, ['source_label', 'SourceLabel'], ''),
        managed: Boolean(readConfigValue(stream, ['managed', 'Managed'], false)),
        onvif_enabled: Boolean(readConfigValue(stream, ['onvif_enabled', 'ONVIFEnabled'], false))
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
            time: readConfigValue(cfg.daily_merge, ['time', 'Time'], '03:30') || '03:30',
            merge_motion_records: Boolean(readConfigValue(cfg.daily_merge, ['merge_motion_records', 'MergeMotionRecords'], false))
        },
        cameras
    };
}

function normalizeConfigCamera(cam) {
    const streamURL = readConfigValue(cam, ['stream_url', 'StreamURL'], '');
    const legacyRTSPURL = readConfigValue(cam, ['rtsp_url', 'RTSPUrl'], '');
    const effectiveStreamURL = String(streamURL || '').trim() !== '' ? streamURL : legacyRTSPURL;
    const segmentDuration = readConfigNumber(cam, ['segment_duration', 'SegmentDuration'], DEFAULT_SEGMENT_DURATION);
    const minSizeKb = readConfigNumber(cam, ['min_size_kb', 'MinSizeKb'], DEFAULT_MIN_SIZE_KB);
    const captureInterval = readConfigNumber(cam, ['capture_interval', 'CaptureInterval'], DEFAULT_CAPTURE_INTERVAL);
    const motionRatio = readConfigNumber(cam, ['motionDetectRatioThreshold', 'MotionDetectRatioThreshold'], DEFAULT_MOTION_RATIO_THRESHOLD);

    return {
        id: readConfigValue(cam, ['id', 'ID'], ''),
        order: readConfigNumber(cam, ['order', 'Order'], 0),
        stream_url: effectiveStreamURL,
        motion_url: readConfigValue(cam, ['motion_url', 'MotionURL'], ''),
        retention_days: readConfigNumber(cam, ['retention_days', 'RetentionDays'], DEFAULT_RETENTION_DAYS),
        segment_duration: segmentDuration > 0 ? segmentDuration : DEFAULT_SEGMENT_DURATION,
        format: readConfigValue(cam, ['format', 'Format'], DEFAULT_RECORD_FORMAT) || DEFAULT_RECORD_FORMAT,
        min_size_kb: minSizeKb > 0 ? minSizeKb : DEFAULT_MIN_SIZE_KB,
        record_time: readConfigValue(cam, ['record_time', 'RecordTime'], DEFAULT_RECORD_TIME) || DEFAULT_RECORD_TIME,
        mode: readConfigValue(cam, ['mode', 'Mode'], DEFAULT_CAMERA_MODE) || DEFAULT_CAMERA_MODE,
        capture_interval: captureInterval > 0 ? captureInterval : DEFAULT_CAPTURE_INTERVAL,
        motion_detect: Boolean(readConfigValue(cam, ['motion_detect', 'MotionDetect'], false)),
        motion_event_source: normalizeMotionEventSource(readConfigValue(cam, ['motion_event_source', 'MotionEventSource'], DEFAULT_MOTION_EVENT_SOURCE)),
        motion_mark_enabled: Boolean(readConfigValue(cam, ['motion_mark_enabled', 'MotionMarkEnabled'], false)),
        motion_mark_event_source: normalizeMotionMarkEventSource(readConfigValue(cam, ['motion_mark_event_source', 'MotionMarkEventSource'], DEFAULT_MOTION_MARK_EVENT_SOURCE)),
        motionDetectRatioThreshold: motionRatio > 0 ? motionRatio : DEFAULT_MOTION_RATIO_THRESHOLD,
        auto_discovered: isManagedByGo2rtcURL(effectiveStreamURL) || Boolean(readConfigValue(cam, ['auto_discovered', 'AutoDiscovered'], false))
    };
}

function normalizeMotionEventSource(source) {
    source = String(source || '').trim().toLowerCase();
    return MOTION_EVENT_SOURCES.has(source) ? source : DEFAULT_MOTION_EVENT_SOURCE;
}

function normalizeMotionMarkEventSource(source) {
    source = String(source || '').trim().toLowerCase();
    return MOTION_EVENT_SOURCES.has(source) ? source : DEFAULT_MOTION_MARK_EVENT_SOURCE;
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

function extractConfigOnvifSource(source) {
    source = String(source || '').trim();
    if (!source) return '';
    if (source.toLowerCase().startsWith('onvif://')) return source;

    const colon = source.indexOf(':');
    const schemeSep = source.indexOf('://');
    if (colon > 0 && (schemeSep < 0 || colon < schemeSep)) {
        return extractConfigOnvifSource(source.slice(colon + 1));
    }
    return '';
}

function configCameraSupportsOnvif(cam) {
    if (!cam) return false;
    if (isManagedByGo2rtcURL(cam.stream_url)) {
        return go2rtcStreamInfoMap.get(String(cam.id || ''))?.onvif_enabled === true;
    }
    return Boolean(extractConfigOnvifSource(cam.stream_url));
}

function syncConfigOnvifDiagnosticsOpen(details) {
    if (!details || !details.dataset) return;
    const key = details.dataset.uiKey || '';
    if (!key) return;
    if (details.open) {
        configOnvifDiagnosticsOpenKeys.add(key);
        ensureOnvifDiagnosticsLoaded(details.dataset.camId || '');
    } else {
        configOnvifDiagnosticsOpenKeys.delete(key);
    }
}

function ensureOnvifDiagnosticsLoaded(camId) {
    camId = String(camId || '').trim();
    if (!camId || onvifDiagnosticsCache.has(camId) || onvifDiagnosticsLoading.has(camId)) return;
    void refreshOnvifDiagnostics(camId, {silent: true});
}

async function refreshOnvifDiagnostics(camId, options = {}) {
    camId = String(camId || '').trim();
    if (!camId) return;
    if (onvifDiagnosticsLoading.has(camId) && !options.force) return;

    onvifDiagnosticsLoading.add(camId);
    try {
        const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/onvif`);
        const payload = await resp.json().catch(() => ({}));
        if (!resp.ok) {
            onvifDiagnosticsCache.set(camId, {
                enabled: false,
                error: payload.error || 'ONVIF 事件诊断读取失败'
            });
        } else {
            onvifDiagnosticsCache.set(camId, payload);
        }
    } catch (e) {
        onvifDiagnosticsCache.set(camId, {
            enabled: false,
            error: 'ONVIF 事件诊断读取失败: ' + e.message
        });
    } finally {
        onvifDiagnosticsLoading.delete(camId);
        if (configPageVisible) rerenderConfigFormPreservingInput();
    }
}

async function startOnvifEventTest(camId) {
    camId = String(camId || '').trim();
    if (!camId) return;

    onvifEventTestState.set(camId, {starting: true});
    rerenderConfigFormPreservingInput();

    try {
        const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/onvif/event-test`, {method: 'POST'});
        const payload = await resp.json().catch(() => ({}));
        if (!resp.ok) {
            onvifEventTestState.set(camId, {error: payload.error || 'ONVIF PullPoint 测试监听启动失败'});
            rerenderConfigFormPreservingInput();
            return;
        }

        const expiresAt = Date.parse(payload.expires_at || '');
        const expiresAtMs = Number.isFinite(expiresAt) ? expiresAt : Date.now() + 30000;
        onvifEventTestState.set(camId, {
            expiresAtMs,
            message: payload.message || 'ONVIF PullPoint 测试监听已启动'
        });
        scheduleOnvifEventTestPolling(camId, expiresAtMs);
        await refreshOnvifDiagnostics(camId, {force: true, silent: true});
    } catch (e) {
        onvifEventTestState.set(camId, {error: 'ONVIF PullPoint 测试监听启动失败: ' + e.message});
        rerenderConfigFormPreservingInput();
    }
}

function scheduleOnvifEventTestPolling(camId, expiresAtMs) {
    const existing = onvifEventTestTimers.get(camId);
    if (existing) clearTimeout(existing);

    const tick = async () => {
        await refreshOnvifDiagnostics(camId, {force: true, silent: true});
        if (Date.now() < expiresAtMs) {
            onvifEventTestTimers.set(camId, setTimeout(tick, 3000));
            return;
        }
        onvifEventTestTimers.delete(camId);
        onvifEventTestState.delete(camId);
        if (configPageVisible) rerenderConfigFormPreservingInput();
    };

    onvifEventTestTimers.set(camId, setTimeout(tick, 1200));
}

function renderOnvifDiagnosticsSection(cam, uiKey) {
    if (!configCameraSupportsOnvif(cam)) return '';

    const camId = String(cam.id || '').trim();
    if (camId) ensureOnvifDiagnosticsLoaded(camId);

    const status = camId ? onvifDiagnosticsCache.get(camId) : null;
    const open = configOnvifDiagnosticsOpenKeys.has(uiKey);
    const stateView = onvifEventStateView(status, camId);
    const body = renderOnvifDiagnosticsBody(camId, status, stateView);

    return `
        <details class="config-onvif-diagnostics ${stateView.className}" data-ui-key="${escapeHtml(uiKey)}" data-cam-id="${escapeHtml(camId)}" ${open ? 'open' : ''} ontoggle="syncConfigOnvifDiagnosticsOpen(this)">
            <summary class="config-onvif-diagnostics-summary">
                <span class="config-onvif-diagnostics-icon">
                    <svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 3v4m0 10v4M5.64 5.64l2.83 2.83m7.06 7.06 2.83 2.83M3 12h4m10 0h4M5.64 18.36l2.83-2.83m7.06-7.06 2.83-2.83"></path><circle cx="12" cy="12" r="3.5" stroke-width="2.2"></circle></svg>
                </span>
                <span class="config-onvif-diagnostics-copy">
                    <strong>ONVIF 事件诊断</strong>
                    <em>确认 Event 服务、PullPoint 支持和最近收到的事件。</em>
                </span>
                <span class="config-onvif-diagnostics-state">${escapeHtml(stateView.label)}</span>
                <span class="config-onvif-diagnostics-chevron" aria-hidden="true">
                    <svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.3" d="M19 9l-7 7-7-7"></path></svg>
                </span>
            </summary>
            <div class="config-onvif-diagnostics-body">${body}</div>
        </details>
    `;
}

function onvifEventStateView(status, camId) {
    if (!camId) return {label: '保存后诊断', className: 'is-muted'};
    if (onvifDiagnosticsLoading.has(camId) && !status) return {label: '读取中', className: 'is-loading'};
    if (!status) return {label: '未读取', className: 'is-muted'};
    if (status.error) return {label: '读取失败', className: 'is-error'};

    const eventState = String(status.event_state || 'not_probed').toLowerCase();
    if (eventState === 'available' && status.pull_point_support === true) {
        const listenerState = String(status.event_listener_state || 'inactive').toLowerCase();
        if (listenerState === 'listening') {
            return onvifRecentPullHealthy(status.event_pull_last_success_at)
                ? {label: '事件就绪', className: 'is-ready'}
                : {label: '监听中', className: 'is-loading'};
        }
        if (listenerState === 'starting') return {label: '监听启动中', className: 'is-loading'};
        if (listenerState === 'error') return {label: '监听异常', className: 'is-error'};
        return {label: '待监听', className: 'is-warn'};
    }
    if (eventState === 'available') {
        return {label: '无 PullPoint', className: 'is-warn'};
    }
    if (eventState === 'probing' || eventState === 'not_probed') {
        return {label: eventState === 'probing' ? '探测中' : '未探测', className: 'is-loading'};
    }
    if (eventState === 'error') {
        return {label: '探测失败', className: 'is-error'};
    }
    return {label: '不可用', className: 'is-warn'};
}

function renderOnvifDiagnosticsBody(camId, status, stateView) {
    if (!camId) {
        return `<div class="config-onvif-diagnostics-message">保存并应用配置后，可读取该摄像头的 ONVIF Event 能力。</div>`;
    }
    if (onvifDiagnosticsLoading.has(camId) && !status) {
        return `<div class="config-onvif-diagnostics-message">正在读取 ONVIF 事件诊断...</div>`;
    }
    if (!status) {
        return `<div class="config-onvif-diagnostics-message">尚未读取诊断状态。</div>${renderOnvifDiagnosticsActions(camId, false)}`;
    }
    if (status.error) {
        return `
            <div class="config-onvif-diagnostics-message is-error">${escapeHtml(status.error)}</div>
            ${renderOnvifDiagnosticsActions(camId, false)}
        `;
    }

    const sourceKind = status.managed_by_go2rtc ? 'go2rtc 托管 ONVIF 源' : '直接 ONVIF 接入';
    const eventReady = status.event_state === 'available' && status.pull_point_support === true;
    const lastErrorText = status.event_listener_last_error || status.last_error || '';
    const lastError = lastErrorText ? `
        <div class="config-onvif-diagnostics-message is-error">${escapeHtml(lastErrorText)}</div>
    ` : '';

    return `
        <div class="config-onvif-diagnostics-grid">
            ${onvifDiagField('接入方式', sourceKind)}
            ${onvifDiagField('Event 服务', onvifStateText(status.event_state))}
            ${onvifDiagField('PullPoint', status.pull_point_support ? '支持' : '不支持')}
            ${onvifDiagField('监听状态', onvifListenerStateText(status.event_listener_state))}
            ${onvifDiagField('最后 Pull 成功', formatOnvifDateTime(status.event_pull_last_success_at))}
            ${onvifDiagField('Motion 验证', status.motion_event_verified ? '已验证' : '未验证')}
            ${onvifDiagField('最后探测', formatOnvifDateTime(status.updated_at))}
            ${onvifDiagField('Event XAddr', status.event_xaddr || '-', 'is-wide')}
            ${onvifDiagField('ONVIF 来源', status.source_url || '-', 'is-wide')}
        </div>
        ${lastError}
        ${renderOnvifLastEvent(status.last_event)}
        ${renderOnvifTestMessage(camId)}
        ${renderOnvifDiagnosticsActions(camId, eventReady)}
    `;
}

function onvifDiagField(label, value, extraClass = '') {
    return `
        <div class="config-onvif-diag-field ${extraClass}">
            <span>${escapeHtml(label)}</span>
            <strong>${escapeHtml(value || '-')}</strong>
        </div>
    `;
}

function renderOnvifLastEvent(event) {
    if (!event) {
        return `<div class="config-onvif-last-event is-empty">暂无收到的 ONVIF 事件。点击测试监听后触发摄像头事件，再观察这里是否更新。</div>`;
    }

    const typeClass = event.motion_topic ? 'is-motion' : 'is-generic';
    const typeText = event.motion ? 'motion' : (event.motion_topic ? 'motion topic' : 'event');
    return `
        <div class="config-onvif-last-event">
            <div class="config-onvif-last-event-head">
                <span class="config-onvif-last-event-type ${typeClass}">${typeText}</span>
                <strong>${escapeHtml(formatOnvifDateTime(event.at || event.received_at))}</strong>
            </div>
            <div class="config-onvif-last-event-topic">${escapeHtml(event.topic || '-')}</div>
            <div class="config-onvif-last-event-meta">
                ${event.operation ? `<span>op=${escapeHtml(event.operation)}</span>` : ''}
                ${event.source ? `<span>source=${escapeHtml(event.source)}</span>` : ''}
                ${event.key ? `<span>key=${escapeHtml(event.key)}</span>` : ''}
                ${event.data ? `<span>data=${escapeHtml(event.data)}</span>` : ''}
            </div>
        </div>
    `;
}

function renderOnvifTestMessage(camId) {
    const test = onvifEventTestState.get(camId);
    if (!test) return '';
    if (test.error) {
        return `<div class="config-onvif-diagnostics-message is-error">${escapeHtml(test.error)}</div>`;
    }
    if (test.starting) {
        return `<div class="config-onvif-diagnostics-message">正在启动 PullPoint 测试监听...</div>`;
    }
    if (test.expiresAtMs && Date.now() < test.expiresAtMs) {
        const remain = Math.max(1, Math.ceil((test.expiresAtMs - Date.now()) / 1000));
        return `<div class="config-onvif-diagnostics-message is-live">${escapeHtml(test.message || 'ONVIF PullPoint 测试监听中')}，剩余约 ${remain} 秒。</div>`;
    }
    return '';
}

function renderOnvifDiagnosticsActions(camId, canTest) {
    const test = onvifEventTestState.get(camId);
    const testActive = Boolean(test?.starting || (test?.expiresAtMs && Date.now() < test.expiresAtMs));
    const testDisabled = canTest && !testActive ? '' : 'disabled';
    const testTitle = canTest ? '启动 30 秒 ONVIF PullPoint 测试监听' : 'Event 可用且支持 PullPoint 后才能测试监听';
    return `
        <div class="config-onvif-diagnostics-actions">
            <button type="button" data-onvif-cam-id="${escapeHtml(camId)}" onclick="refreshOnvifDiagnostics(this.dataset.onvifCamId, {force: true})" class="config-onvif-secondary-btn">
                刷新状态
            </button>
            <button type="button" data-onvif-cam-id="${escapeHtml(camId)}" onclick="startOnvifEventTest(this.dataset.onvifCamId)" class="config-onvif-primary-btn" ${testDisabled} title="${escapeHtml(testTitle)}">
                ${testActive ? '监听中' : '测试监听 30 秒'}
            </button>
        </div>
    `;
}

function onvifStateText(state) {
    switch (String(state || '').toLowerCase()) {
        case 'available':
            return '可用';
        case 'probing':
            return '探测中';
        case 'unavailable':
            return '不可用';
        case 'error':
            return '错误';
        case 'not_probed':
            return '未探测';
        default:
            return state || '-';
    }
}

function onvifListenerStateText(state) {
    switch (String(state || '').toLowerCase()) {
        case 'listening':
            return '监听中';
        case 'starting':
            return '启动中';
        case 'error':
            return '异常';
        case 'inactive':
        case '':
            return '未监听';
        default:
            return state || '-';
    }
}

function onvifRecentPullHealthy(value) {
    if (!value || String(value).startsWith('0001-01-01')) return false;
    const pulledAt = Date.parse(value);
    if (!Number.isFinite(pulledAt)) return false;
    return Date.now() - pulledAt <= 90000;
}

function formatOnvifDateTime(value) {
    if (!value) return '-';
    if (String(value).startsWith('0001-01-01')) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '-';
    return date.toLocaleString('zh-CN', {hour12: false});
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
    document.getElementById('dailyMergeMotionRecords').checked = Boolean(configFormState.daily_merge.merge_motion_records);

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
    const uiKey = ensureConfigCameraUiKey(cam);
    const modeValue = String(cam.mode || 'normal').toLowerCase();
    const normalMode = modeValue !== 'timelapse';
    const managedByGo2rtc = isManagedByGo2rtcURL(cam.stream_url);
    const onvifCapable = configCameraSupportsOnvif(cam);
    const motionEnabled = normalMode && Boolean(cam.motion_detect);
    const motionMarkEnabled = normalMode && !motionEnabled && Boolean(cam.motion_mark_enabled);
    const motionEventSourceValue = motionEventSourceForCamera(cam, onvifCapable);
    const motionMarkEventSourceValue = motionMarkEventSourceForCamera(cam, onvifCapable);
    const motionEventSourceOptions = onvifCapable
        ? [['frame_diff', '本地帧差'], ['onvif', 'ONVIF'], ['auto', '自动']]
        : [['frame_diff', '本地帧差']];
    const motionMarkEventSourceOptions = onvifCapable
        ? [['auto', '自动'], ['onvif', 'ONVIF'], ['frame_diff', '本地帧差']]
        : [['frame_diff', '本地帧差']];
    const motionDetectAttrs = normalMode ? 'onchange="refreshConfigFormFromDom()"' : 'disabled';
    const motionMarkSourceAttrs = motionMarkEnabled ? `onchange="refreshConfigFormFromDom()"` : 'disabled title="先启用普通录制动检标记后再选择来源"';
    const motionUrlAttrs = normalMode ? '' : 'disabled title="延时模式不使用动检流"';
    const motionThresholdAttrs = motionEnabled ? '' : 'disabled title="先启用动检录制后再调整阈值"';
    const captureIntervalAttrs = normalMode ? 'disabled title="仅延时模式生效"' : '';
    const motionSectionTitle = normalMode ? '动检录制' : '延时摄影';
    const motionSectionDesc = normalMode
        ? '启用后仅事件片段会触发录像，阈值越低越敏感。'
        : '仅在延时模式下生效，动检项会被忽略。';
    const motionEventSourceNote = normalMode ? renderMotionEventSourceNote(motionEventSourceValue, onvifCapable) : '';
    const motionMarkControls = normalMode && !motionEnabled ? `
                    ${configCheckboxInput('普通录制动检标记', 'motion_mark_enabled', motionMarkEnabled, 'onchange="refreshConfigFormFromDom()"')}
                    ${configSelectInput('标记来源', 'motion_mark_event_source', motionMarkEventSourceValue, motionMarkEventSourceOptions, motionMarkSourceAttrs, false)}
    ` : '';
    const motionMarkEventSourceNote = normalMode && !motionEnabled ? renderMotionMarkEventSourceNote(motionMarkEventSourceValue, onvifCapable, motionEnabled) : '';
    const sourceHint = managedByGo2rtc ? '使用 go2rtc 同名流' : 'CamKeep 会把接入源注册到 go2rtc';
    const motionHint = normalMode ? (motionMarkEnabled ? '普通录像会生成动检时间轴标记' : '动检开启后仅事件录像') : '延时录像模式会忽略动检';
    const summaryChips = [
        managedByGo2rtc ? '<span class="config-camera-chip config-camera-chip--go2rtc">go2rtc 接管</span>' : '',
        onvifCapable ? '<span class="config-camera-chip config-camera-chip--onvif">ONVIF</span>' : '',
        motionEnabled ? '<span class="config-camera-chip config-camera-chip--motion">动检</span>' : '',
        motionMarkEnabled ? '<span class="config-camera-chip config-camera-chip--motion">动检标记</span>' : '',
        `<span class="config-camera-chip config-camera-chip--mode">${escapeHtml(modeValue || 'normal')} / ${escapeHtml(cam.format || DEFAULT_RECORD_FORMAT)}</span>`
    ].filter(Boolean).join('');
    return `
        <div class="config-camera-card-shell">
            <div class="config-camera-card-head">
                <button onclick="toggleConfigCamera(${index})" class="config-camera-card-summary" type="button" aria-expanded="${expanded ? 'true' : 'false'}">
                    <div class="config-camera-card-index">#${index + 1}</div>
                    <div class="config-camera-title-block">
                        <div class="config-camera-title-row">
                            <h4 class="truncate text-sm font-extrabold text-slate-800">${escapeHtml(cam.id || '未命名摄像头')}</h4>
                        </div>
                        <p class="mt-1 truncate text-[11px] font-medium text-slate-500">${sourceHint}；${motionHint}</p>
                    </div>
                    <span class="config-camera-chip-tray">${summaryChips}</span>
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
                    ${configSelectInput('事件源', 'motion_event_source', motionEventSourceValue, motionEventSourceOptions, `onchange="refreshConfigFormFromDom()"`, false)}
                    ${motionMarkControls}
                    ${configTextInput('动检流 motion_url', 'motion_url', cam.motion_url, '可选，低码率子码流，仅用于识别', false, 'config-field-wide', motionUrlAttrs)}
                    ${configHiddenInput('capture_interval', cam.capture_interval)}
                    ${configNumberInput('动检阈值', 'motionDetectRatioThreshold', cam.motionDetectRatioThreshold, '0.01 表示 1%', '0.001', '', motionThresholdAttrs)}
                    ${motionEventSourceNote}
                    ${motionMarkEventSourceNote}
                ` : `
                    ${configNumberInput('延时抓拍间隔', 'capture_interval', cam.capture_interval, '仅延时生效', '1', 'config-field-wide', captureIntervalAttrs)}
                    ${configHiddenInput('motion_url', cam.motion_url)}
                    ${configHiddenInput('motion_event_source', motionEventSourceValue)}
                    ${configHiddenInput('motion_mark_event_source', motionMarkEventSourceValue)}
                    ${configHiddenInput('motionDetectRatioThreshold', cam.motionDetectRatioThreshold)}
                    <input data-field="motion_detect" type="checkbox" ${cam.motion_detect ? 'checked' : ''} hidden>
                    <input data-field="motion_mark_enabled" type="checkbox" ${cam.motion_mark_enabled ? 'checked' : ''} hidden>
                    <div class="config-form-section-note config-field-wide">延时模式不使用动检录像，保留接入源与抓拍间隔即可。</div>
                `, normalMode ? 'config-form-section--motion' : 'config-form-section--motion config-form-section--timelapse is-muted')}
                ${renderOnvifDiagnosticsSection(cam, uiKey)}
            </div>
        </div>
    `;
}

function motionEventSourceForCamera(cam, onvifCapable) {
    const source = normalizeMotionEventSource(cam?.motion_event_source || DEFAULT_MOTION_EVENT_SOURCE);
    if (!onvifCapable && source !== DEFAULT_MOTION_EVENT_SOURCE) return DEFAULT_MOTION_EVENT_SOURCE;
    return source;
}

function motionMarkEventSourceForCamera(cam, onvifCapable) {
    const source = normalizeMotionMarkEventSource(cam?.motion_mark_event_source || DEFAULT_MOTION_MARK_EVENT_SOURCE);
    if (!onvifCapable && source !== DEFAULT_MOTION_EVENT_SOURCE) return DEFAULT_MOTION_EVENT_SOURCE;
    return source;
}

function renderMotionEventSourceNote(source, onvifCapable) {
    source = normalizeMotionEventSource(source || DEFAULT_MOTION_EVENT_SOURCE);
    let text = '';
    if (source === 'onvif') {
        text = 'ONVIF 模式只使用 PullPoint motion 事件；Event 不可用时不会启动本地帧差回退。';
    } else if (source === 'auto') {
        text = '自动模式会在 ONVIF Event 通道健康时优先使用 PullPoint，不健康时回退本地帧差。';
    } else {
        text = onvifCapable
            ? '本地帧差会读取 motion_url；留空时使用默认 go2rtc 流。'
            : '当前接入源不是 ONVIF，只支持本地帧差事件源。';
    }
    return `<div class="config-form-section-note config-field-wide">${escapeHtml(text)}</div>`;
}

function renderMotionMarkEventSourceNote(source, onvifCapable, motionEnabled) {
    if (motionEnabled) {
        return '<div class="config-form-section-note config-field-wide">动检录制模式下不会生成普通录制动检标记；需要完整录像索引时请关闭动检录制。</div>';
    }
    source = normalizeMotionMarkEventSource(source || DEFAULT_MOTION_MARK_EVENT_SOURCE);
    let text = '';
    if (source === 'onvif') {
        text = '普通录像仍按计划完整保存，时间轴只标记 ONVIF PullPoint motion 事件。';
    } else if (source === 'auto') {
        text = '普通录像仍按计划完整保存；ONVIF 可用时用 PullPoint 标记，不可用时回退本地帧差。';
    } else {
        text = onvifCapable
            ? '普通录像仍按计划完整保存，本地帧差只负责生成时间轴活动区间。'
            : '当前接入源不是 ONVIF，普通录制动检标记只支持本地帧差。';
    }
    return `<div class="config-form-section-note config-field-wide">${escapeHtml(text)}</div>`;
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
    configOnvifDiagnosticsOpenKeys = new Set();
    renderConfigForm();
}

function collectConfigForm(options = {}) {
    const cfg = {
        daily_merge: {
            enabled: document.getElementById('dailyMergeEnabled').checked,
            time: document.getElementById('dailyMergeTime').value || '03:30',
            merge_motion_records: document.getElementById('dailyMergeMotionRecords').checked
        },
        cameras: []
    };

    document.querySelectorAll('.config-camera-card').forEach((card, index) => {
        const mode = readCardField(card, 'mode') || DEFAULT_CAMERA_MODE;
        const cam = {
            id: readCardField(card, 'id').trim(),
            order: readCardNumber(card, 'order', 0),
            stream_url: readCardField(card, 'stream_url').trim(),
            motion_url: readCardField(card, 'motion_url').trim(),
            retention_days: readCardNumber(card, 'retention_days', 0),
            segment_duration: readCardNumber(card, 'segment_duration', 0),
            format: readCardField(card, 'format') || DEFAULT_RECORD_FORMAT,
            min_size_kb: readCardNumber(card, 'min_size_kb', 0),
            record_time: readCardField(card, 'record_time').trim() || DEFAULT_RECORD_TIME,
            mode,
            capture_interval: readCardNumber(card, 'capture_interval', 0),
            motion_detect: mode === 'normal' && readCardCheckbox(card, 'motion_detect'),
            motion_event_source: normalizeMotionEventSource(readCardField(card, 'motion_event_source')),
            motion_mark_enabled: mode === 'normal' && !readCardCheckbox(card, 'motion_detect') && readCardCheckbox(card, 'motion_mark_enabled'),
            motion_mark_event_source: normalizeMotionMarkEventSource(readCardField(card, 'motion_mark_event_source')),
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

function defaultConfigCameraSeed(overrides = {}) {
    return {
        id: '',
        order: 0,
        stream_url: '',
        motion_url: '',
        retention_days: DEFAULT_RETENTION_DAYS,
        segment_duration: DEFAULT_SEGMENT_DURATION,
        format: DEFAULT_RECORD_FORMAT,
        min_size_kb: DEFAULT_MIN_SIZE_KB,
        record_time: DEFAULT_RECORD_TIME,
        mode: DEFAULT_CAMERA_MODE,
        capture_interval: DEFAULT_CAPTURE_INTERVAL,
        motion_detect: false,
        motion_event_source: DEFAULT_MOTION_EVENT_SOURCE,
        motion_mark_enabled: false,
        motion_mark_event_source: DEFAULT_MOTION_MARK_EVENT_SOURCE,
        motionDetectRatioThreshold: DEFAULT_MOTION_RATIO_THRESHOLD,
        auto_discovered: false,
        ...overrides
    };
}

function addConfigCamera(seed = {}) {
    syncConfigCameraExpandedStateFromDom();
    configFormState = collectConfigForm({allowEmptyID: true});
    const nextCam = normalizeConfigCamera(defaultConfigCameraSeed(seed));
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
    const defaults = readBatchCameraDefaults();

    applyBtn.disabled = validCount === 0 || invalidCount > 0;
    summary.innerHTML = renderBatchCameraSummary(validCount, invalidCount, defaults);

    preview.classList.toggle('hidden', validCount === 0 && invalidCount === 0);
    preview.innerHTML = renderBatchCameraPreviewMarkup(latestBatchCameraPreview);
}

function renderBatchCameraSummary(validCount, invalidCount, defaults = readBatchCameraDefaults()) {
    const defaultText = escapeHtml(formatBatchCameraDefaults(defaults));
    if (validCount === 0 && invalidCount === 0) {
        return `
            <strong>等待输入</strong>
            <span>粘贴多行接入源后会实时解析；${defaultText}</span>
        `;
    }
    return `
        <strong>${validCount} 个可添加</strong>
        <span>${invalidCount ? `${invalidCount} 行需要修正后才能添加` : `检查无误，确认后追加到列表顶部；${defaultText}`}</span>
    `;
}

// 批量添加的默认配置只作用于本次新增的摄像头，不会覆盖已有摄像头。
function readBatchCameraDefaults() {
    const mode = readBatchCameraChoice('batchCameraDefaultMode', ['normal', 'timelapse'], DEFAULT_CAMERA_MODE);
    const format = readBatchCameraChoice('batchCameraDefaultFormat', ['mp4', 'ts'], DEFAULT_RECORD_FORMAT);
    const recordTime = (document.getElementById('batchCameraDefaultRecordTime')?.value || '').trim() || DEFAULT_RECORD_TIME;
    const segmentDuration = readBatchCameraInteger('batchCameraDefaultSegmentDuration', DEFAULT_SEGMENT_DURATION, 1);
    const retentionDays = readBatchCameraInteger('batchCameraDefaultRetentionDays', DEFAULT_RETENTION_DAYS, -1);
    return {
        mode,
        format,
        record_time: recordTime,
        segment_duration: segmentDuration,
        retention_days: retentionDays
    };
}

function readBatchCameraChoice(id, allowed, fallback) {
    const value = String(document.getElementById(id)?.value || '').trim();
    return allowed.includes(value) ? value : fallback;
}

function readBatchCameraInteger(id, fallback, minValue = null) {
    const rawValue = String(document.getElementById(id)?.value || '').trim();
    if (rawValue === '') return fallback;

    const parsed = Number.parseInt(rawValue, 10);
    if (!Number.isFinite(parsed)) return fallback;
    if (minValue !== null && parsed < minValue) return minValue;
    return parsed;
}

function formatBatchCameraDefaults(defaults) {
    const mode = defaults.mode === 'timelapse' ? '延时' : '普通';
    const format = String(defaults.format || DEFAULT_RECORD_FORMAT).toUpperCase();
    const schedule = isAllDayRecordTime(defaults.record_time) ? '全天录像' : defaults.record_time;
    const segmentDuration = Number(defaults.segment_duration) || DEFAULT_SEGMENT_DURATION;
    return `默认 ${mode} / ${format} / ${schedule} / 切片 ${segmentDuration} 秒 / ${formatBatchRetentionDays(defaults.retention_days)}`;
}

function isAllDayRecordTime(recordTime) {
    const text = String(recordTime || '').trim();
    return text === '' || text === DEFAULT_RECORD_TIME || text === '00:00-24:00';
}

function formatBatchRetentionDays(retentionDays) {
    if (retentionDays < 0) return '不自动清理';
    return `保留 ${retentionDays || DEFAULT_RETENTION_DAYS} 天`;
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

    const defaults = readBatchCameraDefaults();
    const parsed = parseBatchCameraInput(input.value);
    latestBatchCameraPreview = parsed;
    renderBatchCameraPreview();
    if (parsed.valid.length === 0) return;

    syncConfigCameraExpandedStateFromDom();
    configFormState = collectConfigForm({allowEmptyID: true});
    const nextCameras = parsed.valid.map(item => normalizeConfigCamera(defaultConfigCameraSeed({
        ...defaults,
        id: item.id,
        stream_url: item.stream_url
    })));

    nextCameras.forEach(cam => ensureConfigCameraUiKey(cam));
    configFormState.cameras = [...nextCameras, ...configFormState.cameras];
    configCameraExpandedKeys = new Set();
    configOnvifDiagnosticsOpenKeys = new Set();
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
    if (removedKey) configOnvifDiagnosticsOpenKeys.delete(removedKey);
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
        `  merge_motion_records: ${cfg.daily_merge.merge_motion_records ? 'true' : 'false'}`,
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
        lines.push(`    motion_event_source: ${yamlScalar(cam.motion_event_source || DEFAULT_MOTION_EVENT_SOURCE)}`);
        lines.push(`    motion_mark_enabled: ${cam.motion_mark_enabled ? 'true' : 'false'}`);
        lines.push(`    motion_mark_event_source: ${yamlScalar(cam.motion_mark_event_source || DEFAULT_MOTION_MARK_EVENT_SOURCE)}`);
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

    const newCamYaml = [`${listIndent}- id: "${streamId}"`, `${propIndent}order: ${nextOrder}`, `${propIndent}stream_url: "managed_by_go2rtc"`, `${propIndent}motion_url: ""`, `${propIndent}auto_discovered: true`, `${propIndent}retention_days: 7`, `${propIndent}segment_duration: 600`, `${propIndent}format: ${DEFAULT_RECORD_FORMAT}`, `${propIndent}min_size_kb: 1024`, `${propIndent}record_time: "00:00-23:59"`, `${propIndent}mode: normal`, `${propIndent}motion_detect: false`, `${propIndent}motion_event_source: ${DEFAULT_MOTION_EVENT_SOURCE}`, `${propIndent}motion_mark_enabled: false`, `${propIndent}motion_mark_event_source: ${DEFAULT_MOTION_MARK_EVENT_SOURCE}`, `${propIndent}motionDetectRatioThreshold: 0.01`].join('\n') + '\n';

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
            <div id="cell-${i}" onclick="setActiveCell(${i})" ondblclick="toggleCellFullscreen(${i})" class="matrix-cell relative w-full h-full bg-gray-900 border-[2px] transition-colors overflow-hidden group cursor-pointer ${activeCellClass} ${cellFocusClass}">
                <iframe id="live-iframe-${i}" class="${liveIframeClass}" allow="autoplay; fullscreen; microphone; camera"></iframe>
                <div id="dplayer-${i}" class="w-full h-full hidden"></div>
                <video id="native-player-${i}" class="w-full h-full object-contain hidden bg-black" playsinline controls></video>
                <div id="empty-state-${i}" class="absolute inset-0 flex flex-col items-center justify-center text-gray-700 pointer-events-none group-hover:text-gray-500 transition-colors">
                    <svg class="w-8 h-8 mb-2 opacity-30" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                    <span class="text-xs font-bold tracking-wider uppercase opacity-50">窗口 ${i + 1}</span>
                </div>
                <div class="absolute top-2 left-1/2 z-10 max-w-[80%] -translate-x-1/2 bg-black/35 text-white/80 px-2.5 py-1 text-[10px] rounded backdrop-blur-sm border border-white/5 hidden pointer-events-none truncate opacity-55 transition-all duration-200 group-hover:bg-black/70 group-hover:text-white group-hover:border-white/10 group-hover:opacity-100" id="label-${i}"></div>
                <div id="onvif-event-overlay-${i}" class="onvif-event-overlay hidden" aria-live="polite"></div>
                <button onclick="event.stopPropagation(); toggleOnvifEventOverlay(${i})" onmouseenter="setOnvifEventOverlayHover(${i}, true)" onmouseleave="setOnvifEventOverlayHover(${i}, false)" onfocus="setOnvifEventOverlayHover(${i}, true)" onblur="setOnvifEventOverlayHover(${i}, false)" class="onvif-event-toggle hidden" id="onvif-event-toggle-${i}" type="button" aria-pressed="false" aria-label="显示 ONVIF 事件叠层" title="显示 ONVIF 事件叠层">
                    <span class="onvif-event-toggle-glyph onvif-event-toggle-glyph-default" aria-hidden="true">
                        <svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M3 12h4l2-5 4 10 2-5h6"></path></svg>
                    </span>
                    <span class="onvif-event-toggle-glyph onvif-event-toggle-glyph-event" aria-hidden="true">
                        <svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.1" d="M10.3 21a2 2 0 0 0 3.4 0M4 2c-1.3 1.8-2 3.8-2 6m20 0c0-2.2-.7-4.2-2-6M4.3 15.3A1 1 0 0 0 5 17h14a1 1 0 0 0 .7-1.7C18.5 14 17.5 12.4 17.5 8.5a5.5 5.5 0 0 0-11 0c0 3.9-1 5.5-2.2 6.8Z"></path></svg>
                    </span>
                </button>
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
    refreshOnvifEventOverlay();
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
    clearOnvifEventOverlay(index);
    if (closeBtn) {
        closeBtn.classList.add('hidden');
        closeBtn.classList.remove('flex');
    }
    updateFocusUI();
    refreshOnvifEventOverlay();
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
    refreshOnvifEventOverlay();
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

function lockRecordListHeightDuringRender(list) {
    if (!list || window.scrollY <= 0 || list.children.length === 0) return () => {};
    const height = list.getBoundingClientRect().height;
    if (!Number.isFinite(height) || height <= 0) return () => {};

    const token = ++recordListHeightLockToken;
    list.style.minHeight = `${Math.ceil(height)}px`;
    return () => {
        requestAnimationFrame(() => {
            requestAnimationFrame(() => {
                if (recordListHeightLockToken === token && list.isConnected) {
                    list.style.minHeight = '';
                }
            });
        });
    };
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
    refreshOnvifEventOverlay();
    refreshPTZPanel(isLive);
}

function getActiveLiveCamId() {
    if (currentLayout !== 1) return '';
    const data = cellData[activeCell];
    if (!data || !data.isLive) return '';
    return String(data.camId || data.source || '').trim();
}

function refreshOnvifEventOverlay(options = {}) {
    const camId = getActiveLiveCamId();
    const eligible = isOnvifEventOverlayEligible(camId);
    const noticeView = buildOnvifEventOverlayNoticeView(camId);
    for (let i = 0; i < dpInstances.length; i++) {
        if (i !== activeCell || (!eligible && !noticeView)) clearOnvifEventOverlay(i);
    }
    refreshOnvifEventOverlayControls();

    const overlay = document.getElementById(`onvif-event-overlay-${activeCell}`);
    if (!eligible) {
        stopOnvifEventSummaryPolling();
        if (noticeView && overlay && isOnvifEventOverlayControlAvailable(camId)) {
            renderOnvifEventOverlayNotice(overlay, noticeView);
        }
        return;
    }

    if (!overlay) return;

    if (!options.skipPolling) ensureOnvifEventSummaryPolling(camId);
    const view = buildOnvifEventOverlayView(camId);
    if (!view) {
        if (noticeView) {
            renderOnvifEventOverlayNotice(overlay, noticeView);
        } else {
            clearOnvifEventOverlay(activeCell);
        }
        return;
    }

    if (overlay.dataset.eventKey !== view.viewKey) {
        overlay.dataset.eventKey = view.viewKey;
        overlay.innerHTML = view.events.map(renderOnvifEventOverlayItem).join('');
    }
    overlay.classList.add('is-event-list');
    overlay.classList.remove('is-notice-list');
    overlay.classList.remove('hidden');
    scheduleOnvifEventOverlayExpiry(view.nextRefreshMs);
    refreshOnvifEventOverlayControls();
}

function clearOnvifEventOverlay(index) {
    const overlay = document.getElementById(`onvif-event-overlay-${index}`);
    clearOnvifEventOverlayTimers();
    if (!overlay) return;
    overlay.classList.add('hidden');
    overlay.classList.remove('is-motion', 'is-generic', 'is-waiting', 'is-fading', 'is-event-list', 'is-notice-list');
    delete overlay.dataset.eventKey;
    overlay.innerHTML = '';
}

function refreshOnvifEventOverlayControls() {
    for (let i = 0; i < dpInstances.length; i++) {
        const button = document.getElementById(`onvif-event-toggle-${i}`);
        const cell = document.getElementById(`cell-${i}`);
        if (!button) continue;

        const data = cellData[i];
        const camId = data && data.isLive ? String(data.camId || data.source || '').trim() : '';
        const available = i === activeCell && isOnvifEventOverlayControlAvailable(camId);
        const enabled = available && isOnvifEventOverlayEnabled(camId);
        const eventState = enabled ? getOnvifEventOverlayVisibleEventState(camId) : {hasAny: false, hasMotion: false};
        const hasMotionEvent = enabled && eventState.hasMotion;
        const hasAnyEvent = enabled && eventState.hasAny;
        if (!available) setOnvifEventOverlayHover(i, false);

        button.classList.toggle('hidden', !available);
        button.classList.toggle('is-active', enabled);
        button.classList.toggle('is-has-event', hasMotionEvent);
        button.classList.toggle('is-has-topic', hasAnyEvent && !hasMotionEvent);
        if (cell) cell.classList.toggle('is-onvif-motion-aura', hasMotionEvent);
        button.setAttribute('aria-pressed', enabled ? 'true' : 'false');
        button.setAttribute('aria-label', hasMotionEvent ? '关闭 ONVIF 事件叠层，当前有最近动检事件' : (hasAnyEvent ? '关闭 ONVIF 事件叠层，当前有最近普通事件' : (enabled ? '关闭 ONVIF 事件叠层' : '开启 ONVIF 事件叠层')));
        button.title = hasAnyEvent ? '关闭 ONVIF 事件叠层，悬停查看事件' : (enabled ? '关闭 ONVIF 事件叠层' : '开启 ONVIF 事件叠层');
    }
}

function setOnvifEventOverlayHover(index, hovering) {
    const cell = document.getElementById(`cell-${index}`);
    if (!cell) return;
    cell.classList.toggle('is-onvif-event-toggle-hover', Boolean(hovering));
}

function toggleOnvifEventOverlay(index) {
    if (currentLayout !== 1 || index !== activeCell) return;
    const data = cellData[index];
    const camId = data && data.isLive ? String(data.camId || data.source || '').trim() : '';
    if (!isOnvifEventOverlayControlAvailable(camId)) return;

    if (isOnvifEventOverlayEnabled(camId)) {
        window.onvifEventOverlayEnabledCameras.delete(camId);
        window.cameraOnvifEventSummaryCache.delete(camId);
        window.cameraOnvifEventHistoryCache.delete(camId);
        window.cameraOnvifEventOverlayNoticeCache.delete(camId);
        setOnvifEventOverlayHover(index, false);
    } else {
        window.cameraOnvifEventOverlayNoticeCache.delete(camId);
        window.onvifEventOverlayEnabledCameras.add(camId);
    }
    refreshOnvifEventOverlay();
}

function isOnvifEventOverlayControlAvailable(camId) {
    if (currentLayout !== 1) return false;
    camId = String(camId || '').trim();
    if (!camId) return false;
    const data = cellData[activeCell];
    if (!data || !data.isLive) return false;
    const currentCamId = String(data.camId || data.source || '').trim();
    if (currentCamId !== camId) return false;
    const capability = window.cameraCapabilityCache?.get?.(camId);
    return capability?.onvif_enabled === true;
}

function isOnvifEventOverlayEnabled(camId) {
    camId = String(camId || '').trim();
    return Boolean(camId && window.onvifEventOverlayEnabledCameras?.has?.(camId));
}

function clearOnvifEventOverlayTimers() {
    if (onvifEventOverlayHideTimer) {
        clearTimeout(onvifEventOverlayHideTimer);
        onvifEventOverlayHideTimer = null;
    }
    if (onvifEventOverlayFadeTimer) {
        clearTimeout(onvifEventOverlayFadeTimer);
        onvifEventOverlayFadeTimer = null;
    }
}

function scheduleOnvifEventOverlayExpiry(remainingMs) {
    clearOnvifEventOverlayTimers();
    remainingMs = Math.max(0, Number(remainingMs) || 0);

    onvifEventOverlayHideTimer = setTimeout(() => {
        refreshOnvifEventOverlay({skipPolling: true});
    }, remainingMs);
}

function isOnvifEventOverlayEligible(camId) {
    return isOnvifEventOverlayControlAvailable(camId) && isOnvifEventOverlayEnabled(camId);
}

function ensureOnvifEventSummaryPolling(camId) {
    camId = String(camId || '').trim();
    if (!camId) return;
    if (onvifEventOverlayPollCamId === camId && onvifEventOverlayPollTimer) return;

    stopOnvifEventSummaryPolling();
    onvifEventOverlayPollCamId = camId;
    void fetchOnvifEventSummary(camId);
    onvifEventOverlayPollTimer = setInterval(() => {
        if (!isOnvifEventOverlayEligible(camId) || getActiveLiveCamId() !== camId) {
            refreshOnvifEventOverlay({skipPolling: true});
            return;
        }
        void fetchOnvifEventSummary(camId);
    }, ONVIF_EVENT_OVERLAY_POLL_INTERVAL_MS);
}

function stopOnvifEventSummaryPolling(options = {}) {
    const camId = onvifEventOverlayPollCamId;
    if (onvifEventOverlayPollTimer) {
        clearInterval(onvifEventOverlayPollTimer);
        onvifEventOverlayPollTimer = null;
    }
    onvifEventOverlayPollCamId = '';
    clearOnvifEventOverlayTimers();
    releaseOnvifEventOverlayListener(camId, options);
}

function releaseOnvifEventOverlayListener(camId, options = {}) {
    camId = String(camId || '').trim();
    if (!camId || !window.onvifEventOverlayClientId) return;

    const url = `/api/camera/${encodeURIComponent(camId)}/onvif/event-summary/release?client_id=${encodeURIComponent(window.onvifEventOverlayClientId)}`;
    if (options.beacon && navigator.sendBeacon) {
        try {
            navigator.sendBeacon(url);
            return;
        } catch (_) {
        }
    }

    fetch(url, {
        method: 'POST',
        keepalive: Boolean(options.beacon)
    }).catch(() => {});
}

function createOnvifEventOverlayClientId() {
    if (window.crypto && typeof window.crypto.randomUUID === 'function') {
        return window.crypto.randomUUID();
    }
    return `overlay-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

async function fetchOnvifEventSummary(camId) {
    camId = String(camId || '').trim();
    if (!isOnvifEventOverlayEligible(camId) || getActiveLiveCamId() !== camId) return;

    try {
        const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/onvif/event-summary?ensure_listener=1&client_id=${encodeURIComponent(window.onvifEventOverlayClientId)}`);
        if (!isOnvifEventOverlayEligible(camId) || getActiveLiveCamId() !== camId) return;

        if (!resp.ok) {
            const errorText = await readOnvifEventSummaryError(resp);
            window.cameraOnvifEventSummaryCache.delete(camId);
            window.cameraOnvifEventHistoryCache.delete(camId);
            window.onvifEventOverlayEnabledCameras.delete(camId);
            setOnvifEventOverlayNotice(camId, errorText || 'ONVIF 事件监听不可用');
            stopOnvifEventSummaryPolling();
            refreshOnvifEventOverlay({skipPolling: true});
            return;
        }

        const summary = await resp.json();
        window.cameraOnvifEventSummaryCache.set(camId, summary);
        rememberOnvifEventSummaryEvents(camId, summary);
        refreshOnvifEventOverlay({skipPolling: true});
    } catch (e) {
        window.cameraOnvifEventSummaryCache.delete(camId);
        window.cameraOnvifEventHistoryCache.delete(camId);
        window.onvifEventOverlayEnabledCameras.delete(camId);
        setOnvifEventOverlayNotice(camId, '事件叠层连接失败，可重新打开重试');
        stopOnvifEventSummaryPolling();
        refreshOnvifEventOverlay({skipPolling: true});
    }
}

async function readOnvifEventSummaryError(resp) {
    try {
        const payload = await resp.json();
        if (payload && payload.error) return String(payload.error);
    } catch (_) {
    }
    switch (resp.status) {
        case 404:
            return '该设备不是可用的 ONVIF 接入';
        case 409:
            return 'ONVIF Event 或 PullPoint 不可用';
        case 403:
            return '无权访问该设备事件';
        default:
            return 'ONVIF 事件监听不可用';
    }
}

function buildOnvifEventOverlayView(camId) {
    const status = window.cameraOnvifEventSummaryCache.get(camId);
    if (!status || status.onvif_enabled !== true) return null;
    if (String(status.event_state || '').toLowerCase() !== 'available') return null;
    if (status.pull_point_support !== true) return null;
    if (String(status.event_listener_state || '').toLowerCase() !== 'listening') return null;

    rememberOnvifEventSummaryEvents(camId, status);
    const history = pruneOnvifEventOverlayHistory(camId);
    const events = history.map(event => {
        const ageMs = Math.max(0, Date.now() - event.eventTimeMs);
        return {
            ...event,
            remainingMs: ONVIF_EVENT_OVERLAY_VISIBLE_MS - ageMs
        };
    }).filter(event => event.remainingMs > 0).slice(0, ONVIF_EVENT_OVERLAY_MAX_ITEMS);
    if (events.length === 0) return null;

    const nextRefreshMs = nextOnvifEventOverlayRefreshMs(events);
    const viewKey = events
        .map(event => `${event.eventKey}:${event.remainingMs <= ONVIF_EVENT_OVERLAY_FADE_MS ? 'fade' : 'show'}`)
        .join('~');
    return {events, nextRefreshMs, viewKey};
}

function setOnvifEventOverlayNotice(camId, message) {
    camId = String(camId || '').trim();
    message = String(message || '').trim();
    if (!camId || !message) return;
    window.cameraOnvifEventOverlayNoticeCache.set(camId, {
        message,
        expiresAt: Date.now() + ONVIF_EVENT_OVERLAY_NOTICE_VISIBLE_MS
    });
}

function buildOnvifEventOverlayNoticeView(camId) {
    camId = String(camId || '').trim();
    if (!camId) return null;
    const notice = window.cameraOnvifEventOverlayNoticeCache.get(camId);
    if (!notice) return null;

    const remainingMs = Number(notice.expiresAt || 0) - Date.now();
    if (!Number.isFinite(remainingMs) || remainingMs <= 0) {
        window.cameraOnvifEventOverlayNoticeCache.delete(camId);
        return null;
    }
    return {
        message: notice.message,
        remainingMs,
        viewKey: `notice:${notice.message}:${remainingMs <= ONVIF_EVENT_OVERLAY_FADE_MS ? 'fade' : 'show'}`
    };
}

function renderOnvifEventOverlayNotice(overlay, notice) {
    if (!overlay || !notice) return;
    if (overlay.dataset.eventKey !== notice.viewKey) {
        overlay.dataset.eventKey = notice.viewKey;
        overlay.innerHTML = `
            <div class="onvif-event-overlay-item is-notice${notice.remainingMs <= ONVIF_EVENT_OVERLAY_FADE_MS ? ' is-fading' : ''}" style="--event-index:0">
                <span class="onvif-event-overlay-status" aria-hidden="true"></span>
                <span class="onvif-event-overlay-notice-icon" aria-hidden="true">
                    <svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.1" d="M12 9v4m0 4h.01M10.3 4.6 2.9 18a1.5 1.5 0 0 0 1.3 2.2h15.6a1.5 1.5 0 0 0 1.3-2.2L13.7 4.6a2 2 0 0 0-3.4 0Z"></path></svg>
                </span>
                <span class="onvif-event-overlay-topic" title="${escapeHtml(notice.message)}">${escapeHtml(notice.message)}</span>
            </div>
        `;
    }
    overlay.classList.remove('hidden');
    overlay.classList.add('is-notice-list');
    overlay.classList.remove('is-event-list');
    scheduleOnvifEventOverlayExpiry(notice.remainingMs);
}

function getOnvifEventOverlayVisibleEventState(camId) {
    const events = pruneOnvifEventOverlayHistory(camId);
    return {
        hasAny: events.length > 0,
        hasMotion: events.some(event => event.kind === 'motion')
    };
}

function rememberOnvifEventSummaryEvents(camId, status) {
    camId = String(camId || '').trim();
    if (!camId || !status) return;

    const incoming = Array.isArray(status.recent_events) && status.recent_events.length > 0
        ? status.recent_events
        : (status.last_event ? [status.last_event] : []);
    if (incoming.length === 0) {
        pruneOnvifEventOverlayHistory(camId);
        return;
    }

    const eventByKey = new Map();
    const existing = window.cameraOnvifEventHistoryCache.get(camId) || [];
    existing.forEach(event => eventByKey.set(event.eventKey, event));
    incoming.forEach(event => {
        const normalized = normalizeOnvifEventOverlayEvent(event);
        if (normalized) eventByKey.set(normalized.eventKey, normalized);
    });

    const next = Array.from(eventByKey.values())
        .filter(event => Date.now() - event.eventTimeMs <= ONVIF_EVENT_OVERLAY_VISIBLE_MS)
        .sort((a, b) => b.eventTimeMs - a.eventTimeMs)
        .slice(0, ONVIF_EVENT_OVERLAY_MAX_ITEMS);
    if (next.length > 0) {
        window.cameraOnvifEventHistoryCache.set(camId, next);
    } else {
        window.cameraOnvifEventHistoryCache.delete(camId);
    }
}

function pruneOnvifEventOverlayHistory(camId) {
    camId = String(camId || '').trim();
    if (!camId) return [];
    const history = (window.cameraOnvifEventHistoryCache.get(camId) || [])
        .filter(event => Date.now() - event.eventTimeMs <= ONVIF_EVENT_OVERLAY_VISIBLE_MS)
        .sort((a, b) => b.eventTimeMs - a.eventTimeMs)
        .slice(0, ONVIF_EVENT_OVERLAY_MAX_ITEMS);
    if (history.length > 0) {
        window.cameraOnvifEventHistoryCache.set(camId, history);
    } else {
        window.cameraOnvifEventHistoryCache.delete(camId);
    }
    return history;
}

function normalizeOnvifEventOverlayEvent(event) {
    event = event || {};
    const topic = String(event.topic || '').trim();
    if (!topic) return null;

    const eventTimeValue = event.received_at || event.at;
    const eventTimeMs = parseOnvifEventOverlayTimeMs(eventTimeValue);
    if (!Number.isFinite(eventTimeMs)) return null;

    const isMotion = event.motion === true || event.motion_topic === true;
    return {
        kind: isMotion ? 'motion' : 'generic',
        kindLabel: isMotion ? 'Motion' : 'Topic',
        topicText: topic,
        topicTitle: topic,
        timeText: formatOnvifEventOverlayTime(eventTimeValue),
        eventKey: `${topic}|${eventTimeValue}|${isMotion ? 'motion' : 'topic'}`,
        eventTimeMs
    };
}

function renderOnvifEventOverlayItem(event, index) {
    const icon = event.kind === 'motion'
        ? '<span class="onvif-event-overlay-motion-icon" title="Motion"><svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M3 12h4l2-5 4 10 2-5h6"></path></svg></span>'
        : `<span class="onvif-event-overlay-topic-icon" title="${escapeHtml(event.kindLabel)}"><svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.1" d="M4 7h16M4 12h16M4 17h10"></path></svg></span>`;
    return `
        <div class="onvif-event-overlay-item is-${event.kind}${event.remainingMs <= ONVIF_EVENT_OVERLAY_FADE_MS ? ' is-fading' : ''}" style="--event-index:${index}">
            <span class="onvif-event-overlay-status" aria-hidden="true"></span>
            ${icon}
            <span class="onvif-event-overlay-topic" title="${escapeHtml(event.topicTitle)}">${escapeHtml(event.topicText)}</span>
            ${event.timeText ? `<span class="onvif-event-overlay-time">${escapeHtml(event.timeText)}</span>` : ''}
        </div>
    `;
}

function nextOnvifEventOverlayRefreshMs(events) {
    const next = events.flatMap(event => [event.remainingMs - ONVIF_EVENT_OVERLAY_FADE_MS, event.remainingMs])
        .filter(value => Number.isFinite(value) && value > 0);
    if (next.length === 0) return ONVIF_EVENT_OVERLAY_POLL_INTERVAL_MS;
    return Math.max(100, Math.min(...next));
}

function parseOnvifEventOverlayTimeMs(value) {
    if (!value || String(value).startsWith('0001-01-01')) return NaN;
    const time = Date.parse(value);
    return Number.isFinite(time) ? time : NaN;
}

function formatOnvifEventOverlayTime(value) {
    const time = parseOnvifEventOverlayTimeMs(value);
    if (!Number.isFinite(time)) return '';
    const date = new Date(time);
    return date.toLocaleTimeString('zh-CN', {hour12: false});
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
    clearOnvifEventOverlay(targetCell);
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
    refreshOnvifEventOverlay();
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
    clearOnvifEventOverlay(index);
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
    refreshOnvifEventOverlay();
}

function showCodecNoticeInCell(index, data) {
    stopCellPlayback(index);
    clearOnvifEventOverlay(index);

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
    refreshOnvifEventOverlay();
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
    clearOnvifEventOverlay(index);

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
    refreshOnvifEventOverlay();
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
    updateSelectedRecordCameraBadge('');
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

function updateSelectedRecordCameraBadge(camId = currentSelectedCam) {
    const badge = document.getElementById('recordSelectedCam');
    if (!badge) return;

    const normalizedCamId = String(camId || '').trim();
    badge.classList.toggle('hidden', normalizedCamId === '');
    badge.textContent = normalizedCamId ? `当前: ${normalizedCamId}` : '';
    badge.title = normalizedCamId ? `当前摄像头: ${normalizedCamId}` : '';
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

function buildRecordMarkersUrl(camId, date) {
    const params = new URLSearchParams();
    params.set('start', date);
    params.set('end', date);
    return `/api/camera/${encodeURIComponent(camId)}/record-markers?${params.toString()}`;
}

async function loadRecordMarkersForDate(camId, date) {
    date = String(date || '').trim();
    if (!isRecordDateKey(date)) return [];

    const cacheKey = getRecordArchiveGroupKey(camId, date);
    const cacheable = !isTodayDateKey(date);
    if (cacheable && recordMarkersByDateCache.has(cacheKey)) {
        return recordMarkersByDateCache.get(cacheKey);
    }
    if (cacheable && recordMarkersRequestCache.has(cacheKey)) {
        return recordMarkersRequestCache.get(cacheKey);
    }

    const request = fetch(buildRecordMarkersUrl(camId, date))
        .then(resp => resp.ok ? resp.json() : [])
        .then(markers => Array.isArray(markers) ? markers : [])
        .catch(e => {
            console.warn('加载动检时间轴标记失败:', e);
            return [];
        })
        .then(markers => {
            if (cacheable) {
                recordMarkersByDateCache.set(cacheKey, markers);
            }
            recordMarkersRequestCache.delete(cacheKey);
            return markers;
        });
    if (cacheable) {
        recordMarkersRequestCache.set(cacheKey, request);
    }
    return request;
}

function isRecordDateKey(dateKey) {
    return /^\d{4}-\d{2}-\d{2}$/.test(String(dateKey || ''));
}

function isTodayDateKey(dateKey) {
    return dateKey === formatDateKey(new Date());
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
    btn.onclick = async (event) => {
        event.stopPropagation();
        if (!window.RecordTimeline24h) {
            alert('24H 时间轴组件加载失败');
            return;
        }
        if (activeRecordTimeline24hDockKey === getRecordArchiveGroupKey(camId, date)) {
            snapRecordTimeline24hDockBelowPlayer();
            return;
        }
        await renderRecordTimeline24hDock(camId, date, entries, selectedRecordPath);
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

    recordTimeline24hRenderSeq += 1;
    dock.innerHTML = '';
    dock.classList.add('hidden');
    activeRecordTimeline24hDockKey = '';
    refreshRecordTimeline24hActionStates();
}

async function renderRecordTimeline24hDock(camId, date, entries, selectedRecordPath) {
    const dock = getRecordTimeline24hDock();
    if (!dock || !window.RecordTimeline24h) return false;

    const groupKey = getRecordArchiveGroupKey(camId, date);
    const renderSeq = ++recordTimeline24hRenderSeq;
    dock.innerHTML = '';
    dock.classList.remove('hidden');
    activeRecordTimeline24hDockKey = groupKey;
    refreshRecordTimeline24hActionStates();

    let markers;
    if (!isTodayDateKey(date) && recordMarkersByDateCache.has(groupKey)) {
        markers = recordMarkersByDateCache.get(groupKey);
    } else {
        dock.appendChild(createRecordTimeline24hLoading(date));
        requestAnimationFrame(snapRecordTimeline24hDockBelowPlayer);
        markers = await loadRecordMarkersForDate(camId, date);
        if (renderSeq !== recordTimeline24hRenderSeq || activeRecordTimeline24hDockKey !== groupKey) {
            return false;
        }
    }

    dock.innerHTML = '';
    dock.appendChild(window.RecordTimeline24h.create({
        camId,
        date,
        entries,
        markers,
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
    refreshRecordTimeline24hActionStates();
    requestAnimationFrame(snapRecordTimeline24hDockBelowPlayer);
    return true;
}

function createRecordTimeline24hLoading(date) {
    const loading = document.createElement('section');
    loading.className = 'record24h';
    loading.innerHTML = `
        <div class="rounded-lg border border-slate-100 bg-slate-50 px-4 py-8 text-center text-sm font-bold text-slate-400">
            正在加载 ${escapeHtml(date || '该日')} 24H 时间轴...
        </div>
    `;
    return loading;
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
        content.className = 'record-watermark-window record-archive-content-shell record-archive-timeline-well bg-slate-50/70 p-2 custom-scrollbar';
        if (!window.RecordTimeline) {
            const error = document.createElement('div');
            error.className = 'rounded-lg border border-red-100 bg-red-50 px-4 py-8 text-center text-sm font-bold text-red-400';
            error.textContent = '时间轴组件加载失败';
            content.appendChild(error);
            return;
        }
        if (activeRecordTimeline24hDockKey === getRecordArchiveGroupKey(camId, date)) {
            void renderRecordTimeline24hDock(camId, date, entries, selectedRecordPath);
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
        void renderRecordTimeline24hDock(camId, date, entries, selectedRecordPath);
    }
    content.className = 'record-watermark-window record-archive-content-shell record-archive-file-well max-h-[360px] overflow-y-auto bg-slate-50/60 p-2 custom-scrollbar sm:max-h-[460px]';
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
    const releaseRecordListHeight = lockRecordListHeightDuringRender(list);
    updateSelectedRecordCameraBadge(camId);
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
            groupDiv.className = `record-archive-group ${isOpen ? 'is-open' : 'is-collapsed'} overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm transition-shadow hover:shadow-md`;

            const dateBytes = entries.reduce((sum, entry) => sum + parseRecordSizeBytes(entry.file.size), 0);
            const header = document.createElement('div');
            header.className = 'record-archive-group-header flex w-full items-stretch justify-between gap-2 border-b border-slate-100 transition-colors hover:bg-slate-50';

            const summaryBtn = document.createElement('button');
            summaryBtn.type = 'button';
            summaryBtn.setAttribute('aria-expanded', isOpen ? 'true' : 'false');
            summaryBtn.className = 'record-archive-group-summary flex min-w-0 flex-1 items-center gap-3 px-3 py-2 text-left';
            summaryBtn.innerHTML = `
                <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                        <span class="record-archive-group-title text-[13px] font-extrabold tracking-tight text-slate-800">${archiveDateTitle(date)}</span>
                        <span class="record-archive-group-count rounded-full bg-blue-50 px-2 py-0.5 text-[10px] font-bold text-blue-600 ring-1 ring-blue-100">${entries.length} 段</span>
                    </div>
                    <div class="record-archive-group-meta mt-0.5 text-[11px] font-medium text-slate-400">${archiveDateSubTitle(date)} · ${formatRecordSize(dateBytes)}</div>
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
            collapseBtn.setAttribute('aria-expanded', isOpen ? 'true' : 'false');
            collapseBtn.className = 'record-archive-group-collapse flex h-full items-center px-2 text-slate-400 transition-colors hover:text-slate-700';
            collapseBtn.title = isOpen ? '收起该日录像' : '展开该日录像';
            collapseBtn.innerHTML = `
                <svg class="h-4 w-4 shrink-0 transition-transform ${isOpen ? 'rotate-90' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path>
                </svg>
            `;

            const rightTools = document.createElement('div');
            rightTools.className = 'record-archive-group-tools flex shrink-0 items-center gap-1 pr-2';
            rightTools.appendChild(viewSwitch);
            rightTools.appendChild(timeline24hAction);
            rightTools.appendChild(collapseBtn);

            const setOpen = (nextOpen) => {
                groupDiv.classList.toggle('is-open', nextOpen);
                groupDiv.classList.toggle('is-collapsed', !nextOpen);
                summaryBtn.setAttribute('aria-expanded', nextOpen ? 'true' : 'false');
                collapseBtn.setAttribute('aria-expanded', nextOpen ? 'true' : 'false');
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
    } finally {
        releaseRecordListHeight();
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
    const kindInfo = classifyRecordKindByName(name);
    const timeDisplay = timeParts && timeParts.start ? `${timeParts.start.hourText}:${timeParts.start.minuteText}:${timeParts.start.secondText}` : (kindInfo.merged ? name : '整段录像');
    const sortKey = timeParts && timeParts.start ? `${timeParts.start.hourText}${timeParts.start.minuteText}${timeParts.start.secondText}_${name}` : name;

    return {
        date,
        timeDisplay,
        sortKey,
        ext,
        kind: kindInfo.label,
        kindKey: kindInfo.key,
        kindClass: kindInfo.kindClass,
        iconClass: kindInfo.iconClass,
        hasStartTime: Boolean(timeParts && timeParts.start),
        startSeconds: timeParts && timeParts.start ? timeParts.start.startSeconds : null,
        hasEndTime: Boolean(timeParts && timeParts.end),
        endSeconds: timeParts && timeParts.end ? timeParts.end.startSeconds : null,
        timelineEndSeconds: timeParts ? timeParts.timelineEndSeconds : null,
        estimatedEndSeconds: timeParts ? timeParts.estimatedEndSeconds : null
    };
}

function classifyRecordKindByName(name) {
    const lower = String(name || '').toLowerCase();
    if (/_timelapse\./.test(lower)) return recordKindPresentation('timelapse');
    if (/_motion_merged\./.test(lower)) return recordKindPresentation('motion_merged');
    if (/_motion\./.test(lower)) return recordKindPresentation('motion_fragment');
    if (/_merged\./.test(lower)) return recordKindPresentation('normal_merged');
    return recordKindPresentation('normal_fragment');
}

function recordKindPresentation(key) {
    switch (key) {
        case 'motion_merged':
            return {
                key,
                label: '动检合并',
                merged: true,
                kindClass: 'bg-emerald-50 text-emerald-700 ring-emerald-100',
                iconClass: 'bg-emerald-50 text-emerald-600 ring-emerald-100'
            };
        case 'motion_fragment':
            return {
                key,
                label: '动检碎片',
                merged: false,
                kindClass: 'bg-amber-50 text-amber-700 ring-amber-100',
                iconClass: 'bg-amber-50 text-amber-600 ring-amber-100'
            };
        case 'normal_merged':
            return {
                key,
                label: '普通合并',
                merged: true,
                kindClass: 'bg-blue-50 text-blue-700 ring-blue-100',
                iconClass: 'bg-blue-50 text-blue-600 ring-blue-100'
            };
        case 'timelapse':
            return {
                key,
                label: '延时录像',
                merged: false,
                kindClass: 'bg-purple-50 text-purple-700 ring-purple-100',
                iconClass: 'bg-purple-50 text-purple-600 ring-purple-100'
            };
        case 'normal_fragment':
        default:
            return {
                key: 'normal_fragment',
                label: '普通碎片',
                merged: false,
                kindClass: 'bg-slate-100 text-slate-500 ring-slate-200',
                iconClass: 'bg-slate-50 text-slate-500 ring-slate-200'
            };
    }
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
    item.dataset.recordKind = meta.kindKey || '';
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
