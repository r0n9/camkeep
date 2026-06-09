(function () {
    const DAY_SECONDS = 24 * 60 * 60;
    const SIDE_PADDING = 44;
    const LANE_TOP = 48;
    const LANE_HEIGHT = 22;
    const MIN_SEGMENT_WIDTH = 6;
    const DRAG_START_THRESHOLD = 6;
    const WHEEL_ZOOM_THRESHOLD = 120;
    const WHEEL_ZOOM_REPEAT_MS = 180;
    const WHEEL_ZOOM_RESET_MS = 260;
    const ZOOM_ANIMATION_MS = 220;
    const ZOOM_GESTURE_PREVIEW_SCALE = 0.045;
    const PINCH_ZOOM_STEP_RATIO = 1.3;
    const MIN_LABEL_GAP_PX = 72;
    const MAX_ELASTIC_PAN_PX = 34;
    const fallbackDurationSeconds = 5 * 60;
    const states = new Map();
    const zoomLevels = [
        {label: '24H', trackWidth: 1040, tickSeconds: 2 * 60 * 60},
        {label: '18H', trackWidth: 1320, tickSeconds: 60 * 60},
        {label: '12H', trackWidth: 1680, tickSeconds: 60 * 60},
        {label: '8H', trackWidth: 2160, tickSeconds: 30 * 60},
        {label: '6H', trackWidth: 2640, tickSeconds: 30 * 60},
        {label: '4H', trackWidth: 3420, tickSeconds: 15 * 60},
        {label: '3H', trackWidth: 4200, tickSeconds: 15 * 60},
        {label: '2H', trackWidth: 5600, tickSeconds: 10 * 60},
        {label: '1H', trackWidth: 8400, tickSeconds: 5 * 60},
        {label: '45m', trackWidth: 10080, tickSeconds: 5 * 60},
        {label: '30m', trackWidth: 12600, tickSeconds: 2 * 60},
        {label: '20m', trackWidth: 16800, tickSeconds: 2 * 60},
        {label: '10m', trackWidth: 25200, tickSeconds: 60},
        {label: '5m', trackWidth: 37800, tickSeconds: 30},
        {label: '2m', trackWidth: 56700, tickSeconds: 20},
        {label: '1m', trackWidth: 75600, tickSeconds: 10}
    ];

    function create(options) {
        const {
            camId,
            date,
            entries,
            markers = [],
            selectedRecordPath = '',
            initialViewportWidth = 0,
            onPlayAtTime = () => {},
            onClearPlayback = () => {}
        } = options || {};

        const wrapper = document.createElement('section');
        wrapper.className = 'record24h';

        const key = `${camId || ''}:${date || ''}`;
        const state = getState(key);
        const segments = buildSegments(entries || []);
        const markerSegments = buildMarkerSegments(markers || [], date);
        const unknownCount = (entries || []).filter(entry => !entry.meta || !entry.meta.hasStartTime).length;
        if (segments.length > 0 && !Number.isFinite(state.pointerSeconds)) {
            state.pointerSeconds = initialPointerSeconds(segments, selectedRecordPath, date);
        }

        const render = () => {
            wrapper.innerHTML = '';
            const zoom = zoomLevels[state.zoomIndex];
            const zoomBy = direction => requestZoom(
                state,
                state.zoomIndex + direction,
                state.pointerSeconds,
                0.5,
                render
            );

            wrapper.appendChild(createHeader(date, segments, markerSegments, unknownCount, state, zoomBy));
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
            const stageWidth = scale.trackWidth + axisPadding(scale) * 2;

            const stage = document.createElement('div');
            stage.className = 'record24h-stage';
            stage.style.width = `${stageWidth}px`;
            stage.style.height = `${stageHeight}px`;
            applyZoomMotion(stage, state);

            drawGrid(stage, scale, axisTop, stageHeight);
            drawSegments(stage, layout.segments, scale, selectedRecordPath);
            drawMarkers(stage, markerSegments, scale);

            const pointer = createPointer(state.pointerSeconds, axisTop);
            const timelineShell = document.createElement('div');
            timelineShell.className = 'record24h-timeline-shell';

            const updatePointer = (seconds) => {
                const nextSeconds = clampSeconds(seconds);
                state.pointerSeconds = nextSeconds;
                pointer.label.textContent = formatClock(nextSeconds, true);
                const hit = findSegmentAt(segments, nextSeconds);
                const marker = findMarkerAt(markerSegments, nextSeconds);
                updateStatus(status, nextSeconds, hit, marker);
            };

            const commitPointer = (seconds) => {
                const nextSeconds = clampSeconds(seconds);
                updatePointer(nextSeconds);
                const hit = findSegmentAt(segments, nextSeconds);
                if (!hit) {
                    onClearPlayback({
                        seconds: nextSeconds,
                        timeLabel: formatClock(nextSeconds, true)
                    });
                    return;
                }
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
            timelineShell.appendChild(viewport);
            timelineShell.appendChild(pointer.el);
            wrapper.appendChild(status.el);
            wrapper.appendChild(timelineShell);
            wrapper.appendChild(createFooter());

            const restoreViewportAnchor = () => {
                if (!viewport.isConnected || viewport.clientWidth <= 0) return;
                const anchorSeconds = Number.isFinite(state.scrollAnchorSeconds)
                    ? state.scrollAnchorSeconds
                    : state.pointerSeconds;
                const anchorRatio = Number.isFinite(state.scrollAnchorRatio) ? state.scrollAnchorRatio : 0.5;
                const anchorX = secondsToX(anchorSeconds, scale);
                const maxScrollLeft = Math.max(0, viewport.scrollWidth - viewport.clientWidth);
                viewport.scrollLeft = clamp(anchorX - viewport.clientWidth * anchorRatio, 0, maxScrollLeft);
                updatePointer(viewportCenterSeconds(viewport, scale));
                state.scrollAnchorSeconds = state.pointerSeconds;
                state.scrollAnchorRatio = 0.5;
            };
            restoreViewportAnchor();
            requestAnimationFrame(restoreViewportAnchor);
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
                scrollAnchorRatio: 0.5,
                lastWheelZoomAt: 0
            });
        }
        return states.get(key);
    }

    function effectiveScale(zoom, viewportWidth) {
        // 留出半个视口的左右空白，让 00:00 和 24:00 也能滚到中心指针下。
        const sidePadding = Math.max(SIDE_PADDING, Math.round(viewportWidth / 2));
        if (zoom.label !== '24H') return {...zoom, sidePadding};
        const availableTrackWidth = Math.max(320, viewportWidth - SIDE_PADDING * 2 - 2);
        return {...zoom, trackWidth: availableTrackWidth, fitToViewport: true, sidePadding};
    }

    function requestZoom(state, targetZoomIndex, anchorSeconds, anchorRatio, render) {
        const previousIndex = state.zoomIndex;
        const nextIndex = clampZoomIndex(targetZoomIndex);
        if (nextIndex === previousIndex) return false;

        state.scrollAnchorSeconds = clampSeconds(anchorSeconds);
        state.scrollAnchorRatio = Number.isFinite(anchorRatio) ? clamp(anchorRatio, 0, 1) : 0.5;
        state.zoomDirection = Math.sign(nextIndex - previousIndex);
        // 时间轴缩放仍落到固定档位，用轻微的反向缩放过渡，减少重绘后的突跳感。
        state.zoomFromScale = nextIndex > previousIndex ? 1 - ZOOM_GESTURE_PREVIEW_SCALE : 1 + ZOOM_GESTURE_PREVIEW_SCALE;
        state.zoomAnimationToken = (state.zoomAnimationToken || 0) + 1;
        state.zoomIndex = nextIndex;
        render();
        return true;
    }

    function applyZoomMotion(stage, state) {
        const direction = Number(state.zoomDirection) || 0;
        if (direction === 0) return;

        const token = state.zoomAnimationToken || 0;
        stage.style.setProperty('--record24h-zoom-from-scale', String(state.zoomFromScale || 1));
        stage.classList.add(direction > 0 ? 'is-zooming-in' : 'is-zooming-out');
        setTimeout(() => {
            if (state.zoomAnimationToken === token) state.zoomDirection = 0;
        }, ZOOM_ANIMATION_MS);
    }

    function createHeader(date, segments, markerSegments, unknownCount, state, zoomBy) {
        const header = document.createElement('div');
        header.className = 'record24h-header';

        const realCount = segments.filter(segment => !segment.estimated).length;
        const estimatedCount = segments.length - realCount;
        const coverageText = segments.length > 0
            ? `${segments.length} 段 · ${formatDuration(totalCoverageSeconds(segments))} 覆盖`
            : '无可定位录像';
        const markerText = markerSegments.length > 0 ? ` · 动检 ${markerSegments.length} 段` : '';

        const title = document.createElement('div');
        title.className = 'record24h-title';
        title.innerHTML = `
            <strong>24H 录像分布</strong>
            <span>${escapeHtml(date || '录像日')} · ${coverageText}${markerText}${estimatedCount ? ` · ${estimatedCount} 段固定区间` : ''}${unknownCount ? ` · ${unknownCount} 段未识别` : ''}</span>
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
            zoomBy(-1);
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
            zoomBy(1);
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
        hint.textContent = '中线指针固定 · 拖动时间轴定位播放 · 点击定位播放 · 滚轮/双指缩放';

        el.appendChild(hit);
        el.appendChild(hint);
        return {el, hit};
    }

    function updateStatus(status, seconds, segment, marker = null) {
        const badge = status.el.parentElement
            ? status.el.parentElement.querySelector('[data-record24h-time-badge]')
            : null;
        if (badge) badge.textContent = formatClock(seconds, true);

        if (!segment) {
            status.hit.textContent = marker
                ? `${formatClock(seconds, true)} · 该时间点无录像 · ${markerLabel(marker)}`
                : `${formatClock(seconds, true)} · 该时间点无录像`;
            return;
        }
        const range = `${formatClock(segment.startSeconds, true)}-${formatClock(segment.endSeconds, true)}`;
        const estimated = segment.estimated ? '固定区间' : '真实区间';
        status.hit.textContent = `${formatClock(seconds, true)} · ${segment.kind} · ${range} · ${estimated}${marker ? ` · ${markerLabel(marker)}` : ''}`;
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
            <span class="record24h-legend"><span class="record24h-legend-dot is-motion-marker"></span>动检标记</span>
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
        axis.style.left = `${axisPadding(zoom)}px`;
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

    function drawMarkers(stage, markers, zoom) {
        markers.forEach(marker => {
            const left = secondsToX(marker.startSeconds, zoom);
            const right = secondsToX(marker.endSeconds, zoom);
            const bar = document.createElement('span');
            bar.className = `record24h-marker is-${marker.sourceKey}`;
            bar.style.left = `${left}px`;
            bar.style.top = `${LANE_TOP + 22}px`;
            bar.style.width = `${Math.max(4, right - left)}px`;
            bar.title = `${markerLabel(marker)} · ${formatClock(marker.startSeconds, true)}-${formatClock(marker.endSeconds, true)}${marker.topic ? ` · ${marker.topic}` : ''}`;
            stage.appendChild(bar);
        });
    }

    function createPointer(seconds, axisTop) {
        const el = document.createElement('div');
        el.className = 'record24h-pointer';
        el.style.top = '30px';
        el.style.height = `${Math.max(42, axisTop - 24)}px`;

        const label = document.createElement('div');
        label.className = 'record24h-pointer-label';
        label.textContent = formatClock(seconds, true);

        const handle = document.createElement('span');
        handle.className = 'record24h-pointer-handle';
        handle.title = '播放指针固定在时间轴窗口中心';

        el.appendChild(label);
        el.appendChild(handle);
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
        let elasticPanOffset = 0;
        let scrollSyncFrame = 0;

        const setElasticPanOffset = (offset) => {
            const nextOffset = Math.abs(offset) < 0.5 ? 0 : offset;
            if (Math.abs(nextOffset - elasticPanOffset) < 0.5) return;
            elasticPanOffset = nextOffset;
            stage.style.setProperty('--record24h-elastic-x', `${nextOffset.toFixed(1)}px`);
        };

        const settleElasticPanOffset = () => {
            if (elasticPanOffset === 0) return;
            setElasticPanOffset(0);
        };

        const syncPointerToViewportCenter = () => {
            const centerSeconds = viewportCenterSeconds(viewport, zoom);
            updatePointer(centerSeconds);
            state.scrollAnchorSeconds = centerSeconds;
            state.scrollAnchorRatio = 0.5;
        };

        const schedulePointerCenterSync = () => {
            if (scrollSyncFrame) return;
            scrollSyncFrame = requestAnimationFrame(() => {
                scrollSyncFrame = 0;
                if (viewport.isConnected) syncPointerToViewportCenter();
            });
        };

        const setGesturePreview = (scale, originX) => {
            stage.style.setProperty('--record24h-gesture-scale', scale.toFixed(3));
            stage.style.setProperty('--record24h-transform-origin', `${originX.toFixed(1)}px 50%`);
        };

        const clearGesturePreview = () => {
            stage.classList.remove('is-wheel-zooming');
            stage.style.removeProperty('--record24h-gesture-scale');
            stage.style.removeProperty('--record24h-transform-origin');
        };

        const setPinchPreview = ratio => {
            const previewScale = clamp(Math.pow(Math.max(0.01, ratio), 0.35), 1 - ZOOM_GESTURE_PREVIEW_SCALE * 1.8, 1 + ZOOM_GESTURE_PREVIEW_SCALE * 1.8);
            const rect = stage.getBoundingClientRect();
            const originX = rect.width > 0 ? clamp(pinch.centerX - rect.left, 0, rect.width) : rect.width / 2;
            setGesturePreview(previewScale, originX);
        };

        const setWheelZoomPreview = () => {
            const progress = clamp(Math.abs(wheelZoomDelta) / WHEEL_ZOOM_THRESHOLD, 0, 1);
            const direction = wheelZoomDelta > 0 ? -1 : 1;
            const previewScale = 1 + direction * progress * ZOOM_GESTURE_PREVIEW_SCALE;
            const originX = viewport.scrollLeft + viewport.clientWidth / 2;
            stage.classList.add('is-wheel-zooming');
            setGesturePreview(previewScale, originX);
        };

        const applyPanDrag = (deltaX) => {
            const maxScrollLeft = Math.max(0, viewport.scrollWidth - viewport.clientWidth);
            const desiredScrollLeft = drag.startScrollLeft - deltaX;
            const nextScrollLeft = clamp(desiredScrollLeft, 0, maxScrollLeft);
            viewport.scrollLeft = nextScrollLeft;
            syncPointerToViewportCenter();

            const boundaryOverflow = nextScrollLeft - desiredScrollLeft;
            setElasticPanOffset(rubberbandPanOffset(boundaryOverflow, viewport.clientWidth));
        };

        const rememberViewportAnchor = (clientX, seconds = null) => {
            const rect = viewport.getBoundingClientRect();
            const ratio = rect.width > 0 ? clamp((clientX - rect.left) / rect.width, 0, 1) : 0.5;
            state.scrollAnchorRatio = ratio;
            state.scrollAnchorSeconds = Number.isFinite(seconds) ? seconds : viewportClientXToSeconds(clientX, viewport, zoom);
        };

        const rememberViewportCenter = () => {
            const rect = viewport.getBoundingClientRect();
            rememberViewportAnchor(rect.left + rect.width / 2);
        };

        const zoomAroundCenter = (direction) => {
            return requestZoom(
                state,
                state.zoomIndex + direction,
                viewportCenterSeconds(viewport, zoom),
                0.5,
                render
            );
        };

        viewport.addEventListener('wheel', event => {
            const horizontalWheel = Math.abs(event.deltaX) > Math.abs(event.deltaY) || event.shiftKey;
            if (horizontalWheel) {
                event.preventDefault();
                const panDelta = normalizeWheelDelta(event, event.deltaX || event.deltaY, viewport.clientWidth);
                viewport.scrollLeft += panDelta;
                syncPointerToViewportCenter();
                rememberViewportCenter();
                return;
            }

            event.preventDefault();
            wheelZoomDelta += normalizeWheelDelta(event, event.deltaY, viewport.clientWidth);
            setWheelZoomPreview();
            const now = performance.now();
            if (Math.abs(wheelZoomDelta) >= WHEEL_ZOOM_THRESHOLD && now - (state.lastWheelZoomAt || 0) >= WHEEL_ZOOM_REPEAT_MS) {
                const direction = wheelZoomDelta > 0 ? -1 : 1;
                clearGesturePreview();
                if (zoomAroundCenter(direction)) {
                    state.lastWheelZoomAt = now;
                    const remaining = Math.max(0, Math.abs(wheelZoomDelta) - WHEEL_ZOOM_THRESHOLD);
                    wheelZoomDelta = Math.sign(wheelZoomDelta) * Math.min(remaining, WHEEL_ZOOM_THRESHOLD * 0.35);
                } else {
                    wheelZoomDelta = 0;
                }
            }
            if (wheelZoomTimer) clearTimeout(wheelZoomTimer);
            wheelZoomTimer = setTimeout(() => {
                wheelZoomDelta = 0;
                wheelZoomTimer = null;
                clearGesturePreview();
            }, WHEEL_ZOOM_RESET_MS);
        }, {passive: false});

        viewport.addEventListener('scroll', schedulePointerCenterSync, {passive: true});

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
                stage.classList.remove('is-dragging', 'is-scrubbing');
                settleElasticPanOffset();
                clearGesturePreview();
                stage.classList.add('is-pinching');
                setPinchPreview(1);
                rememberViewportAnchor(centerX);
                event.preventDefault();
                return;
            }

            drag = {
                pointerId: event.pointerId,
                mode: 'pan',
                startX: event.clientX,
                startY: event.clientY,
                startScrollLeft: viewport.scrollLeft,
                active: false
            };
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
                    pinch.centerX = (first.clientX + second.clientX) / 2;
                    setPinchPreview(distance / pinch.startDistance);
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
            applyPanDrag(deltaX);
            event.preventDefault();
        });

        const applyPinchZoom = () => {
            if (!pinch || pinch.startDistance <= 0 || pinch.currentDistance <= 0) return false;
            const ratio = pinch.currentDistance / pinch.startDistance;
            const targetZoomIndex = pinchZoomTargetIndex(pinch.startZoomIndex, ratio);
            if (targetZoomIndex === state.zoomIndex) return false;
            return requestZoom(
                state,
                targetZoomIndex,
                viewportClientXToSeconds(pinch.centerX, viewport, zoom),
                viewportClientXToRatio(pinch.centerX, viewport),
                render
            );
        };

        const finishPointer = event => {
            const cancelled = event.type === 'pointercancel';
            const wasDrag = drag && drag.pointerId === event.pointerId ? drag : null;
            activePointers.delete(event.pointerId);
            if (stage.hasPointerCapture(event.pointerId)) {
                stage.releasePointerCapture(event.pointerId);
            }

            if (pinch && activePointers.size < 2) {
                const zoomed = !cancelled && applyPinchZoom();
                if (!zoomed) clearGesturePreview();
                pinch = null;
                stage.classList.remove('is-pinching');
                event.preventDefault();
                return;
            }

            if (!wasDrag) return;
            drag = null;
            stage.classList.remove('is-dragging', 'is-scrubbing');
            settleElasticPanOffset();
            const seconds = eventSeconds(event, stage, zoom);
            if (!cancelled && !wasDrag.active) {
                centerViewportOnSeconds(viewport, seconds, zoom);
                syncPointerToViewportCenter();
                rememberViewportCenter();
                commitPointer(state.pointerSeconds);
            } else if (!cancelled && wasDrag.active) {
                syncPointerToViewportCenter();
                rememberViewportCenter();
                commitPointer(state.pointerSeconds);
            }
            event.preventDefault();
        };

        stage.addEventListener('pointerup', finishPointer);
        stage.addEventListener('pointercancel', finishPointer);
        stage.addEventListener('lostpointercapture', event => {
            activePointers.delete(event.pointerId);
            if (drag && drag.pointerId === event.pointerId) {
                drag = null;
                stage.classList.remove('is-dragging', 'is-scrubbing');
                settleElasticPanOffset();
            }
            if (activePointers.size < 2) {
                pinch = null;
                stage.classList.remove('is-pinching');
                clearGesturePreview();
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

    function buildMarkerSegments(markers, dateKey) {
        const dayStart = dateKeyToLocalTime(dateKey);
        if (!Number.isFinite(dayStart)) return [];
        const dayEnd = dayStart + DAY_SECONDS * 1000;

        return (markers || [])
            .map((marker, index) => {
                const startMs = Date.parse(marker && marker.start);
                const endMs = Date.parse(marker && marker.end);
                if (!Number.isFinite(startMs) || !Number.isFinite(endMs) || endMs <= startMs) return null;

                const clippedStart = clamp(startMs, dayStart, dayEnd);
                const clippedEnd = clamp(endMs, dayStart, dayEnd);
                if (clippedEnd <= clippedStart) return null;

                const startSeconds = Math.floor((clippedStart - dayStart) / 1000);
                const endSeconds = Math.ceil((clippedEnd - dayStart) / 1000);
                const source = String(marker.source || '').trim();
                return {
                    index,
                    source,
                    sourceKey: markerSourceKey(source),
                    topic: String(marker.topic || '').trim(),
                    score: Number(marker.score),
                    reason: String(marker.reason || '').trim(),
                    startSeconds: clampSeconds(startSeconds),
                    endSeconds: Math.max(clampSeconds(startSeconds) + 1, clampSeconds(endSeconds))
                };
            })
            .filter(Boolean)
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

    function findMarkerAt(markers, seconds) {
        const hitSeconds = clampSeconds(seconds);
        const hits = markers.filter(marker => hitSeconds >= marker.startSeconds && hitSeconds <= marker.endSeconds);
        if (hits.length === 0) return null;
        return hits.sort((a, b) => (a.endSeconds - a.startSeconds) - (b.endSeconds - b.startSeconds))[0];
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
        const ratio = (x - axisPadding(zoom)) / zoom.trackWidth;
        return Math.round(clamp(ratio, 0, 1) * DAY_SECONDS);
    }

    function viewportClientXToSeconds(clientX, viewport, zoom) {
        const rect = viewport.getBoundingClientRect();
        const x = clientX - rect.left + viewport.scrollLeft;
        const ratio = (x - axisPadding(zoom)) / zoom.trackWidth;
        return Math.round(clamp(ratio, 0, 1) * DAY_SECONDS);
    }

    function viewportCenterSeconds(viewport, zoom) {
        const rect = viewport.getBoundingClientRect();
        return viewportClientXToSeconds(rect.left + rect.width / 2, viewport, zoom);
    }

    function centerViewportOnSeconds(viewport, seconds, zoom) {
        const maxScrollLeft = Math.max(0, viewport.scrollWidth - viewport.clientWidth);
        const targetLeft = secondsToX(seconds, zoom) - viewport.clientWidth / 2;
        viewport.scrollLeft = clamp(targetLeft, 0, maxScrollLeft);
    }

    function viewportClientXToRatio(clientX, viewport) {
        const rect = viewport.getBoundingClientRect();
        return rect.width > 0 ? clamp((clientX - rect.left) / rect.width, 0, 1) : 0.5;
    }

    function secondsToX(seconds, zoom) {
        return axisPadding(zoom) + (clampSeconds(seconds) / DAY_SECONDS) * zoom.trackWidth;
    }

    function axisPadding(zoom) {
        return Number.isFinite(zoom && zoom.sidePadding) ? zoom.sidePadding : SIDE_PADDING;
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

    function rubberbandPanOffset(offset, viewportWidth) {
        if (!Number.isFinite(offset) || offset === 0) return 0;
        const limit = Math.min(MAX_ELASTIC_PAN_PX, Math.max(20, viewportWidth * 0.06));
        const absOffset = Math.abs(offset);
        const resisted = limit * (1 - Math.exp(-absOffset / (limit * 2.8)));
        return Math.sign(offset) * Math.min(limit, resisted);
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

    function markerSourceKey(source) {
        source = String(source || '').toLowerCase();
        if (source.includes('onvif')) return 'onvif';
        return 'frame-diff';
    }

    function markerLabel(marker) {
        const label = markerSourceLabel(marker && marker.source);
        const score = Number(marker && marker.score);
        const scoreText = Number.isFinite(score) && score > 0 && markerSourceKey(marker && marker.source) === 'frame-diff'
            ? ` · ${(score * 100).toFixed(2)}%`
            : '';
        return `动检标记 · ${label}${scoreText}`;
    }

    function markerSourceLabel(source) {
        switch (String(source || '').trim()) {
            case 'onvif':
                return 'ONVIF';
            case 'auto_onvif':
                return '自动/ONVIF';
            case 'auto_frame_diff':
                return '自动/帧差';
            case 'frame_diff':
                return '帧差';
            default:
                return '未知来源';
        }
    }

    function dateKeyToLocalTime(dateKey) {
        const parts = String(dateKey || '').split('-').map(Number);
        if (parts.length !== 3 || parts.some(part => !Number.isInteger(part))) return NaN;
        return new Date(parts[0], parts[1] - 1, parts[2], 0, 0, 0, 0).getTime();
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
