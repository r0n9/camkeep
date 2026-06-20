// === 实时节点状态、统计筛选与卡片渲染 ===
const cameraCoverObjectURLs = new Map();
const cameraCoverRequested = new Set();
const cameraCoverFailed = new Set();
const cameraCardRenderKeys = new Map();
let latestCameraStatusEntries = [];
let activeCameraStatusFilter = 'all';
let activeCameraSearchQuery = '';

// 窄屏/触屏折叠态下，录制控制收起为单个「当前状态」按钮，点击后展开三个控制按钮。
// 这里按当前覆盖状态映射折叠按钮要显示的图标与文案（与三个控制按钮的图标保持一致）。
const cameraOverrideToggleMeta = {
    start: {
        stateClass: 'camera-node-action-toggle--start',
        label: '强录',
        icon: '<svg class="h-3 w-3" fill="currentColor" viewBox="0 0 24 24"><circle cx="12" cy="12" r="5"></circle></svg>'
    },
    stop: {
        stateClass: 'camera-node-action-toggle--stop',
        label: '停录',
        icon: '<svg class="h-3 w-3" fill="currentColor" viewBox="0 0 24 24"><rect x="7" y="7" width="10" height="10" rx="1.5"></rect></svg>'
    },
    auto: {
        stateClass: 'camera-node-action-toggle--auto',
        label: '计划',
        icon: '<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.3"><path stroke-linecap="round" stroke-linejoin="round" d="M7 7h10v10H7z"></path><path stroke-linecap="round" stroke-linejoin="round" d="M9 3v4M15 3v4M7 11h10"></path></svg>'
    }
};

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
                data-preview-cam-id="${escapeHtml(camId)}"
                onclick="event.stopPropagation(); openCameraLiveFromNode(this.dataset.previewCamId)"
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

function collapseCameraNodeActions(exceptGroup = null) {
    document.querySelectorAll('.camera-node-card-actions.is-expanded').forEach(group => {
        if (group === exceptGroup) return;
        group.classList.remove('is-expanded');
        group.querySelector('.camera-node-action-toggle')?.setAttribute('aria-expanded', 'false');
    });
}

