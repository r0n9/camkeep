(function () {
    const PANEL_BASE_CLASS = 'player-ptz-panel ptz-panel-root';
    const PANEL_HIDDEN_CLASS = 'player-ptz-panel ptz-panel-root hidden';
    const MOVE_DURATION_MS = 700;
    const MOVE_RENEW_MS = 480;
    const PANEL_ENTER_MS = 320;

    const state = {
        onvifStatusCache: new Map(),
        panelCollapsedByMode: {desktop: true, mobile: false},
        panelCollapsed: true,
        stopTimer: null,
        activeMove: null,
        actionInFlight: false,
        lightEnabledByCamera: new Map(),
        speedValue: 0.55,
        speedDragging: false,
        lastRenderKey: '',
        pendingRender: null,
        panelAnimationTimer: null
    };

    function getPanel() {
        return document.getElementById('ptz-panel');
    }

    function getActiveCamId() {
        if (typeof window.getActiveLiveCamId !== 'function') return '';
        return String(window.getActiveLiveCamId() || '').trim();
    }

    function getCachedCapability(camId) {
        if (!window.cameraCapabilityCache || typeof window.cameraCapabilityCache.get !== 'function') return null;
        return window.cameraCapabilityCache.get(camId) || null;
    }

    function canProbePTZ(camId) {
        const capability = getCachedCapability(camId);
        return !capability || capability.onvif_enabled !== false;
    }

    function canSendPTZCommand(camId) {
        const capability = getCachedCapability(camId);
        return !capability || capability.onvif_enabled !== false;
    }

    function statusFromCachedCapability(camId) {
        const capability = getCachedCapability(camId);
        if (!capability || capability.onvif_enabled !== true) return null;
        return {
            capability_state: capability.capability_state || 'not_probed',
            ptz_state: capability.ptz_state || 'not_probed',
            imaging_state: capability.imaging_state || 'not_probed'
        };
    }

    function isMobileViewport() {
        return document.documentElement?.dataset?.layoutMode === 'mobile';
    }

    function currentPanelMode() {
        return isMobileViewport() ? 'mobile' : 'desktop';
    }

    function syncMobileDockVisibility(panel = getPanel()) {
        const dock = document.getElementById('mobilePtzDock');
        if (!dock) return;
        const visible = Boolean(panel && !panel.classList.contains('hidden') && isMobileViewport());
        dock.hidden = !visible;
        dock.setAttribute('aria-hidden', visible ? 'false' : 'true');
        dock.classList.toggle('is-expanded', visible && panel.classList.contains('is-expanded'));
        dock.classList.toggle('is-collapsed', visible && panel.classList.contains('is-collapsed'));
    }

    function getPanelCollapsed(mode = currentPanelMode()) {
        if (!state.panelCollapsedByMode || typeof state.panelCollapsedByMode !== 'object') {
            state.panelCollapsedByMode = {desktop: true, mobile: false};
        }
        if (Object.prototype.hasOwnProperty.call(state.panelCollapsedByMode, mode)) {
            return Boolean(state.panelCollapsedByMode[mode]);
        }
        return mode === 'mobile' ? false : true;
    }

    function setPanelCollapsed(collapsed, mode = currentPanelMode()) {
        if (!state.panelCollapsedByMode || typeof state.panelCollapsedByMode !== 'object') {
            state.panelCollapsedByMode = {desktop: true, mobile: false};
        }
        state.panelCollapsedByMode[mode] = Boolean(collapsed);
        state.panelCollapsed = Boolean(collapsed);
    }

    function escapeHtmlValue(value) {
        if (typeof window.escapeHtml === 'function') return window.escapeHtml(value);
        return String(value).replace(/[&<>"']/g, char => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#39;'
        })[char]);
    }

    function hidePanel() {
        const panel = getPanel();
        if (!panel) return;
        if (state.activeMove) void stopMove(null, true);
        state.speedDragging = false;
        clearStopTimer();
        clearPanelAnimation();
        panel.className = PANEL_HIDDEN_CLASS;
        panel.setAttribute('aria-hidden', 'true');
        panel.innerHTML = '';
        state.lastRenderKey = '';
        state.pendingRender = null;
        syncMobileDockVisibility(panel);
    }

    function hidePanelWhenSafe() {
        if (panelInteractionActive()) {
            state.pendingRender = {hide: true};
            return;
        }
        hidePanel();
    }

    function clearStopTimer() {
        if (state.stopTimer) {
            clearTimeout(state.stopTimer);
            state.stopTimer = null;
        }
    }

    function clearPanelAnimation() {
        if (state.panelAnimationTimer) {
            clearTimeout(state.panelAnimationTimer);
            state.panelAnimationTimer = null;
        }
    }

    function schedulePanelEnterCleanup(panel) {
        clearPanelAnimation();
        state.panelAnimationTimer = setTimeout(() => {
            panel.classList.remove('is-entering');
            state.panelAnimationTimer = null;
        }, PANEL_ENTER_MS);
    }

    function clearMoveRenewTimer(move) {
        if (move && move.renewTimer) {
            clearTimeout(move.renewTimer);
            move.renewTimer = null;
        }
    }

    function friendlyText(text) {
        return String(text || '').replace(/PTZ\s*/g, '云台').trim();
    }

    function stateText(status) {
        if (!status) return '未探测';
        if (status.ptz_state === 'available') return '就绪';
        if (status.capability_state === 'probing' || status.ptz_state === 'probing') return '探测中';
        if (status.capability_state === 'error' || status.ptz_state === 'error') return friendlyText(status.last_error || '探测失败');
        return '不可用';
    }

    function panelStateText(status) {
        if (!status) return '未探测';
        const ptzReady = status.ptz_state === 'available';
        const imagingReady = status.imaging_state === 'available';
        if (ptzReady && imagingReady) return '云台/成像就绪';
        if (ptzReady) return '云台就绪';
        if (imagingReady) return '聚焦/光圈就绪';
        return stateText(status);
    }

    function setFeedback(text, isError = false) {
        const feedback = document.getElementById('ptz-feedback');
        if (!feedback) return;
        feedback.innerText = friendlyText(text);
        feedback.classList.toggle('text-rose-400', isError);
        feedback.classList.toggle('text-slate-500', !isError);
    }

    async function readError(resp) {
        try {
            const payload = await resp.json();
            return friendlyText(payload.error || '云台请求失败');
        } catch (_) {
            return '云台请求失败';
        }
    }

    function currentSpeed() {
        const slider = document.getElementById('ptz-speed');
        const speed = Number(slider?.value || state.speedValue || 0.55);
        if (!Number.isFinite(speed)) return state.speedValue || 0.55;
        return Math.min(1, Math.max(0.15, speed));
    }

    function currentImagingStep() {
        return Math.min(0.25, Math.max(0.02, currentSpeed() * 0.12));
    }

    function updateSpeedLabel() {
        const slider = document.getElementById('ptz-speed');
        const label = document.getElementById('ptz-speed-label');
        if (!slider) return;
        state.speedValue = currentSpeed();
        const speedText = `${Math.round(state.speedValue * 100)}%`;
        if (label) label.innerText = speedText;
        slider.title = speedText;
        slider.setAttribute('aria-valuetext', speedText);
    }

    function getLightEnabled(camId) {
        return Boolean(state.lightEnabledByCamera.get(camId));
    }

    function setLightEnabled(camId, enabled) {
        if (!camId) return;
        state.lightEnabledByCamera.set(camId, Boolean(enabled));
    }

    function panelRenderKey(camId, status) {
        return JSON.stringify({
            camId,
            mode: currentPanelMode(),
            collapsed: state.panelCollapsed,
            light_enabled: getLightEnabled(camId),
            capability_state: status?.capability_state || '',
            ptz_state: status?.ptz_state || '',
            imaging_state: status?.imaging_state || '',
            last_error: status?.last_error || ''
        });
    }

    function panelInteractionActive() {
        return Boolean(state.activeMove || state.speedDragging);
    }

    function renderPanelIfNeeded(camId, status, options = {}) {
        const panel = getPanel();
        if (!panel || getActiveCamId() !== camId) return;
        if (!options.force && panelInteractionActive()) {
            state.pendingRender = {camId, status};
            return;
        }

        const renderKey = panelRenderKey(camId, status);
        if (!options.force && state.lastRenderKey === renderKey) return;

        renderPanel(camId, status);
        state.lastRenderKey = renderKey;
        state.pendingRender = null;
    }

    function flushPendingRender() {
        const pending = state.pendingRender;
        if (!pending || panelInteractionActive()) return;
        state.pendingRender = null;
        if (pending.hide) {
            hidePanel();
            return;
        }
        renderPanelIfNeeded(pending.camId, pending.status);
    }

    function moveButton(title, x, y, zoom, path, disabled = '') {
        const variantClass = zoom !== 0 ? 'ptz-zoom' : 'ptz-direction';
        return `
        <button class="ptz-btn ${variantClass}"
                title="${title}"
                aria-label="${title}"
                onpointerdown="window.PTZ.startMove(event, ${x}, ${y}, ${zoom})"
                onpointerup="window.PTZ.stopMove(event)"
                onpointercancel="window.PTZ.stopMove(event)"
                onlostpointercapture="window.PTZ.stopMove(event)"
                oncontextmenu="window.PTZ.suppressGesture(event)"
                ${disabled}>
            <svg class="h-4 w-4" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">${path}</svg>
        </button>
    `;
    }

    function controlButton(title, variantClass, path, disabled = '', pointerDownAction = null, pointerUpAction = null, clickAction = null, iconClass = '') {
        const pointerAttrs = pointerDownAction ? `
                onpointerdown="${pointerDownAction}"
                onpointerup="${pointerUpAction || ''}"
                onpointercancel="${pointerUpAction || ''}"
                onlostpointercapture="${pointerUpAction || ''}"` : '';
        const clickAttr = clickAction ? ` onclick="${clickAction}"` : '';
        const iconMarkup = iconClass
            ? `<span class="ptz-action-icon ${iconClass}" aria-hidden="true"></span>`
            : `<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">${path}</svg>`;
        return `
        <button class="ptz-action-btn px-2 ${variantClass}"
                title="${title}"
                aria-label="${title}"
                ${clickAttr}${pointerAttrs}
                oncontextmenu="window.PTZ.suppressGesture(event)"
                ${disabled}>
            ${iconMarkup}
            <span class="truncate">${title}</span>
        </button>
    `;
    }

    function renderPanel(camId, status) {
        const panel = getPanel();
        if (!panel || getActiveCamId() !== camId) return;
        const mode = currentPanelMode();
        const ptzAvailable = Boolean(status && status.ptz_state === 'available');
        const imagingAvailable = Boolean(status && status.imaging_state === 'available');

        state.panelCollapsed = getPanelCollapsed(mode);

        const text = panelStateText(status);
        const collapsedClass = state.panelCollapsed ? 'is-collapsed' : 'is-expanded';
        const mobileDockedClass = mode === 'mobile' ? 'is-mobile-docked' : '';
        const enteringClass = !state.panelCollapsed && panel.classList.contains('is-collapsed') ? 'is-entering' : '';
        panel.className = `${PANEL_BASE_CLASS} ${collapsedClass} ${mobileDockedClass} ${enteringClass}`.trim();

        panel.setAttribute('aria-hidden', 'false');
        syncMobileDockVisibility(panel);

        if (state.panelCollapsed) {
            clearPanelAnimation();
            panel.innerHTML = `
            <button onclick="window.PTZ.togglePanel(event)" class="ptz-collapse-tab flex h-full w-full items-center justify-center text-slate-400 transition-colors hover:text-white" title="展开云台" aria-label="展开云台">
                <svg class="h-4 w-4" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-width="2.2" d="M12 5v14M5 12h14"></path>
                    <path stroke-width="1.9" d="M8.5 8.5 5 12l3.5 3.5M15.5 8.5 19 12l-3.5 3.5M8.5 8.5 12 5l3.5 3.5M8.5 15.5 12 19l3.5-3.5"></path>
                </svg>
            </button>
        `;
            return;
        }

        const disabled = ptzAvailable ? '' : 'disabled';
        const disabledClass = ptzAvailable ? '' : ' opacity-45 pointer-events-none';
        const imagingDisabled = imagingAvailable ? '' : 'disabled';
        const imagingDisabledClass = imagingAvailable ? '' : ' opacity-45 pointer-events-none';
        const speedDisabled = ptzAvailable || imagingAvailable ? '' : 'disabled';
        const speedDisabledClass = ptzAvailable || imagingAvailable ? '' : ' opacity-45 pointer-events-none';
        const lightDisabled = status ? '' : 'disabled';
        const lightEnabled = getLightEnabled(camId);
        const lightButtonClass = lightEnabled ? 'ptz-light is-active' : 'ptz-light';
        const lightButtonTitle = lightEnabled ? '灯光关' : '灯光开';
        panel.innerHTML = `
        <div class="custom-scrollbar flex h-full flex-col overflow-y-auto p-3 sm:p-3.5">
            <div class="ptz-panel-header mb-3 flex items-center gap-2">
                <div class="ptz-panel-heading min-w-0">
                    <div class="ptz-panel-title text-[15px] font-extrabold tracking-normal text-slate-100">云台</div>
                    <div id="ptz-feedback" class="ptz-panel-feedback mt-0.5 truncate text-[10px] font-bold text-slate-500">${escapeHtmlValue(text)}</div>
                </div>
                <div class="ptz-header-speed-panel${speedDisabledClass}">
                    <input id="ptz-speed" type="range" min="0.15" max="1" step="0.05" value="${state.speedValue}" oninput="window.PTZ.updateSpeedLabel()" onpointerdown="window.PTZ.beginSpeedInteraction(event)" onpointerup="window.PTZ.endSpeedInteraction(event)" onpointercancel="window.PTZ.endSpeedInteraction(event)" onlostpointercapture="window.PTZ.endSpeedInteraction(event)" onfocus="window.PTZ.beginSpeedInteraction()" onblur="window.PTZ.endSpeedInteraction()" aria-label="控制速度" class="block min-w-0 flex-1 accent-blue-500" ${speedDisabled}>
                    <span id="ptz-speed-label" class="ptz-speed-label shrink-0 text-right font-mono text-[10px] font-black text-slate-300">55%</span>
                </div>
                <button onclick="window.PTZ.togglePanel(event)" class="ptz-panel-collapse-btn flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-slate-800 bg-slate-900 text-slate-400 shadow-[0_6px_16px_-12px_rgba(2,6,23,0.9)] transition-colors hover:border-slate-700 hover:text-white" title="折叠云台" aria-label="折叠云台">
                    <svg class="h-[17px] w-[17px]" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M9 5l7 7-7 7"></path></svg>
                </button>
            </div>

            <div class="ptz-dial grid grid-cols-3 gap-1.5${disabledClass}">
                ${moveButton('左上', -1, 1, 0, '<path d="M7 17V7h10M7 7l10 10" />', disabled)}
                ${moveButton('上', 0, 1, 0, '<path d="M12 19V5m0 0l-6 6m6-6l6 6" />', disabled)}
                ${moveButton('右上', 1, 1, 0, '<path d="M17 17V7H7m10 0L7 17" />', disabled)}
                ${moveButton('左', -1, 0, 0, '<path d="M19 12H5m0 0l6-6m-6 6l6 6" />', disabled)}
                <button onclick="window.PTZ.stopMove(event, true)" oncontextmenu="window.PTZ.suppressGesture(event)" class="ptz-btn ptz-stop" title="停止" aria-label="停止" ${disabled}>
                    <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><rect x="8" y="8" width="8" height="8" rx="1.5" stroke-width="2.2"></rect></svg>
                </button>
                ${moveButton('右', 1, 0, 0, '<path d="M5 12h14m0 0l-6-6m6 6l-6 6" />', disabled)}
                ${moveButton('左下', -1, -1, 0, '<path d="M7 7v10h10M7 17L17 7" />', disabled)}
                ${moveButton('下', 0, -1, 0, '<path d="M12 5v14m0 0l-6-6m6 6l6-6" />', disabled)}
                ${moveButton('右下', 1, -1, 0, '<path d="M17 7v10H7m10 0L7 7" />', disabled)}
            </div>

            <div class="ptz-tool-panel ptz-combined-tool-panel mt-2 p-1.5 sm:p-1.5">
                <div class="ptz-control-grid">
                    ${controlButton('拉近', 'ptz-zoom', '', disabled, 'window.PTZ.startMove(event, 0, 0, 1)', 'window.PTZ.stopMove(event)', null, 'ptz-icon-zoom-in')}
                    ${controlButton('拉远', 'ptz-zoom', '', disabled, 'window.PTZ.startMove(event, 0, 0, -1)', 'window.PTZ.stopMove(event)', null, 'ptz-icon-zoom-out')}
                    ${controlButton('近焦', 'ptz-focus', '', imagingDisabled, null, null, 'window.PTZ.adjustFocus(event, -1)', 'ptz-icon-focus-near')}
                    ${controlButton('远焦', 'ptz-focus', '', imagingDisabled, null, null, 'window.PTZ.adjustFocus(event, 1)', 'ptz-icon-focus-far')}
                    ${controlButton('开大', 'ptz-iris', '', imagingDisabled, null, null, 'window.PTZ.adjustIris(event, 1)', 'ptz-icon-iris-open')}
                    ${controlButton('收小', 'ptz-iris', '', imagingDisabled, null, null, 'window.PTZ.adjustIris(event, -1)', 'ptz-icon-iris-close')}
                    ${controlButton('自动聚焦', 'ptz-autofocus', '', imagingDisabled, null, null, 'window.PTZ.autoFocus(event)', 'ptz-icon-autofocus')}
                    ${controlButton(lightButtonTitle, lightButtonClass, '', lightDisabled, null, null, 'window.PTZ.toggleLight(event)', 'ptz-icon-light')}
                </div>
            </div>
        </div>
    `;
        if (enteringClass) schedulePanelEnterCleanup(panel);
        updateSpeedLabel();
    }

    async function refreshPanel(options = {}) {
        if (options.expanded === true) {
            state.panelCollapsed = false;
        }
        const camId = getActiveCamId();
        if (!camId) {
            hidePanel();
            return;
        }
        if (!canProbePTZ(camId)) {
            state.onvifStatusCache.delete(camId);
            hidePanel();
            return;
        }

        const cached = state.onvifStatusCache.get(camId);
        if (cached) renderPanelIfNeeded(camId, cached);
        const statusFromList = statusFromCachedCapability(camId);
        if (statusFromList) {
            state.onvifStatusCache.set(camId, {...cached, ...statusFromList});
            renderPanelIfNeeded(camId, state.onvifStatusCache.get(camId));
        }
        if (!options.force) {
            if (!cached && !statusFromList) hidePanelWhenSafe();
            return;
        }

        try {
            const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/ptz/status`);
            if (getActiveCamId() !== camId) return;
            if (!resp.ok) {
                state.onvifStatusCache.delete(camId);
                hidePanelWhenSafe();
                return;
            }
            const status = await resp.json();
            state.onvifStatusCache.set(camId, status);
            renderPanelIfNeeded(camId, status);
        } catch (e) {
            if (!cached) hidePanelWhenSafe();
        }
    }

    function togglePanel(event) {
        if (event) event.stopPropagation();
        const mode = currentPanelMode();
        setPanelCollapsed(!getPanelCollapsed(mode), mode);
        const camId = getActiveCamId();
        if (!camId || !canSendPTZCommand(camId)) return;
        renderPanelIfNeeded(camId, state.onvifStatusCache.get(camId));
    }

    function suppressGesture(event) {
        if (!event) return;
        event.preventDefault();
        event.stopPropagation();
        clearPTZSelection();
    }

    function isInsidePTZ(target) {
        return Boolean(target && target.closest && target.closest('#ptz-panel'));
    }

    function clearPTZSelection() {
        const selection = window.getSelection && window.getSelection();
        if (selection && !selection.isCollapsed) selection.removeAllRanges();
    }

    function shouldHandlePointer(event) {
        return !event || event.isPrimary !== false;
    }

    function capturePointer(event) {
        if (event.currentTarget && event.pointerId !== undefined) {
            try {
                event.currentTarget.setPointerCapture(event.pointerId);
            } catch (_) {
            }
        }
    }

    function releasePointer(move) {
        if (!move || !move.target || move.pointerId === undefined) return;
        try {
            if (move.target.hasPointerCapture && move.target.hasPointerCapture(move.pointerId)) {
                move.target.releasePointerCapture(move.pointerId);
            }
        } catch (_) {
        }
    }

    async function startMove(event, x, y, zoom) {
        suppressGesture(event);
        if (!shouldHandlePointer(event)) return;

        const camId = getActiveCamId();
        if (!camId || !canSendPTZCommand(camId)) return;

        if (state.activeMove) {
            await stopMove(null, true);
        }

        capturePointer(event);
        clearStopTimer();
        const target = event?.currentTarget || null;
        if (target) target.classList.add('is-pressing');

        const move = {
            camId,
            pointerId: event?.pointerId,
            target,
            x,
            y,
            zoom,
            stopped: false,
            requestInFlight: false,
            renewTimer: null,
            needsStopAfterRequest: false
        };
        state.activeMove = move;
        setFeedback('移动中');
        void sendMovePulse(move);
    }

    async function sendMovePulse(move) {
        const camId = getActiveCamId();
        if (!move || move.stopped || state.activeMove !== move || camId !== move.camId || move.requestInFlight || !canSendPTZCommand(camId)) return;

        const speed = currentSpeed();
        move.requestInFlight = true;
        try {
            const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/ptz/move`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({
                    x: move.x * speed,
                    y: move.y * speed,
                    zoom: move.zoom * speed,
                    duration_ms: MOVE_DURATION_MS
                })
            });
            if (!resp.ok) throw new Error(await readError(resp));
            if (move.stopped || state.activeMove !== move) {
                move.needsStopAfterRequest = true;
                return;
            }
            move.renewTimer = setTimeout(() => sendMovePulse(move), MOVE_RENEW_MS);
        } catch (e) {
            if (state.activeMove === move) {
                setFeedback(e.message || '云台指令失败', true);
                void stopMove(null, true);
            }
        } finally {
            move.requestInFlight = false;
            if (move.needsStopAfterRequest) {
                move.needsStopAfterRequest = false;
                void sendStop(move.camId, true);
            }
        }
    }

    async function stopMove(event, force = false) {
        suppressGesture(event);
        if (!shouldHandlePointer(event)) return;

        const move = state.activeMove;
        if (!move && !event) return;
        if (!move && !force) return;
        if (event && move && move.pointerId !== undefined && event.pointerId !== move.pointerId && !force) {
            return;
        }
        clearStopTimer();
        clearMoveRenewTimer(move);
        if (move) {
            move.stopped = true;
            if (move.target) move.target.classList.remove('is-pressing');
            releasePointer(move);
            if (move.requestInFlight) move.needsStopAfterRequest = true;
        }
        state.activeMove = null;

        const camId = getActiveCamId();
        const stopCamId = move?.camId || camId;
        if (!stopCamId || !canSendPTZCommand(stopCamId)) {
            flushPendingRender();
            return;
        }
        await sendStop(stopCamId, force);
        flushPendingRender();
    }

    async function sendStop(camId, force = false) {
        if (!canSendPTZCommand(camId)) return;
        try {
            const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/ptz/stop`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({pan_tilt: true, zoom: true})
            });
            if (!resp.ok && force) throw new Error(await readError(resp));
            setFeedback('就绪');
        } catch (e) {
            if (force) setFeedback(e.message || '云台停止失败', true);
        }
    }

    async function adjustImaging(event, kind, direction, label, successText) {
        if (event) {
            event.preventDefault();
            event.stopPropagation();
        }
        const camId = getActiveCamId();
        if (!camId || state.actionInFlight || !canSendPTZCommand(camId)) return;

        const step = currentImagingStep();
        state.actionInFlight = true;
        setFeedback(`${label}中`);
        try {
            const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/ptz/${kind}`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({
                    direction,
                    step
                })
            });
            if (!resp.ok) {
                const detail = await readError(resp);
                console.error(`[PTZ] ${label}接口调用失败`, {
                    camera_id: camId,
                    status: resp.status,
                    error: detail
                });
                setFeedback(`${label}调用失败，可能不支持`, true);
                return;
            }
            setFeedback(successText);
        } catch (e) {
            console.error(`[PTZ] ${label}接口调用失败`, e);
            setFeedback(`${label}调用失败，可能不支持`, true);
        } finally {
            state.actionInFlight = false;
        }
    }

    async function adjustFocus(event, direction) {
        await adjustImaging(event, 'focus', direction, '聚焦', '聚焦已下发');
    }

    async function adjustIris(event, direction) {
        await adjustImaging(event, 'iris', direction, '光圈', '光圈已调整');
    }

    async function autoFocus(event) {
        if (event) {
            event.preventDefault();
            event.stopPropagation();
        }
        const camId = getActiveCamId();
        if (!camId || state.actionInFlight || !canSendPTZCommand(camId)) return;

        state.actionInFlight = true;
        setFeedback('自动对焦中');
        try {
            const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/ptz/autofocus`, {
                method: 'POST'
            });
            if (!resp.ok) {
                const detail = await readError(resp);
                console.error('[PTZ] 自动对焦接口调用失败', {
                    camera_id: camId,
                    status: resp.status,
                    error: detail
                });
                setFeedback('自动对焦调用失败，可能不支持', true);
                return;
            }
            setFeedback('自动对焦已下发');
        } catch (e) {
            console.error('[PTZ] 自动对焦接口调用失败', e);
            setFeedback('自动对焦调用失败，可能不支持', true);
        } finally {
            state.actionInFlight = false;
        }
    }

    async function toggleLight(event) {
        if (event) {
            event.preventDefault();
            event.stopPropagation();
        }
        const camId = getActiveCamId();
        if (!camId || state.actionInFlight || !canSendPTZCommand(camId)) return;

        const nextEnabled = !getLightEnabled(camId);
        state.actionInFlight = true;
        setFeedback(nextEnabled ? '灯光开启中' : '灯光关闭中');
        try {
            const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/ptz/light`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({enabled: nextEnabled})
            });
            if (!resp.ok) {
                const detail = await readError(resp);
                console.error('[PTZ] 灯光控制接口调用失败', {
                    camera_id: camId,
                    status: resp.status,
                    error: detail
                });
                setFeedback('灯光控制失败，可能不支持', true);
                return;
            }
            setLightEnabled(camId, nextEnabled);
            renderPanelIfNeeded(camId, state.onvifStatusCache.get(camId), {force: true});
            setFeedback(nextEnabled ? '灯光已开启' : '灯光已关闭');
        } catch (e) {
            console.error('[PTZ] 灯光控制接口调用失败', e);
            setFeedback('灯光控制失败，可能不支持', true);
        } finally {
            state.actionInFlight = false;
        }
    }

    function beginSpeedInteraction(event) {
        if (event && !shouldHandlePointer(event)) return;
        state.speedDragging = true;
        if (event) capturePointer(event);
    }

    function endSpeedInteraction(event) {
        if (event && !shouldHandlePointer(event)) return;
        if (event?.currentTarget) releasePointer({
            target: event.currentTarget,
            pointerId: event.pointerId
        });
        state.speedDragging = false;
        flushPendingRender();
    }

    window.PTZ = {
        adjustFocus,
        adjustIris,
        autoFocus,
        beginSpeedInteraction,
        endSpeedInteraction,
        getActiveCamId,
        hidePanel,
        refreshPanel,
        startMove,
        stopMove,
        suppressGesture,
        togglePanel,
        toggleLight,
        updateSpeedLabel
    };

    window.addEventListener('blur', () => stopMove(null, true));
    window.addEventListener('pagehide', () => stopMove(null, true));
    document.addEventListener('visibilitychange', () => {
        if (document.hidden) void stopMove(null, true);
    });
    document.addEventListener('selectstart', event => {
        if (!isInsidePTZ(event.target)) return;
        suppressGesture(event);
    });
    document.addEventListener('selectionchange', () => {
        const selection = window.getSelection && window.getSelection();
        if (!selection || selection.isCollapsed) return;
        const anchor = selection.anchorNode && (selection.anchorNode.nodeType === Node.ELEMENT_NODE
            ? selection.anchorNode
            : selection.anchorNode.parentElement);
        const focus = selection.focusNode && (selection.focusNode.nodeType === Node.ELEMENT_NODE
            ? selection.focusNode
            : selection.focusNode.parentElement);
        if (isInsidePTZ(anchor) || isInsidePTZ(focus)) selection.removeAllRanges();
    });
})();
