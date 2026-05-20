(function () {
    const DAY_SECONDS = 24 * 60 * 60;
    const SIDE_PADDING = 44;
    const LANE_TOP = 48;
    const LANE_HEIGHT = 22;
    const MIN_SEGMENT_WIDTH = 6;
    const DRAG_START_THRESHOLD = 6;
    const WHEEL_ZOOM_THRESHOLD = 90;
    const PINCH_ZOOM_STEP_RATIO = 1.22;
    const MIN_LABEL_GAP_PX = 72;
    const fallbackDurationSeconds = 5 * 60;
    const states = new Map();
    const zoomLevels = [
        {label: '24H', trackWidth: 1040, tickSeconds: 2 * 60 * 60},
        {label: '12H', trackWidth: 1680, tickSeconds: 60 * 60},
        {label: '6H', trackWidth: 2640, tickSeconds: 30 * 60},
        {label: '3H', trackWidth: 4200, tickSeconds: 15 * 60},
        {label: '2H', trackWidth: 5600, tickSeconds: 10 * 60},
        {label: '1H', trackWidth: 8400, tickSeconds: 5 * 60},
        {label: '30m', trackWidth: 12600, tickSeconds: 2 * 60},
        {label: '10m', trackWidth: 25200, tickSeconds: 60},
        {label: '5m', trackWidth: 37800, tickSeconds: 30},
        {label: '1m', trackWidth: 75600, tickSeconds: 10}
    ];

    function create(options) {
        const {
            camId,
            date,
            entries,
            selectedRecordPath = '',
            initialViewportWidth = 0,
            onPlayAtTime = () => {}
        } = options || {};

        const wrapper = document.createElement('section');
        wrapper.className = 'record24h';

        const key = `${camId || ''}:${date || ''}`;
        const state = getState(key);
        const segments = buildSegments(entries || []);
        const unknownCount = (entries || []).filter(entry => !entry.meta || !entry.meta.hasStartTime).length;
        if (segments.length > 0 && !Number.isFinite(state.pointerSeconds)) {
            state.pointerSeconds = initialPointerSeconds(segments, selectedRecordPath, date);
        }

        const render = () => {
            wrapper.innerHTML = '';
            const zoom = zoomLevels[state.zoomIndex];

            wrapper.appendChild(createHeader(date, segments, unknownCount, state, render));
            if (segments.length === 0) {
                wrapper.appendChild(createEmptyState(unknownCount));
                return;
            }

            const status = createStatus();
            const viewport = document.createElement('div');
            viewport.className = 'record24h-viewport custom-scrollbar';

            const layout = layoutSegments(segments);
            const laneCount = 1;
            const axisTop = LANE_TOP + laneCount * LANE_HEIGHT + 18;
            const stageHeight = axisTop + 42;
            const viewportWidth = Math.max(0, wrapper.getBoundingClientRect().width || wrapper.clientWidth || initialViewportWidth || 0);
            const scale = effectiveScale(zoom, viewportWidth);
            const stageWidth = scale.trackWidth + SIDE_PADDING * 2;

            const stage = document.createElement('div');
            stage.className = 'record24h-stage';
            stage.style.width = `${stageWidth}px`;
            stage.style.height = `${stageHeight}px`;

            drawGrid(stage, scale, axisTop, stageHeight);
            drawSegments(stage, layout.segments, scale, selectedRecordPath);

            const pointer = createPointer(state.pointerSeconds, scale, axisTop);
            stage.appendChild(pointer.el);

            const updatePointer = (seconds) => {
                const nextSeconds = clampSeconds(seconds);
                state.pointerSeconds = nextSeconds;
                const x = secondsToX(nextSeconds, scale);
                pointer.el.style.left = `${x}px`;
                pointer.label.textContent = formatClock(nextSeconds, true);
                const hit = findSegmentAt(segments, nextSeconds);
                updateStatus(status, nextSeconds, hit);
            };

            const commitPointer = (seconds) => {
                const nextSeconds = clampSeconds(seconds);
                updatePointer(nextSeconds);
                const hit = findSegmentAt(segments, nextSeconds);
                if (!hit) return;
                const offsetSeconds = Math.max(0, Math.min(nextSeconds - hit.startSeconds, hit.durationSeconds));
                onPlayAtTime({
                    entry: hit.entry,
                    segment: hit,
                    seconds: nextSeconds,
                    timeLabel: formatClock(nextSeconds, true),
                    offsetSeconds
                });
            };

            enableTimelineInteraction({
                viewport,
                stage,
                zoom: scale,
                state,
                render,
                updatePointer,
                commitPointer
            });
            viewport.appendChild(stage);
            wrapper.appendChild(status.el);
            wrapper.appendChild(viewport);
            wrapper.appendChild(createFooter());

            requestAnimationFrame(() => {
                if (!viewport.isConnected) return;
                if (scale.fitToViewport) {
                    viewport.scrollLeft = 0;
                    updatePointer(state.pointerSeconds);
                    return;
                }
                const anchorSeconds = Number.isFinite(state.scrollAnchorSeconds)
                    ? state.scrollAnchorSeconds
                    : state.pointerSeconds;
                const anchorRatio = Number.isFinite(state.scrollAnchorRatio) ? state.scrollAnchorRatio : 0.42;
                const anchorX = secondsToX(anchorSeconds, scale);
                viewport.scrollLeft = Math.max(0, anchorX - viewport.clientWidth * anchorRatio);
                updatePointer(state.pointerSeconds);
            });
        };

        render();
        return wrapper;
    }

    function getState(key) {
        if (!states.has(key)) {
            states.set(key, {
                zoomIndex: 0,
                pointerSeconds: NaN,
                scrollAnchorSeconds: NaN,
                scrollAnchorRatio: 0.5
            });
        }
        return states.get(key);
    }

    function effectiveScale(zoom, viewportWidth) {
        if (zoom.label !== '24H') return zoom;
        const availableTrackWidth = Math.max(320, viewportWidth - SIDE_PADDING * 2 - 2);
        return {...zoom, trackWidth: availableTrackWidth, fitToViewport: true};
    }

    function createHeader(date, segments, unknownCount, state, render) {
        const header = document.createElement('div');
        header.className = 'record24h-header';

        const realCount = segments.filter(segment => !segment.estimated).length;
        const estimatedCount = segments.length - realCount;
        const coverageText = segments.length > 0
            ? `${segments.length} 段 · ${formatDuration(totalCoverageSeconds(segments))} 覆盖`
            : '无可定位录像';

        const title = document.createElement('div');
        title.className = 'record24h-title';
        title.innerHTML = `
            <strong>24H 录像分布</strong>
            <span>${escapeHtml(date || '录像日')} · ${coverageText}${estimatedCount ? ` · ${estimatedCount} 段固定区间` : ''}${unknownCount ? ` · ${unknownCount} 段未识别` : ''}</span>
        `;

        const actions = document.createElement('div');
        actions.className = 'record24h-actions';

        const timeBadge = document.createElement('div');
        timeBadge.className = 'record24h-time-badge';
        timeBadge.dataset.record24hTimeBadge = 'true';
        timeBadge.textContent = Number.isFinite(state.pointerSeconds) ? formatClock(state.pointerSeconds, true) : '--:--:--';

        const zoom = document.createElement('div');
        zoom.className = 'record24h-zoom';
        const zoomOut = document.createElement('button');
        zoomOut.type = 'button';
        zoomOut.title = '缩小时间轴';
        zoomOut.textContent = '-';
        zoomOut.disabled = state.zoomIndex <= 0;
        zoomOut.onclick = () => {
            setZoomIndex(state, state.zoomIndex - 1);
            render();
        };

        const zoomLabel = document.createElement('span');
        zoomLabel.className = 'record24h-zoom-level';
        zoomLabel.textContent = zoomLevels[state.zoomIndex].label;

        const zoomIn = document.createElement('button');
        zoomIn.type = 'button';
        zoomIn.title = '放大时间轴';
        zoomIn.textContent = '+';
        zoomIn.disabled = state.zoomIndex >= zoomLevels.length - 1;
        zoomIn.onclick = () => {
            setZoomIndex(state, state.zoomIndex + 1);
            render();
        };

        zoom.appendChild(zoomOut);
        zoom.appendChild(zoomLabel);
        zoom.appendChild(zoomIn);
        actions.appendChild(timeBadge);
        actions.appendChild(zoom);
        header.appendChild(title);
        header.appendChild(actions);
        return header;
    }

    function createStatus() {
        const el = document.createElement('div');
        el.className = 'record24h-status';

        const hit = document.createElement('div');
        hit.className = 'record24h-hit';

        const hint = document.createElement('div');
        hint.className = 'record24h-hint';
        hint.textContent = '拖动平移 · 滚轮缩放 · 点击定位播放 · 双指缩放';

        el.appendChild(hit);
        el.appendChild(hint);
        return {el, hit};
    }

    function updateStatus(status, seconds, segment) {
        const badge = status.el.parentElement
            ? status.el.parentElement.querySelector('[data-record24h-time-badge]')
            : null;
        if (badge) badge.textContent = formatClock(seconds, true);

        if (!segment) {
            status.hit.textContent = `${formatClock(seconds, true)} · 该时间点无录像`;
            return;
        }
        const range = `${formatClock(segment.startSeconds, true)}-${formatClock(segment.endSeconds, true)}`;
        const estimated = segment.estimated ? '固定区间' : '真实区间';
        status.hit.textContent = `${formatClock(seconds, true)} · ${segment.kind} · ${range} · ${estimated}`;
    }

    function createEmptyState(unknownCount) {
        const empty = document.createElement('div');
        empty.className = 'record24h-empty';
        empty.innerHTML = `
            <strong>该日没有可定位录像</strong>
            <span>${unknownCount ? `${unknownCount} 个文件缺少可识别开始时间，可切回卡片视图处理。` : '录像文件名中没有可解析的开始时间。'}</span>
        `;
        return empty;
    }

    function createFooter() {
        const footer = document.createElement('div');
        footer.className = 'record24h-footer';
        footer.innerHTML = `
            <span class="record24h-legend"><span class="record24h-legend-dot"></span>有开始和结束时间</span>
            <span class="record24h-legend"><span class="record24h-legend-dot is-estimated"></span>只有开始时间，按固定 ${formatDuration(fallbackDurationSeconds)} 展示</span>
        `;
        return footer;
    }

    function drawGrid(stage, zoom, axisTop, stageHeight) {
        const lanes = document.createElement('div');
        lanes.className = 'record24h-lanes';
        lanes.style.top = `${LANE_TOP}px`;
        lanes.style.height = `${Math.max(20, axisTop - LANE_TOP - 16)}px`;
        stage.appendChild(lanes);

        const axis = document.createElement('div');
        axis.className = 'record24h-axis';
        axis.style.left = `${SIDE_PADDING}px`;
        axis.style.top = `${axisTop}px`;
        axis.style.width = `${zoom.trackWidth}px`;
        stage.appendChild(axis);

        const labelStep = chooseLabelStep(zoom);
        for (let seconds = 0; seconds <= DAY_SECONDS; seconds += zoom.tickSeconds) {
            const x = secondsToX(seconds, zoom);
            const strong = isStrongTick(seconds, zoom);

            const gridLine = document.createElement('div');
            gridLine.className = `record24h-grid-line ${strong ? 'is-strong' : ''}`;
            gridLine.style.left = `${x}px`;
            gridLine.style.top = '36px';
            gridLine.style.height = `${stageHeight - 84}px`;
            stage.appendChild(gridLine);

            const tick = document.createElement('div');
            tick.className = `record24h-tick ${strong ? '' : 'is-minor'}`;
            tick.style.left = `${x}px`;
            tick.style.top = `${axisTop - (strong ? 13 : 8)}px`;
            tick.style.height = `${strong ? 22 : 14}px`;
            stage.appendChild(tick);

            if (seconds % labelStep !== 0) continue;
            const label = document.createElement('div');
            label.className = 'record24h-label';
            label.style.left = `${x}px`;
            label.style.top = `${axisTop + 18}px`;
            label.textContent = formatClock(seconds, false);
            stage.appendChild(label);
        }
    }

    function chooseLabelStep(zoom) {
        const secondsPerPixel = DAY_SECONDS / zoom.trackWidth;
        const minLabelSeconds = secondsPerPixel * MIN_LABEL_GAP_PX;
        const candidates = [
            60,
            2 * 60,
            5 * 60,
            10 * 60,
            15 * 60,
            30 * 60,
            60 * 60,
            2 * 60 * 60,
            3 * 60 * 60,
            6 * 60 * 60
        ];
        return candidates.find(seconds => seconds >= minLabelSeconds && seconds % zoom.tickSeconds === 0)
            || candidates.find(seconds => seconds >= minLabelSeconds)
            || 6 * 60 * 60;
    }

    function isStrongTick(seconds, zoom) {
        if (seconds % 3600 === 0) return true;
        if (zoom.tickSeconds >= 30 * 60) return seconds % (2 * 60 * 60) === 0;
        if (zoom.tickSeconds >= 5 * 60) return seconds % (30 * 60) === 0;
        return seconds % (10 * 60) === 0;
    }

    function drawSegments(stage, segments, zoom, selectedRecordPath) {
        segments.forEach(segment => {
            const left = secondsToX(segment.startSeconds, zoom);
            const right = secondsToX(segment.endSeconds, zoom);
            const bar = document.createElement('button');
            bar.type = 'button';
            bar.className = `record24h-segment ${segment.estimated ? 'is-estimated' : 'is-real'}`;
            bar.dataset.recordAxisPath = segment.path;
            bar.classList.toggle('is-selected', selectedRecordPath !== '' && segment.path === selectedRecordPath);
            bar.style.left = `${left}px`;
            bar.style.top = `${LANE_TOP + 3}px`;
            bar.style.width = `${Math.max(MIN_SEGMENT_WIDTH, right - left)}px`;
            bar.title = `${segment.kind} · ${formatClock(segment.startSeconds, true)}-${formatClock(segment.endSeconds, true)}${segment.estimated ? ' · 固定区间' : ''}`;
            bar.innerHTML = `<span class="record24h-segment-label">${escapeHtml(segment.kind)}</span>`;
            stage.appendChild(bar);
        });
    }

    function createPointer(seconds, zoom, axisTop) {
        const el = document.createElement('div');
        el.className = 'record24h-pointer';
        el.style.top = '30px';
        el.style.height = `${Math.max(42, axisTop - 24)}px`;
        el.style.left = `${secondsToX(seconds, zoom)}px`;

        const label = document.createElement('div');
        label.className = 'record24h-pointer-label';
        label.textContent = formatClock(seconds, true);
        el.appendChild(label);
        return {el, label};
    }

    function enableTimelineInteraction(options) {
        const {
            viewport,
            stage,
            zoom,
            state,
            render,
            updatePointer,
            commitPointer
        } = options;
        const activePointers = new Map();
        let drag = null;
        let pinch = null;
        let wheelZoomDelta = 0;
        let wheelZoomTimer = null;

        const rememberViewportAnchor = (clientX, seconds = null) => {
            const rect = viewport.getBoundingClientRect();
            const ratio = rect.width > 0 ? clamp((clientX - rect.left) / rect.width, 0, 1) : 0.5;
            state.scrollAnchorRatio = ratio;
            state.scrollAnchorSeconds = Number.isFinite(seconds) ? seconds : viewportClientXToSeconds(clientX, viewport, zoom);
        };

        const zoomAround = (clientX, direction) => {
            const anchorSeconds = viewportClientXToSeconds(clientX, viewport, zoom);
            rememberViewportAnchor(clientX, anchorSeconds);
            if (setZoomIndex(state, state.zoomIndex + direction)) {
                render();
                return true;
            }
            return false;
        };

        viewport.addEventListener('wheel', event => {
            const horizontalWheel = Math.abs(event.deltaX) > Math.abs(event.deltaY) || event.shiftKey;
            if (horizontalWheel) {
                event.preventDefault();
                const panDelta = normalizeWheelDelta(event, event.deltaX || event.deltaY, viewport.clientWidth);
                viewport.scrollLeft += panDelta;
                return;
            }

            event.preventDefault();
            wheelZoomDelta += normalizeWheelDelta(event, event.deltaY, viewport.clientWidth);
            if (Math.abs(wheelZoomDelta) >= WHEEL_ZOOM_THRESHOLD) {
                const direction = wheelZoomDelta > 0 ? -1 : 1;
                wheelZoomDelta = 0;
                zoomAround(event.clientX, direction);
            }
            if (wheelZoomTimer) clearTimeout(wheelZoomTimer);
            wheelZoomTimer = setTimeout(() => {
                wheelZoomDelta = 0;
                wheelZoomTimer = null;
            }, 180);
        }, {passive: false});

        stage.addEventListener('pointerdown', event => {
            if (event.button !== 0 && event.pointerType === 'mouse') return;
            activePointers.set(event.pointerId, {clientX: event.clientX, clientY: event.clientY});
            stage.setPointerCapture(event.pointerId);

            if (activePointers.size === 2) {
                const [first, second] = [...activePointers.values()];
                const centerX = (first.clientX + second.clientX) / 2;
                pinch = {
                    startDistance: pointerDistance(first, second),
                    currentDistance: pointerDistance(first, second),
                    centerX,
                    startZoomIndex: state.zoomIndex
                };
                drag = null;
                stage.classList.add('is-pinching');
                rememberViewportAnchor(centerX);
                event.preventDefault();
                return;
            }

            const seconds = eventSeconds(event, stage, zoom);
            drag = {
                pointerId: event.pointerId,
                startX: event.clientX,
                startY: event.clientY,
                startScrollLeft: viewport.scrollLeft,
                active: false,
                seconds
            };
            updatePointer(seconds);
            event.preventDefault();
        });

        stage.addEventListener('pointermove', event => {
            if (activePointers.has(event.pointerId)) {
                activePointers.set(event.pointerId, {clientX: event.clientX, clientY: event.clientY});
            }

            if (pinch && activePointers.size >= 2) {
                const [first, second] = [...activePointers.values()];
                const distance = pointerDistance(first, second);
                if (pinch.startDistance > 0 && distance > 0) {
                    pinch.currentDistance = distance;
                }
                event.preventDefault();
                return;
            }

            if (!drag || drag.pointerId !== event.pointerId) return;
            const deltaX = event.clientX - drag.startX;
            const deltaY = event.clientY - drag.startY;
            if (!drag.active) {
                if (Math.hypot(deltaX, deltaY) < DRAG_START_THRESHOLD) return;
                drag.active = true;
                stage.classList.add('is-dragging');
            }
            viewport.scrollLeft = drag.startScrollLeft - deltaX;
            updatePointer(eventSeconds(event, stage, zoom));
            event.preventDefault();
        });

        const applyPinchZoom = () => {
            if (!pinch || pinch.startDistance <= 0 || pinch.currentDistance <= 0) return false;
            const ratio = pinch.currentDistance / pinch.startDistance;
            const targetZoomIndex = pinchZoomTargetIndex(pinch.startZoomIndex, ratio);
            if (targetZoomIndex === state.zoomIndex) return false;
            state.scrollAnchorSeconds = viewportClientXToSeconds(pinch.centerX, viewport, zoom);
            state.scrollAnchorRatio = viewportClientXToRatio(pinch.centerX, viewport);
            state.zoomIndex = targetZoomIndex;
            render();
            return true;
        };

        const finishPointer = event => {
            const cancelled = event.type === 'pointercancel';
            const wasDrag = drag && drag.pointerId === event.pointerId ? drag : null;
            activePointers.delete(event.pointerId);
            if (stage.hasPointerCapture(event.pointerId)) {
                stage.releasePointerCapture(event.pointerId);
            }

            if (pinch && activePointers.size < 2) {
                if (!cancelled) applyPinchZoom();
                pinch = null;
                stage.classList.remove('is-pinching');
                event.preventDefault();
                return;
            }

            if (!wasDrag) return;
            drag = null;
            stage.classList.remove('is-dragging');
            const seconds = eventSeconds(event, stage, zoom);
            updatePointer(seconds);
            if (!cancelled && !wasDrag.active) {
                rememberViewportAnchor(event.clientX, seconds);
                commitPointer(seconds);
            }
            event.preventDefault();
        };

        stage.addEventListener('pointerup', finishPointer);
        stage.addEventListener('pointercancel', finishPointer);
        stage.addEventListener('lostpointercapture', event => {
            activePointers.delete(event.pointerId);
            if (drag && drag.pointerId === event.pointerId) {
                drag = null;
                stage.classList.remove('is-dragging');
            }
            if (activePointers.size < 2) {
                pinch = null;
                stage.classList.remove('is-pinching');
            }
        });
    }

    function buildSegments(entries) {
        return (entries || [])
            .filter(entry => entry && entry.meta && entry.meta.hasStartTime)
            .map((entry, index) => {
                const meta = entry.meta;
                const startSeconds = clampSeconds(meta.startSeconds);
                const rawEnd = Number.isFinite(meta.timelineEndSeconds)
                    ? meta.timelineEndSeconds
                    : meta.hasEndTime && Number.isFinite(meta.endSeconds)
                        ? meta.endSeconds
                        : startSeconds + fallbackDurationSeconds;
                const endSeconds = Math.max(startSeconds + 1, clampSeconds(rawEnd));
                const path = entryPath(entry);
                return {
                    entry,
                    index,
                    path,
                    kind: meta.kind || '录像',
                    startSeconds,
                    endSeconds,
                    durationSeconds: Math.max(1, endSeconds - startSeconds),
                    estimated: !meta.hasEndTime
                };
            })
            .sort((a, b) => a.startSeconds - b.startSeconds || a.endSeconds - b.endSeconds || a.index - b.index);
    }

    function layoutSegments(segments) {
        return {
            segments: segments.map(segment => ({...segment, lane: 0})),
            laneCount: 1
        };
    }

    function initialPointerSeconds(segments, selectedRecordPath, date) {
        const selected = segments.find(segment => selectedRecordPath !== '' && segment.path === selectedRecordPath);
        if (selected) return selected.startSeconds;
        if (date && isToday(date)) {
            const now = new Date();
            return now.getHours() * 3600 + now.getMinutes() * 60 + now.getSeconds();
        }
        return segments[0].startSeconds;
    }

    function findSegmentAt(segments, seconds) {
        const hitSeconds = clampSeconds(seconds);
        const hits = segments.filter(segment => hitSeconds >= segment.startSeconds && hitSeconds <= segment.endSeconds);
        if (hits.length === 0) return null;
        return hits.sort((a, b) => {
            if (a.estimated !== b.estimated) return a.estimated ? 1 : -1;
            return a.durationSeconds - b.durationSeconds;
        })[0];
    }

    function totalCoverageSeconds(segments) {
        if (segments.length === 0) return 0;
        const ranges = segments
            .map(segment => [segment.startSeconds, segment.endSeconds])
            .sort((a, b) => a[0] - b[0]);
        let total = 0;
        let currentStart = ranges[0][0];
        let currentEnd = ranges[0][1];
        for (let i = 1; i < ranges.length; i++) {
            const [start, end] = ranges[i];
            if (start <= currentEnd) {
                currentEnd = Math.max(currentEnd, end);
            } else {
                total += currentEnd - currentStart;
                currentStart = start;
                currentEnd = end;
            }
        }
        return total + currentEnd - currentStart;
    }

    function eventSeconds(event, stage, zoom) {
        const rect = stage.getBoundingClientRect();
        const x = event.clientX - rect.left;
        const ratio = (x - SIDE_PADDING) / zoom.trackWidth;
        return Math.round(clamp(ratio, 0, 1) * DAY_SECONDS);
    }

    function viewportClientXToSeconds(clientX, viewport, zoom) {
        const rect = viewport.getBoundingClientRect();
        const x = clientX - rect.left + viewport.scrollLeft;
        const ratio = (x - SIDE_PADDING) / zoom.trackWidth;
        return Math.round(clamp(ratio, 0, 1) * DAY_SECONDS);
    }

    function viewportClientXToRatio(clientX, viewport) {
        const rect = viewport.getBoundingClientRect();
        return rect.width > 0 ? clamp((clientX - rect.left) / rect.width, 0, 1) : 0.5;
    }

    function secondsToX(seconds, zoom) {
        return SIDE_PADDING + (clampSeconds(seconds) / DAY_SECONDS) * zoom.trackWidth;
    }

    function setZoomIndex(state, zoomIndex) {
        const nextIndex = clampZoomIndex(zoomIndex);
        if (nextIndex === state.zoomIndex) return false;
        state.zoomIndex = nextIndex;
        return true;
    }

    function clampZoomIndex(zoomIndex) {
        return Math.min(zoomLevels.length - 1, Math.max(0, Math.round(zoomIndex)));
    }

    function pinchZoomTargetIndex(startZoomIndex, ratio) {
        if (!Number.isFinite(ratio) || ratio <= 0) return startZoomIndex;
        if (ratio >= PINCH_ZOOM_STEP_RATIO) {
            return clampZoomIndex(startZoomIndex + Math.floor(Math.log(ratio) / Math.log(PINCH_ZOOM_STEP_RATIO)));
        }
        if (ratio <= 1 / PINCH_ZOOM_STEP_RATIO) {
            return clampZoomIndex(startZoomIndex - Math.floor(Math.log(1 / ratio) / Math.log(PINCH_ZOOM_STEP_RATIO)));
        }
        return startZoomIndex;
    }

    function normalizeWheelDelta(event, value, pageSize) {
        if (event.deltaMode === 1) return value * 16;
        if (event.deltaMode === 2) return value * pageSize;
        return value;
    }

    function pointerDistance(first, second) {
        return Math.hypot(first.clientX - second.clientX, first.clientY - second.clientY);
    }

    function clampSeconds(seconds) {
        const value = Number(seconds);
        if (!Number.isFinite(value)) return 0;
        return Math.min(DAY_SECONDS, Math.max(0, value));
    }

    function clamp(value, min, max) {
        return Math.min(max, Math.max(min, value));
    }

    function formatClock(seconds, withSeconds) {
        const normalized = clampSeconds(Math.round(seconds));
        if (normalized >= DAY_SECONDS) return withSeconds ? '24:00:00' : '24:00';
        const hour = Math.floor(normalized / 3600);
        const minute = Math.floor((normalized % 3600) / 60);
        const second = normalized % 60;
        const base = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
        return withSeconds ? `${base}:${String(second).padStart(2, '0')}` : base;
    }

    function formatDuration(seconds) {
        const normalized = Math.max(0, Math.round(seconds));
        if (normalized >= 3600) {
            const hours = Math.floor(normalized / 3600);
            const minutes = Math.round((normalized % 3600) / 60);
            return minutes ? `${hours}h${minutes}m` : `${hours}h`;
        }
        const minutes = Math.max(1, Math.round(normalized / 60));
        return `${minutes}m`;
    }

    function entryPath(entry) {
        const file = entry && entry.file;
        return String((file && (file.path || file.url || file.name)) || '').trim();
    }

    function isToday(dateKey) {
        const now = new Date();
        const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
        return dateKey === today;
    }

    function escapeHtml(value) {
        return String(value ?? '').replace(/[&<>"']/g, char => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#39;'
        }[char]));
    }

    window.RecordTimeline24h = {create};
})();