function toggleCameraNodeActions(toggle) {
    const group = toggle.closest('.camera-node-card-actions');
    if (!group) return;

    const nextExpanded = !group.classList.contains('is-expanded');
    collapseCameraNodeActions(group);
    group.classList.toggle('is-expanded', nextExpanded);
    toggle.setAttribute('aria-expanded', nextExpanded ? 'true' : 'false');
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
    const startDisabledAttr = overrideState === 'start' ? 'disabled' : '';
    const stopDisabledAttr = overrideState === 'stop' ? 'disabled' : '';
    const autoDisabledAttr = overrideState === 'auto' ? 'disabled' : '';
    const startActionTitle = overrideState === 'start' ? '当前已是强制录制' : '强制录制';
    const stopActionTitle = overrideState === 'stop' ? '当前已是强制停止' : '强制停止';
    const autoActionTitle = overrideState === 'auto' ? '当前已是按计划录制' : '恢复计划';
    const toggleMeta = cameraOverrideToggleMeta[overrideState] || cameraOverrideToggleMeta.auto;
    const adminActions = canAdmin() ? `
            <div class="camera-node-card-actions flex shrink-0 items-center" aria-label="录制控制">
                <div class="camera-node-action-list flex items-center">
                    <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'start')"
                            class="camera-node-action-btn camera-node-action-btn--start ${startActionClass} flex items-center justify-center transition-all active:scale-95"
                            title="${startActionTitle}"
                            aria-label="${startActionTitle}"
                            aria-pressed="${overrideState === 'start'}"
                            ${startDisabledAttr}>
                        <svg class="h-3 w-3" fill="currentColor" viewBox="0 0 24 24">
                            <circle cx="12" cy="12" r="5"></circle>
                        </svg>
                        <span class="camera-node-action-label">强录</span>
                    </button>
                    <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'stop')"
                            class="camera-node-action-btn camera-node-action-btn--stop ${stopActionClass} flex items-center justify-center transition-all active:scale-95"
                            title="${stopActionTitle}"
                            aria-label="${stopActionTitle}"
                            aria-pressed="${overrideState === 'stop'}"
                            ${stopDisabledAttr}>
                        <svg class="h-3 w-3" fill="currentColor" viewBox="0 0 24 24">
                            <rect x="7" y="7" width="10" height="10" rx="1.5"></rect>
                        </svg>
                        <span class="camera-node-action-label">停录</span>
                    </button>
                    <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'auto')"
                            class="camera-node-action-btn camera-node-action-btn--auto ${autoActionClass} flex items-center justify-center transition-all active:scale-95"
                            title="${autoActionTitle}"
                            aria-label="${autoActionTitle}"
                            aria-pressed="${overrideState === 'auto'}"
                            ${autoDisabledAttr}>
                        <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.3">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M7 7h10v10H7z"></path>
                            <path stroke-linecap="round" stroke-linejoin="round" d="M9 3v4M15 3v4M7 11h10"></path>
                        </svg>
                        <span class="camera-node-action-label">计划</span>
                    </button>
                </div>
                <button type="button"
                        onclick="event.stopPropagation(); toggleCameraNodeActions(this)"
                        class="camera-node-action-toggle ${toggleMeta.stateClass} hidden items-center justify-center transition-all active:scale-95"
                        title="当前录制控制：${toggleMeta.label}，点击展开"
                        aria-label="当前录制控制：${toggleMeta.label}，点击展开录制控制"
                        aria-expanded="false">
                    ${toggleMeta.icon}
                    <span class="camera-node-action-label">${toggleMeta.label}</span>
                </button>
            </div>
    ` : '';
    const mobileIntents = `
        <div class="mobile-camera-intents" aria-label="${escapeHtml(id)} 快捷操作">
            <button type="button" class="mobile-camera-intent mobile-camera-intent--live" data-mobile-camera-live="${escapeHtml(id)}" aria-label="播放 ${escapeHtml(id)} 直播">
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.1" d="M15 10l4.5-2.25A1 1 0 0121 8.64v6.72a1 1 0 01-1.5.87L15 14M5 6h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2z"></path>
                </svg>
                <span>直播</span>
            </button>
            <button type="button" class="mobile-camera-intent mobile-camera-intent--records" data-mobile-camera-records="${escapeHtml(id)}" aria-label="查看 ${escapeHtml(id)} 录像">
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.1" d="M7 4h10a2 2 0 012 2v14l-7-3-7 3V6a2 2 0 012-2z"></path>
                </svg>
                <span>录像</span>
            </button>
        </div>
    `;
    let streamLight, streamText, streamTitle;

    if (streamState === 'online') {
        streamLight = 'bg-green-500 shadow-[0_0_5px_#22c55e]';
        streamText = '<span class="text-[8px] leading-none text-green-600 font-bold">在线</span>';
        streamTitle = '摄像机实时流状态: 在线，已有真实媒体数据';
    } else if (streamState === 'idle') {
        streamLight = 'bg-blue-400 shadow-[0_0_5px_#60a5fa]';
        streamText = '<span class="text-[8px] leading-none text-blue-500 font-bold">待连接</span>';
        streamTitle = '摄像机实时流状态: 待连接，设备可访问，视频流尚未连接';
    } else {
        streamLight = 'bg-red-500 shadow-[0_0_5px_#ef4444]';
        streamText = '<span class="text-[8px] leading-none text-red-500 font-bold">断线</span>';
        streamTitle = '摄像机实时流状态: 断线';
    }
    const streamStateClass = streamState === 'online'
        ? 'camera-node-stream-pill--online'
        : streamState === 'idle'
            ? 'camera-node-stream-pill--idle'
            : 'camera-node-stream-pill--offline';

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
        <div class="camera-node-card-body flex items-center gap-1 p-1">
            ${buildCameraCoverMarkup(id, cam, streamState)}
            <div class="camera-node-card-content flex min-w-0 flex-1 flex-col justify-center gap-0.5">
                <div class="camera-node-card-title-row flex min-w-0 items-center gap-1">
                        <span class="camera-node-id truncate text-[11px] font-extrabold leading-3 text-gray-800">${escapeHtml(id)}</span>
                        <span class="camera-node-mode-badge shrink-0 rounded ${modeBadgeClass} px-1 py-0.5 text-[7px] font-bold leading-none">${modeDisplay}</span>
                        <span class="camera-node-record-chip ${recordStateView.chipClass}" title="本地录制状态: ${escapeHtml(recordStateView.title)}">${recordStateView.label}</span>
                </div>
                <div class="camera-node-card-control-row flex min-w-0 items-center justify-between gap-1">
                        <span class="camera-node-stream-pill ${streamStateClass} inline-flex h-3.5 min-w-0 items-center rounded bg-slate-50 px-1 ring-1 ring-slate-100" title="${escapeHtml(streamTitle)}">
                            <span class="mr-0.5 h-1.5 w-1.5 shrink-0 rounded-full ${streamLight}"></span>
                            ${streamText}
                        </span>
                        ${adminActions}
                </div>
            </div>
        </div>

        <div class="camera-node-card-footer camera-node-schedule-pill ${recordSchedule.pillClass} flex min-w-0 items-center gap-1 border-t px-2 py-0.5"
             title="${escapeHtml(recordSchedule.title)}">
                <svg class="camera-node-schedule-icon h-2.5 w-2.5 shrink-0 ${recordSchedule.iconClass}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6l4 2"></path>
                    <circle cx="12" cy="12" r="9"></circle>
                </svg>
                <span class="camera-node-schedule-badge shrink-0 text-[7px] font-bold leading-none ${recordSchedule.badgeClass}">${recordSchedule.badge}</span>
                <span class="camera-node-schedule-text min-w-0 flex-1 truncate font-mono text-[8px] font-semibold leading-none ${recordSchedule.textClass}">${escapeHtml(recordSchedule.text)}</span>
        </div>
        ${isMobileCameraInteraction() ? mobileIntents : ''}
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

    if (recordState === 'record_error') {
        return {
            cardClass: `is-record-error ${overrideClass}`,
            chipClass: 'camera-node-record-chip--record-error',
            label: '录制异常',
            title: 'FFmpeg 拉流或录制失败，等待重试'
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

const cameraStatusFilters = new Set(['all', 'online', 'idle', 'recording', 'offline']);

function setCameraStatusFilter(filter) {
    const nextFilter = cameraStatusFilters.has(filter) ? filter : 'all';
    activeCameraStatusFilter = activeCameraStatusFilter === nextFilter && nextFilter !== 'all' ? 'all' : nextFilter;
    updateCameraStats(latestCameraStatusEntries.map(entry => [entry.id, entry.cam]));
    renderCameraListFromState();
}

function normalizeCameraStreamState(cam) {
    const state = String(cam?.stream_state || 'offline').toLowerCase();
    return state === 'online' || state === 'idle' || state === 'offline' ? state : 'offline';
}

function normalizeCameraRecordState(cam) {
    return String(cam?.record_state || (cam?.is_running ? 'recording' : 'idle')).toLowerCase();
}

function cameraRecordStateIsActive(recordState) {
    return recordState === 'recording' || recordState === 'motion_detecting' || recordState === 'motion_recording';
}

function cameraMatchesStatusFilter(cam, filter) {
    if (filter === 'all') return true;

    const streamState = normalizeCameraStreamState(cam);
    if (filter === 'recording') return cameraRecordStateIsActive(normalizeCameraRecordState(cam));
    return streamState === filter;
}

function filteredCameraStatusEntries() {
    const query = activeCameraSearchQuery.trim().toLowerCase();
    return latestCameraStatusEntries.filter(entry => {
        if (!cameraMatchesStatusFilter(entry.cam, activeCameraStatusFilter)) return false;
        if (!query) return true;
        return cameraMatchesSearchQuery(entry, query);
    });
}

function cameraMatchesSearchQuery(entry, query) {
    if (!entry) return false;
    const {id, cam} = entry;
    return [
        id,
        cam?.name,
        cam?.mode,
        cam?.record_state,
        cam?.stream_state,
        cam?.state,
        cam?.record_time,
        buildRecordStateView(cam?.record_state || (cam?.is_running ? 'recording' : 'idle'), normalizeRecordOverride(cam?.record_override)).label,
        cameraStatusFilterLabel(normalizeCameraStreamState(cam)),
        cameraRecordStateIsActive(normalizeCameraRecordState(cam)) ? '录制' : ''
    ].some(value => String(value || '').toLowerCase().includes(query));
}

function syncCameraStatusFilterControls(visibleCount = 0, totalCount = 0) {
    document.querySelectorAll('[data-camera-status-filter]').forEach(control => {
        const active = control.dataset.cameraStatusFilter === activeCameraStatusFilter;
        control.classList.toggle('is-active', active);
        control.setAttribute('aria-pressed', active ? 'true' : 'false');
    });

    const summary = document.getElementById('camStatsSummary');
    if (!summary || activeCameraStatusFilter === 'all' || totalCount === 0) return;
    summary.innerText = `${cameraStatusFilterLabel(activeCameraStatusFilter)} ${visibleCount}/${totalCount}`;
    summary.dataset.state = visibleCount > 0 ? 'active' : 'empty';
}

function cameraStatusFilterLabel(filter) {
    switch (filter) {
    case 'online':
        return '在线';
    case 'idle':
        return '待连接';
    case 'recording':
        return '录制';
    case 'offline':
        return '离线';
    default:
        return '全部';
    }
}

function renderCameraListMessage(list, key, message, detail = '') {
    if (list.dataset.messageKey === key) return;
    list.dataset.messageKey = key;
    list.innerHTML = `
        <div class="camera-node-empty">
            <strong>${escapeHtml(message)}</strong>
            ${detail ? `<span>${escapeHtml(detail)}</span>` : ''}
        </div>
    `;
}

function renderCameraListFromState() {
    const list = document.getElementById('camList');
    if (!list) return;

    const totalCount = latestCameraStatusEntries.length;
    const entries = filteredCameraStatusEntries();
    syncCameraStatusFilterControls(entries.length, totalCount);

    if (totalCount === 0) {
        renderCameraListMessage(list, 'empty-all', '暂无可访问的实时节点');
        return;
    }
    if (entries.length === 0) {
        const statusLabel = cameraStatusFilterLabel(activeCameraStatusFilter);
        const title = activeCameraSearchQuery.trim()
            ? '没有匹配的设备'
            : `暂无${statusLabel}节点`;
        const detail = activeCameraSearchQuery.trim()
            ? '可尝试更换关键词或切换状态筛选'
            : '点击 TOTAL 可恢复全部节点';
        renderCameraListMessage(list, `empty-filter-${activeCameraStatusFilter}-${activeCameraSearchQuery.trim()}`, title, detail);
        return;
    }

    delete list.dataset.messageKey;
    Array.from(list.children).forEach(child => {
        if (!child.dataset.camId) child.remove();
    });

    const visibleCamIds = new Set();
    const existingItems = new Map();
    Array.from(list.children).forEach(item => {
        if (!item.dataset.camId) return;
        existingItems.set(item.dataset.camId, item);
    });

    entries.forEach((entry, index) => {
        const {id, cam} = entry;
        visibleCamIds.add(id);

        const view = buildCameraCardView(id, cam);
        let item = existingItems.get(id);
        const created = !item;
        if (!item) {
            item = document.createElement('div');
            item.dataset.camId = id;
            item.onclick = (event) => handleCameraCardClick(event, id);
        }
        item.className = view.className;
        if (view.style) {
            item.setAttribute('style', view.style);
        } else {
            item.removeAttribute('style');
        }
        if (created || cameraCardRenderKeys.get(id) !== view.html) {
            item.innerHTML = view.html;
            bindCameraCardIntentHandlers(item, id);
            cameraCardRenderKeys.set(id, view.html);
        }
        if (list.children[index] !== item) {
            list.insertBefore(item, list.children[index] || null);
        }
    });

    Array.from(list.children).forEach(item => {
        if (!item.dataset.camId) return;
        if (!visibleCamIds.has(item.dataset.camId)) item.remove();
    });
}

function isMobileCameraInteraction() {
    return window.CamKeepMobile?.isMobileMode?.() === true;
}

function handleCameraCardClick(event, camId) {
    if (event.target?.closest?.('button, a, input, select, textarea, [data-mobile-camera-live], [data-mobile-camera-records]')) {
        return;
    }
    if (isMobileCameraInteraction()) {
        if (currentSelectedCam !== camId) {
            selectCamera(camId);
        }
        return;
    }
    selectCamera(camId);
}

function bindCameraCardIntentHandlers(item, camId) {
    item.querySelector('[data-mobile-camera-live]')?.addEventListener('click', event => {
        event.stopPropagation();
        openCameraLiveFromNode(camId);
    });
    item.querySelector('[data-mobile-camera-records]')?.addEventListener('click', event => {
        event.stopPropagation();
        openCameraRecordsFromNode(camId);
    });
}

function initCameraNodeSearch() {
    const input = document.getElementById('cameraNodeSearchInput');
    if (!input) return;
    input.addEventListener('input', () => {
        activeCameraSearchQuery = input.value || '';
        renderCameraListFromState();
    });
}

async function readStatusApiError(resp, fallback = '状态同步失败') {
    if (typeof readApiErrorMessage === 'function') {
        return readApiErrorMessage(resp, fallback);
    }
    const payload = await resp.clone().json().catch(() => ({}));
    return payload.error || payload.message || fallback;
}

function isStatusAuthFailure(resp) {
    if (typeof isAuthFailureResponse === 'function') return isAuthFailureResponse(resp);
    return resp && (resp.status === 401 || resp.status === 403);
}

function statusAuthFailureMessage(resp) {
    if (typeof authFailureMessage === 'function') return authFailureMessage(resp);
    if (resp?.status === 401) return '登录已过期，请重新登录。';
    if (resp?.status === 403) return '当前账号无权访问实时状态。';
    return '状态接口未通过权限检查。';
}

function renderStatusLoadFailure(key, message, detail = '') {
    latestCameraStatusEntries = [];
    currentSelectedCam = null;
    cameraCardRenderKeys.clear();
    window.cameraCapabilityCache?.clear?.();
    window.cameraOnvifEventSummaryCache?.clear?.();
    window.cameraOnvifEventHistoryCache?.clear?.();
    window.cameraOnvifEventOverlayNoticeCache?.clear?.();
    setSelectedRecordPath('');
    updateSelectedRecordCameraBadge('');
    window.syncMobileStageDevicePicker?.();
    renderRecordSelectionPrompt(message);
    updateCameraStats([], true);
    const list = document.getElementById('camList');
    if (list) renderCameraListMessage(list, key, message, detail);
    window.PTZ?.hidePanel?.();
}

// --- 状态加载 ---
async function loadStatus() {
    try {
        const resp = await fetch('/api/status');
        if (!resp.ok) {
            if (isStatusAuthFailure(resp)) {
                renderStatusLoadFailure(
                    `auth-${resp.status}`,
                    statusAuthFailureMessage(resp),
                    resp.status === 401 ? '请重新登录后继续使用实时预览和录像控制。' : '请切换到有权限的账号。'
                );
                return;
            }
            throw new Error(await readStatusApiError(resp, '状态同步失败'));
        }
        const data = await resp.json();
        if (!data || typeof data !== 'object' || Array.isArray(data)) {
            throw new Error('状态响应格式异常');
        }
        const cameras = Object.entries(data || {});
        latestCameraStatusEntries = cameras.map(([id, cam], index) => ({id, cam, index}));
        updateCameraStats(cameras);
        const visibleCamIds = new Set(cameras.map(([id]) => id));
        if (currentSelectedCam && !visibleCamIds.has(currentSelectedCam)) {
            currentSelectedCam = null;
            setSelectedRecordPath('');
            updateSelectedRecordCameraBadge('');
            renderRecordSelectionPrompt('当前账号不可访问原选中的摄像头');
        }
        window.syncMobileStageDevicePicker?.();

        Array.from(cameraCardRenderKeys.keys()).forEach(id => {
            if (!visibleCamIds.has(id)) cameraCardRenderKeys.delete(id);
        });
        Array.from(window.cameraCapabilityCache.keys()).forEach(id => {
            if (!visibleCamIds.has(id)) {
                window.cameraCapabilityCache.delete(id);
                window.cameraOnvifEventSummaryCache?.delete?.(id);
                window.cameraOnvifEventHistoryCache?.delete?.(id);
                window.cameraOnvifEventOverlayNoticeCache?.delete?.(id);
            }
        });

        latestCameraStatusEntries.forEach(({id, cam}) => {
            window.cameraCapabilityCache.set(id, {
                onvif_enabled: cam.onvif_enabled === true,
                capability_state: cam.onvif_capability_state || cam.capability_state || '',
                ptz_state: cam.ptz_state || '',
                imaging_state: cam.imaging_state || ''
            });
        });

        renderCameraListFromState();
        if (cameras.length === 0 && !currentSelectedCam) renderRecordSelectionPrompt('当前账号暂无可访问摄像头');

        if (typeof refreshOnvifEventOverlay === 'function') refreshOnvifEventOverlay();
        window.syncMobileStageDevicePicker?.();
        refreshPTZPanel();
    } catch (e) {
        if (typeof refreshOnvifEventOverlay === 'function') refreshOnvifEventOverlay();
        renderStatusLoadFailure('status-error', '状态同步失败', e.message || '请稍后重试。');
        console.error("同步状态失败:", e);
    }
}

function updateCameraStats(cameras, failed = false) {
    const summary = document.getElementById('camStatsSummary');
    const panel = document.getElementById('camStatsPanel');
    const totalEl = document.getElementById('camStatsTotal');
    const onlineEl = document.getElementById('camStatsOnline');
    const idleEl = document.getElementById('camStatsIdle');
    const recordingEl = document.getElementById('camStatsRecording');
    const offlineEl = document.getElementById('camStatsOffline');

    if (failed) {
        if (summary) {
            summary.innerText = '同步失败';
            summary.dataset.state = 'failed';
        }
        updateCameraStatsDistribution(panel, {total: 0, online: 0, idle: 0, offline: 0});
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
        if (recordState === 'record_error') result.recordError += 1;
        return result;
    }, {total: 0, online: 0, idle: 0, recording: 0, recordError: 0, offline: 0});

    updateCameraStatsDistribution(panel, stats);

    if (summary) {
        if (stats.total === 0) {
            summary.innerText = '0 节点';
            summary.dataset.state = 'empty';
        } else if (stats.offline > 0) {
            summary.innerText = `${stats.offline} 离线`;
            summary.dataset.state = 'warning';
        } else if (stats.recordError > 0) {
            summary.innerText = `${stats.recordError} 录制异常`;
            summary.dataset.state = 'warning';
        } else if (stats.recording > 0) {
            summary.innerText = `${stats.recording} 录制中`;
            summary.dataset.state = 'active';
        } else {
            summary.innerText = `${stats.total} 节点`;
            summary.dataset.state = 'ok';
        }
    }
    if (totalEl) totalEl.innerText = stats.total;
    if (onlineEl) onlineEl.innerText = stats.online;
    if (idleEl) idleEl.innerText = stats.idle;
    if (recordingEl) recordingEl.innerText = stats.recording;
    if (offlineEl) offlineEl.innerText = stats.offline;
}

function updateCameraStatsDistribution(panel, stats) {
    if (!panel) return;
    const total = Math.max(0, Number(stats.total) || 0);
    const share = value => {
        if (!total) return '0%';
        const percent = Math.max(0, Math.min(100, ((Number(value) || 0) / total) * 100));
        return `${percent.toFixed(2)}%`;
    };
    panel.style.setProperty('--live-stat-online', share(stats.online));
    panel.style.setProperty('--live-stat-idle', share(stats.idle));
    panel.style.setProperty('--live-stat-offline', share(stats.offline));
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

function selectCamera(camId) {
    currentSelectedCam = camId;
    updateSelectedRecordCameraBadge(camId);
    applySelectedCameraCardStyles();
    window.syncMobileStageDevicePicker?.();
    refreshPTZPanel();
    loadRecords(camId);
}

function previewLive(camId) {
    currentSelectedCam = camId;
    updateSelectedRecordCameraBadge(camId);
    applySelectedCameraCardStyles();
    window.syncMobileStageDevicePicker?.();
    refreshPTZPanel();
    playVideo(camId, true, `🟢 直播: ${camId}`);
    loadRecords(camId);
}

function openCameraLiveFromNode(camId) {
    previewLive(camId);
    if (isMobileCameraInteraction()) {
        window.CamKeepMobile?.setTab?.('monitor', {instant: true});
    }
}

function openCameraRecordsFromNode(camId) {
    selectCamera(camId);
    if (isMobileCameraInteraction()) {
        window.CamKeepMobile?.setTab?.('records', {instant: true});
    }
}

window.getMobileCameraEntries = function () {
    return Array.isArray(latestCameraStatusEntries) ? latestCameraStatusEntries.slice() : [];
};

window.getCurrentSelectedCam = function () {
    return currentSelectedCam || '';
};

window.selectCamera = selectCamera;
window.previewLive = previewLive;
window.openCameraLiveFromNode = openCameraLiveFromNode;
window.openCameraRecordsFromNode = openCameraRecordsFromNode;

function applySelectedCameraCardStyles() {
    const list = document.getElementById('camList');
    if (!list) return;
    Array.from(list.children).forEach(item => {
        if (!item.dataset.camId) return;
        item.classList.toggle('is-selected', item.dataset.camId === currentSelectedCam);
    });
}

window.addEventListener('camkeep:layoutchange', () => {
    cameraCardRenderKeys.clear();
    renderCameraListFromState();
});

document.addEventListener('click', event => {
    if (!event.target.closest('.camera-node-card-actions')) {
        collapseCameraNodeActions();
    }
});

document.addEventListener('keydown', event => {
    if (event.key === 'Escape') {
        collapseCameraNodeActions();
    }
});

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initCameraNodeSearch);
} else {
    initCameraNodeSearch();
}
