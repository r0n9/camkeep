(function () {
    const PANEL_HIDDEN_CLASS = 'hidden shrink-0 border-l border-gray-800 bg-slate-950 text-slate-100 transition-all duration-300';
    const MOVE_DURATION_MS = 700;
    const MOVE_RENEW_MS = 480;

    const state = {
        onvifStatusCache: new Map(),
        panelCollapsed: false,
        stopTimer: null,
        activeMove: null,
        actionInFlight: false,
        speedValue: 0.55
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
        panel.className = PANEL_HIDDEN_CLASS;
        panel.innerHTML = '';
    }

    function clearStopTimer() {
        if (state.stopTimer) {
            clearTimeout(state.stopTimer);
            state.stopTimer = null;
        }
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

    function controlButton(title, variantClass, path, disabled = '', pointerDownAction = null, pointerUpAction = null, clickAction = null) {
        const pointerAttrs = pointerDownAction ? `
                onpointerdown="${pointerDownAction}"
                onpointerup="${pointerUpAction || ''}"
                onpointercancel="${pointerUpAction || ''}"
                onlostpointercapture="${pointerUpAction || ''}"` : '';
        const clickAttr = clickAction ? ` onclick="${clickAction}"` : '';
        return `
        <button class="ptz-action-btn px-2 ${variantClass}"
                title="${title}"
                aria-label="${title}"
                ${clickAttr}${pointerAttrs}
                oncontextmenu="window.PTZ.suppressGesture(event)"
                ${disabled}>
            <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">${path}</svg>
            <span class="truncate">${title}</span>
        </button>
    `;
    }

    function renderPanel(camId, status) {
        const panel = getPanel();
        if (!panel || getActiveCamId() !== camId) return;
        const ptzAvailable = status && status.ptz_state === 'available';
        const imagingAvailable = status && status.imaging_state === 'available';
        if (status && status.ptz_state === 'unavailable' && !imagingAvailable) {
            hidePanel();
            return;
        }

        const text = panelStateText(status);
        const collapsedClass = state.panelCollapsed ? 'w-[34px]' : 'w-[236px]';
        panel.className = `ptz-panel-root shrink-0 border-l border-gray-800 bg-slate-950 text-slate-100 transition-all duration-300 ${collapsedClass}`;

        if (state.panelCollapsed) {
            panel.innerHTML = `
            <button onclick="window.PTZ.togglePanel(event)" class="ptz-collapse-tab flex h-full w-full flex-col items-center justify-center gap-1.5 text-slate-400 transition-colors hover:text-white" title="展开云台" aria-label="展开云台">
                <svg class="h-3.5 w-3.5 rotate-180" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M9 5l7 7-7 7"></path></svg>
                <span class="vertical-rl text-[11px] font-black tracking-normal">云台</span>
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
        panel.innerHTML = `
        <div class="custom-scrollbar flex h-full flex-col overflow-y-auto p-3">
            <div class="mb-3 flex items-center justify-between gap-2">
                <div class="min-w-0">
                    <div class="text-sm font-extrabold tracking-normal text-slate-100">云台</div>
                    <div id="ptz-feedback" class="mt-0.5 truncate text-[10px] font-bold text-slate-500">${escapeHtmlValue(text)}</div>
                </div>
                <button onclick="window.PTZ.togglePanel(event)" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-slate-800 bg-slate-900 text-slate-400 shadow-[0_6px_16px_-12px_rgba(2,6,23,0.9)] transition-colors hover:border-slate-700 hover:text-white" title="折叠云台" aria-label="折叠云台">
                    <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M9 5l7 7-7 7"></path></svg>
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

            <div class="ptz-tool-panel mt-2 p-1.5${disabledClass}">
                <div class="ptz-control-grid">
                    ${controlButton('拉近', 'ptz-zoom', '<path d="M12 5v14M5 12h14" />', disabled, 'window.PTZ.startMove(event, 0, 0, 1)', 'window.PTZ.stopMove(event)')}
                    ${controlButton('拉远', 'ptz-zoom', '<path d="M5 12h14" />', disabled, 'window.PTZ.startMove(event, 0, 0, -1)', 'window.PTZ.stopMove(event)')}
                </div>
            </div>

            <div class="ptz-tool-panel mt-1.5 p-1.5${imagingDisabledClass}">
                <div class="ptz-control-grid">
                    ${controlButton('近焦', 'ptz-focus', '<path d="M12 5v14M5 12h14" /><circle cx="12" cy="12" r="6" />', imagingDisabled, null, null, 'window.PTZ.adjustFocus(event, -1)')}
                    ${controlButton('远焦', 'ptz-focus', '<circle cx="12" cy="12" r="7" /><path d="M7 12h10" />', imagingDisabled, null, null, 'window.PTZ.adjustFocus(event, 1)')}
                </div>
            </div>

            <div class="ptz-tool-panel mt-1.5 p-1.5${imagingDisabledClass}">
                <div class="ptz-control-grid">
                    ${controlButton('开大', 'ptz-iris', '<circle cx="12" cy="12" r="7" /><path d="M12 8v8M8 12h8" />', imagingDisabled, null, null, 'window.PTZ.adjustIris(event, 1)')}
                    ${controlButton('收小', 'ptz-iris', '<circle cx="12" cy="12" r="7" /><path d="M8 12h8" />', imagingDisabled, null, null, 'window.PTZ.adjustIris(event, -1)')}
                </div>
            </div>

            <div class="ptz-speed-panel mt-2 flex items-center gap-2 rounded-lg border border-slate-800 bg-slate-900/70 px-2 py-1.5${speedDisabledClass}">
                <input id="ptz-speed" type="range" min="0.15" max="1" step="0.05" value="${state.speedValue}" oninput="window.PTZ.updateSpeedLabel()" aria-label="控制速度" class="block min-w-0 flex-1 accent-blue-500" ${speedDisabled}>
                <span id="ptz-speed-label" class="w-8 shrink-0 text-right font-mono text-[10px] font-black text-slate-300">55%</span>
            </div>
        </div>
    `;
        updateSpeedLabel();
    }

    async function refreshPanel(options = {}) {
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
        if (cached) renderPanel(camId, cached);
        const statusFromList = statusFromCachedCapability(camId);
        if (statusFromList) {
            state.onvifStatusCache.set(camId, {...cached, ...statusFromList});
            renderPanel(camId, state.onvifStatusCache.get(camId));
        }
        if (!options.force) {
            if (!cached && !statusFromList) hidePanel();
            return;
        }

        try {
            const resp = await fetch(`/api/camera/${encodeURIComponent(camId)}/ptz/status`);
            if (getActiveCamId() !== camId) return;
            if (!resp.ok) {
                state.onvifStatusCache.delete(camId);
                hidePanel();
                return;
            }
            const status = await resp.json();
            state.onvifStatusCache.set(camId, status);
            renderPanel(camId, status);
        } catch (e) {
            if (!cached) hidePanel();
        }
    }

    function togglePanel(event) {
        if (event) event.stopPropagation();
        state.panelCollapsed = !state.panelCollapsed;
        const camId = getActiveCamId();
        if (!camId || !canSendPTZCommand(camId)) return;
        renderPanel(camId, state.onvifStatusCache.get(camId));
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
        if (!stopCamId || !canSendPTZCommand(stopCamId)) return;
        await sendStop(stopCamId, force);
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
            if (!resp.ok) throw new Error(await readError(resp));
            setFeedback(successText);
        } catch (e) {
            setFeedback(e.message || `${label}失败`, true);
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

    window.PTZ = {
        adjustFocus,
        adjustIris,
        getActiveCamId,
        hidePanel,
        refreshPanel,
        startMove,
        stopMove,
        suppressGesture,
        togglePanel,
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
