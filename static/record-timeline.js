(function () {
    const windowStarts = new Map();
    const transitionDirections = new Map();
    const windowSize = 10;
    const minTrackWidth = 920;
    const maxTrackWidth = 1480;
    const tickTargetWidth = 96;
    const sidePadding = 112;
    const cardWidth = 192;
    const cardHeight = 54;
    const laneHeight = 68;
    const cardAreaHeight = 420;
    const dialHeight = 76;
    const cardGap = 12;
    const dragStartThreshold = 6;
    const wheelPageThreshold = 180;
    const wheelPageMinDuration = 300;
    const wheelPageMinSamples = 4;
    const wheelPageCooldown = 700;
    const wheelIntentResetDelay = 280;
    let lastWheelPageAt = 0;

    function create(options) {
        const {
            camId,
            date,
            entries,
            selectedRecordPath = '',
            renderItem,
            onUpdate = () => {}
        } = options;

        const wrapper = document.createElement('div');
        wrapper.className = 'space-y-2';

        const knownEntries = entries
            .filter(entry => entry.meta.hasStartTime)
            .sort((a, b) => a.meta.startSeconds - b.meta.startSeconds || a.meta.sortKey.localeCompare(b.meta.sortKey));
        const unknownEntries = entries.filter(entry => !entry.meta.hasStartTime);

        if (knownEntries.length > 0) {
            const timelineKey = groupKey(camId, date);
            const transitionDirection = transitionDirections.get(timelineKey) || '';
            transitionDirections.delete(timelineKey);

            const windowStart = getWindowStart(camId, date, knownEntries.length);
            const windowEntries = knownEntries.slice(windowStart, windowStart + windowSize);
            const scale = buildScale(windowEntries);

            wrapper.appendChild(createTimelineStage(windowEntries, selectedRecordPath, renderItem, scale, {
                dayEntries: knownEntries,
                transitionDirection,
                canPrev: windowStart > 0,
                canNext: windowStart + windowSize < knownEntries.length,
                onPage: direction => navigateWindow(camId, date, knownEntries.length, windowStart, direction, onUpdate)
            }));
        } else {
            const empty = document.createElement('div');
            empty.className = 'rounded-lg border border-dashed border-slate-200 bg-white px-4 py-8 text-center text-sm font-bold text-slate-400';
            empty.textContent = '该日录像缺少可识别开始时间';
            wrapper.appendChild(empty);
        }

        if (unknownEntries.length > 0) {
            wrapper.appendChild(createUnknownList(unknownEntries, renderItem));
        }

        return wrapper;
    }

    function groupKey(camId, date) {
        return `${camId}:${date}`;
    }

    function maxWindowStart(total) {
        return Math.max(0, Math.floor((total - 1) / windowSize) * windowSize);
    }

    function getWindowStart(camId, date, total) {
        const savedStart = windowStarts.get(groupKey(camId, date)) || 0;
        return Math.min(Math.max(0, savedStart), maxWindowStart(total));
    }

    function setWindowStart(camId, date, start, total) {
        const nextStart = Math.min(Math.max(0, start), maxWindowStart(total));
        windowStarts.set(groupKey(camId, date), nextStart);
        return nextStart;
    }

    function navigateWindow(camId, date, total, windowStart, direction, onUpdate) {
        const step = direction === 'next' ? windowSize : -windowSize;
        const nextStart = setWindowStart(camId, date, windowStart + step, total);
        if (nextStart === windowStart) return false;

        transitionDirections.set(groupKey(camId, date), direction);
        requestAnimationFrame(onUpdate);
        return true;
    }

    function createTimelineStage(entries, selectedRecordPath, renderItem, scale, paging) {
        const frame = document.createElement('div');
        frame.className = 'record-timeline-frame';
        if (paging.transitionDirection) {
            frame.classList.add(`is-window-enter-${paging.transitionDirection}`);
        }

        const scroll = document.createElement('div');
        scroll.className = 'record-timeline-scroll custom-scrollbar';
        enableDrag(scroll, paging, frame);

        const layout = layoutEntries(entries, scale);
        const laneCount = Math.max(1, layout.reduce((max, item) => Math.max(max, item.lane + 1), 1));
        const lanesHeight = laneCount * laneHeight;
        const stageHeight = Math.max(cardAreaHeight, lanesHeight);
        const stageWidth = scale.trackWidth + sidePadding * 2;

        const stage = document.createElement('div');
        stage.className = 'record-timeline-stage';
        stage.style.width = `${stageWidth}px`;
        stage.style.height = `${stageHeight}px`;

        layout.forEach(item => {
            const top = item.lane * laneHeight + 4;
            const pinTop = top + cardHeight - 2;
            const pinHeight = Math.max(16, stageHeight - pinTop + 8);
            const path = entryPath(item.entry);

            const pin = document.createElement('div');
            pin.className = 'record-timeline-pin';
            pin.dataset.recordAxisPath = path;
            pin.classList.toggle('is-selected', selectedRecordPath !== '' && path === selectedRecordPath);
            pin.style.left = `${item.anchorX}px`;
            pin.style.top = `${pinTop}px`;
            pin.style.height = `${pinHeight}px`;
            stage.appendChild(pin);

            const entryWrap = document.createElement('div');
            entryWrap.className = 'record-timeline-entry';
            entryWrap.style.left = `${item.left}px`;
            entryWrap.style.top = `${top}px`;
            entryWrap.appendChild(renderItem(item.entry, {timeline: true}));
            entryWrap.addEventListener('pointerenter', () => pin.classList.add('is-hovered'));
            entryWrap.addEventListener('pointerleave', () => pin.classList.remove('is-hovered'));
            entryWrap.addEventListener('focusin', () => pin.classList.add('is-hovered'));
            entryWrap.addEventListener('focusout', () => pin.classList.remove('is-hovered'));
            stage.appendChild(entryWrap);
        });

        scroll.appendChild(stage);

        const dialWrap = document.createElement('div');
        dialWrap.className = 'record-timeline-dial-fixed';
        const dial = createDial(scale);
        dial.style.width = `${stageWidth}px`;
        dialWrap.appendChild(dial);

        const syncDial = () => {
            dial.style.transform = `translateX(${-scroll.scrollLeft}px)`;
        };
        scroll.addEventListener('scroll', syncDial, {passive: true});

        const selectedEntry = entries.find(entry => entryPath(entry) === selectedRecordPath);
        const preferredEntry = selectedEntry || entries[0];
        const preferredLeft = sidePadding + ((preferredEntry.meta.startSeconds - scale.startSeconds) / scale.spanSeconds) * scale.trackWidth;
        requestAnimationFrame(() => {
            if (!scroll.isConnected) return;
            scroll.scrollLeft = Math.max(0, preferredLeft - Math.min(360, scroll.clientWidth * 0.35));
            syncDial();
        });

        frame.appendChild(createDayPositionBadge(entries[0].meta.startSeconds, paging.dayEntries || entries));
        frame.appendChild(createSideNavButton('prev', paging.canPrev, () => paging.onPage('prev')));
        frame.appendChild(scroll);
        frame.appendChild(createSideNavButton('next', paging.canNext, () => paging.onPage('next')));
        frame.appendChild(dialWrap);
        return frame;
    }

    function createUnknownList(entries, renderItem) {
        const fallback = document.createElement('div');
        fallback.className = 'rounded-lg border border-slate-200 bg-white p-2';

        const title = document.createElement('div');
        title.className = 'mb-2 px-1 text-[11px] font-bold text-slate-400';
        title.textContent = '未识别开始时间';

        const grid = document.createElement('div');
        grid.className = 'grid grid-cols-1 gap-1.5 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4';
        entries.forEach(entry => {
            grid.appendChild(renderItem(entry, {timeline: false}));
        });

        fallback.appendChild(title);
        fallback.appendChild(grid);
        return fallback;
    }

    function createSideNavButton(direction, enabled, onClick) {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = `record-timeline-side-nav record-timeline-side-nav-${direction}`;
        btn.disabled = !enabled;
        btn.title = enabled
            ? direction === 'prev' ? '查看更早的录像' : '查看更晚的录像'
            : direction === 'prev' ? '已经到最早的录像' : '已经到最晚的录像';
        btn.innerHTML = `
            ${direction === 'prev'
                ? `<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M15 19l-7-7 7-7"></path></svg>`
                : ''}
            <span>${enabled ? direction === 'prev' ? '更早录像' : '更晚录像' : direction === 'prev' ? '已到最早' : '已到最晚'}</span>
            ${direction === 'next'
                ? `<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M9 5l7 7-7 7"></path></svg>`
                : ''}
        `;
        btn.onclick = () => {
            if (enabled) onClick();
        };
        return btn;
    }

    function createDayPositionBadge(startSeconds, dayEntries) {
        const percent = Math.min(100, Math.max(0, (startSeconds / 86400) * 100));
        const heatmap = buildDayHeatmap(dayEntries);
        const badge = document.createElement('div');
        badge.className = 'record-timeline-day-position';
        badge.title = `当前窗口从 ${formatSecondsClock(startSeconds)} 开始，约位于当天 ${Math.round(percent)}%。颜色表示全天录像分布密度`;
        badge.innerHTML = `
            <div class="record-timeline-day-position-head">
                <span>当日起点</span>
                <strong>${formatSecondsClock(startSeconds)}</strong>
            </div>
            <div class="record-timeline-day-position-track">
                <span class="record-timeline-day-position-heat" aria-hidden="true">
                    ${heatmap.segments.map(segment => `
                        <span class="record-timeline-day-position-segment is-level-${segment.level}" title="${segment.title}"></span>
                    `).join('')}
                </span>
                <span class="record-timeline-day-position-dot" style="left: ${percent}%"></span>
            </div>
            <div class="record-timeline-day-position-foot">
                <span>00:00</span>
                <span>24:00</span>
            </div>
        `;
        return badge;
    }

    function buildDayHeatmap(entries) {
        const binCount = 48;
        const secondsPerBin = 86400 / binCount;
        const counts = Array.from({length: binCount}, () => 0);
        const sourceEntries = Array.isArray(entries) ? entries : [];
        sourceEntries.forEach(entry => {
            const seconds = entry && entry.meta ? entry.meta.startSeconds : null;
            if (!Number.isFinite(seconds)) return;
            const bin = Math.min(binCount - 1, Math.max(0, Math.floor(seconds / secondsPerBin)));
            counts[bin] += 1;
        });

        const maxCount = Math.max(...counts, 0);
        const segments = counts.map((count, index) => {
            const start = Math.round(index * secondsPerBin);
            const end = Math.round((index + 1) * secondsPerBin);
            return {
                level: heatmapLevel(count, maxCount),
                title: `${formatSecondsClock(start)}-${formatSecondsClock(end)} · ${count} 个录像`
            };
        });
        return {segments};
    }

    function heatmapLevel(count, maxCount) {
        if (count <= 0 || maxCount <= 0) return 0;
        if (maxCount === 1) return 1;
        const ratio = count / maxCount;
        if (ratio <= 0.34) return 1;
        if (ratio <= 0.67) return 2;
        if (ratio < 1) return 3;
        return 4;
    }

    function buildScale(entries) {
        const starts = entries.map(entry => entry.meta.startSeconds).sort((a, b) => a - b);
        const first = starts[0] || 0;
        const last = starts[starts.length - 1] || first;
        const rawSpan = Math.max(0, last - first);
        const minSpan = 15 * 60;
        const padding = rawSpan === 0 ? 5 * 60 : Math.min(15 * 60, Math.max(3 * 60, Math.round(rawSpan * 0.12)));
        let startSeconds = Math.max(0, first - padding);
        let endSeconds = Math.min(86400, last + padding);

        if (endSeconds - startSeconds < minSpan) {
            const center = Math.round((first + last) / 2);
            startSeconds = Math.max(0, center - minSpan / 2);
            endSeconds = Math.min(86400, startSeconds + minSpan);
            startSeconds = Math.max(0, endSeconds - minSpan);
        }

        const tickIntervalSeconds = chooseTickInterval(endSeconds - startSeconds);
        startSeconds = Math.max(0, Math.floor(startSeconds / tickIntervalSeconds) * tickIntervalSeconds);
        endSeconds = Math.min(86400, Math.ceil(endSeconds / tickIntervalSeconds) * tickIntervalSeconds);
        const spanSeconds = Math.max(tickIntervalSeconds, endSeconds - startSeconds);
        const tickCount = Math.max(1, Math.ceil(spanSeconds / tickIntervalSeconds));
        const trackWidth = Math.min(maxTrackWidth, Math.max(minTrackWidth, tickCount * tickTargetWidth));

        return {startSeconds, endSeconds, spanSeconds, tickIntervalSeconds, trackWidth};
    }

    function chooseTickInterval(spanSeconds) {
        const targetMaxTicks = 12;
        const intervals = [
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
        return intervals.find(interval => Math.ceil(spanSeconds / interval) <= targetMaxTicks) || intervals[intervals.length - 1];
    }

    function layoutEntries(entries, scale) {
        const laneRightEdges = [];
        return entries.map(entry => {
            const offsetRatio = (entry.meta.startSeconds - scale.startSeconds) / scale.spanSeconds;
            const anchorX = sidePadding + Math.min(1, Math.max(0, offsetRatio)) * scale.trackWidth;
            const left = anchorX - cardWidth / 2;
            const right = left + cardWidth + cardGap;
            let lane = laneRightEdges.findIndex(edge => left >= edge);
            if (lane === -1) {
                lane = laneRightEdges.length;
                laneRightEdges.push(right);
            } else {
                laneRightEdges[lane] = right;
            }
            return {entry, anchorX, left, lane};
        });
    }

    function createDial(scale) {
        const dial = document.createElement('div');
        dial.className = 'record-timeline-dial';
        dial.style.height = `${dialHeight}px`;

        const axis = document.createElement('div');
        axis.className = 'record-timeline-axis';
        axis.style.left = `${sidePadding}px`;
        axis.style.width = `${scale.trackWidth}px`;
        dial.appendChild(axis);

        for (let second = scale.startSeconds; second <= scale.endSeconds; second += scale.tickIntervalSeconds) {
            const x = sidePadding + ((second - scale.startSeconds) / scale.spanSeconds) * scale.trackWidth;
            const isStrong = second % 3600 === 0 || scale.tickIntervalSeconds >= 3600;
            const isMedium = !isStrong && (second % 1800 === 0 || scale.tickIntervalSeconds >= 900);

            const tick = document.createElement('div');
            tick.className = `record-timeline-tick ${isStrong ? 'record-timeline-tick-hour' : isMedium ? 'record-timeline-tick-half' : 'record-timeline-tick-minor'}`;
            tick.style.left = `${x}px`;
            dial.appendChild(tick);

            const label = document.createElement('div');
            label.className = 'record-timeline-label';
            label.style.left = `${x}px`;
            label.textContent = formatSecondsClock(second);
            dial.appendChild(label);
        }

        return dial;
    }

    function formatSecondsClock(seconds) {
        const normalized = Math.min(86400, Math.max(0, Math.round(seconds)));
        if (normalized >= 86400) return '24:00';
        const hour = Math.floor(normalized / 3600);
        const minute = Math.floor((normalized % 3600) / 60);
        return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
    }

    function normalizeWheelDelta(event, value, pageSize) {
        if (event.deltaMode === 1) return value * 16;
        if (event.deltaMode === 2) return value * pageSize;
        return value;
    }

    function enableDrag(scroll, paging, frame) {
        let drag = null;
        let suppressClickAfterDrag = false;
        let suppressClickTimer = null;
        let edgeFeedbackTimer = null;
        let wheelEdgeIntent = null;
        let wheelResetTimer = null;

        const clearEdgeFeedback = () => {
            frame.classList.remove('is-edge-left', 'is-edge-right');
            if (edgeFeedbackTimer) {
                clearTimeout(edgeFeedbackTimer);
                edgeFeedbackTimer = null;
            }
        };

        const setEdgeFeedback = direction => {
            frame.classList.toggle('is-edge-left', direction === 'prev');
            frame.classList.toggle('is-edge-right', direction === 'next');
            if (edgeFeedbackTimer) clearTimeout(edgeFeedbackTimer);
            edgeFeedbackTimer = setTimeout(clearEdgeFeedback, 360);
        };

        const clearSwipeIntentFeedback = () => {
            frame.classList.remove('is-swipe-intent-prev', 'is-swipe-intent-next');
            frame.style.removeProperty('--record-timeline-swipe-progress');
        };

        const setSwipeIntentFeedback = (direction, progress) => {
            const normalizedProgress = Math.min(1, Math.max(0.08, progress));
            frame.classList.toggle('is-swipe-intent-prev', direction === 'prev');
            frame.classList.toggle('is-swipe-intent-next', direction === 'next');
            frame.style.setProperty('--record-timeline-swipe-progress', normalizedProgress.toFixed(3));
        };

        const resetWheelEdgeIntent = () => {
            wheelEdgeIntent = null;
            clearSwipeIntentFeedback();
            if (wheelResetTimer) {
                clearTimeout(wheelResetTimer);
                wheelResetTimer = null;
            }
        };

        const trackWheelEdgeIntent = (direction, delta) => {
            const now = performance.now();
            if (now - lastWheelPageAt < wheelPageCooldown) {
                resetWheelEdgeIntent();
                return false;
            }

            if (!wheelEdgeIntent || wheelEdgeIntent.direction !== direction) {
                wheelEdgeIntent = {
                    direction,
                    startedAt: now,
                    distance: 0,
                    samples: 0
                };
            }

            wheelEdgeIntent.distance += Math.abs(delta);
            wheelEdgeIntent.samples += 1;

            if (wheelResetTimer) clearTimeout(wheelResetTimer);
            wheelResetTimer = setTimeout(resetWheelEdgeIntent, wheelIntentResetDelay);

            const durationProgress = (now - wheelEdgeIntent.startedAt) / wheelPageMinDuration;
            const distanceProgress = wheelEdgeIntent.distance / wheelPageThreshold;
            const sampleProgress = wheelEdgeIntent.samples / wheelPageMinSamples;
            setSwipeIntentFeedback(direction, Math.min(durationProgress, distanceProgress, sampleProgress));

            return wheelEdgeIntent.distance >= wheelPageThreshold
                && now - wheelEdgeIntent.startedAt >= wheelPageMinDuration
                && wheelEdgeIntent.samples >= wheelPageMinSamples;
        };

        const suppressNextClick = () => {
            suppressClickAfterDrag = true;
            if (suppressClickTimer) clearTimeout(suppressClickTimer);
            suppressClickTimer = setTimeout(() => {
                suppressClickAfterDrag = false;
                suppressClickTimer = null;
            }, 160);
        };

        scroll.addEventListener('pointerdown', event => {
            if (event.button !== 0 || event.target.closest('[data-record-action]')) return;
            clearEdgeFeedback();
            drag = {
                pointerId: event.pointerId,
                startX: event.clientX,
                startScrollLeft: scroll.scrollLeft,
                active: false
            };
        });

        scroll.addEventListener('pointermove', event => {
            if (!drag || drag.pointerId !== event.pointerId) return;
            const delta = event.clientX - drag.startX;
            if (!drag.active) {
                if (Math.abs(delta) < dragStartThreshold) return;
                drag.active = true;
                suppressClickAfterDrag = true;
                if (suppressClickTimer) {
                    clearTimeout(suppressClickTimer);
                    suppressClickTimer = null;
                }
                scroll.classList.add('is-dragging');
                scroll.setPointerCapture(event.pointerId);
            }

            const maxScrollLeft = Math.max(0, scroll.scrollWidth - scroll.clientWidth);
            const desiredScrollLeft = drag.startScrollLeft - delta;
            const clampedScrollLeft = Math.min(maxScrollLeft, Math.max(0, desiredScrollLeft));
            scroll.scrollLeft = clampedScrollLeft;

            if (Math.abs(delta) > 3) event.preventDefault();
        });

        const endDrag = event => {
            if (!drag || drag.pointerId !== event.pointerId) return;
            if (drag.active) suppressNextClick();
            if (drag.active && scroll.hasPointerCapture(event.pointerId)) {
                scroll.releasePointerCapture(event.pointerId);
            }
            scroll.classList.remove('is-dragging');
            clearEdgeFeedback();
            drag = null;
        };
        scroll.addEventListener('pointerup', endDrag);
        scroll.addEventListener('pointercancel', endDrag);
        scroll.addEventListener('click', event => {
            if (!suppressClickAfterDrag) return;
            suppressClickAfterDrag = false;
            if (suppressClickTimer) {
                clearTimeout(suppressClickTimer);
                suppressClickTimer = null;
            }
            event.preventDefault();
            event.stopPropagation();
        }, true);

        scroll.addEventListener('wheel', event => {
            if (event.target.closest('[data-record-action]')) return;

            const horizontalDelta = Math.abs(event.deltaX) >= Math.abs(event.deltaY)
                ? normalizeWheelDelta(event, event.deltaX, scroll.clientWidth)
                : event.shiftKey
                    ? normalizeWheelDelta(event, event.deltaY, scroll.clientWidth)
                    : 0;
            if (horizontalDelta === 0) {
                resetWheelEdgeIntent();
                return;
            }

            const maxScrollLeft = Math.max(0, scroll.scrollWidth - scroll.clientWidth);
            const atStart = scroll.scrollLeft <= 1;
            const atEnd = scroll.scrollLeft >= maxScrollLeft - 1;

            if (horizontalDelta < 0 && atStart) {
                event.preventDefault();
                if (paging.canPrev && trackWheelEdgeIntent('prev', horizontalDelta)) {
                    if (paging.onPage('prev')) lastWheelPageAt = performance.now();
                    resetWheelEdgeIntent();
                } else if (!paging.canPrev) {
                    setEdgeFeedback('prev');
                    resetWheelEdgeIntent();
                }
                return;
            }

            if (horizontalDelta > 0 && atEnd) {
                event.preventDefault();
                if (paging.canNext && trackWheelEdgeIntent('next', horizontalDelta)) {
                    if (paging.onPage('next')) lastWheelPageAt = performance.now();
                    resetWheelEdgeIntent();
                } else if (!paging.canNext) {
                    setEdgeFeedback('next');
                    resetWheelEdgeIntent();
                }
                return;
            }

            resetWheelEdgeIntent();
        }, {passive: false});
    }

    function entryPath(entry) {
        const file = entry && entry.file;
        return String((file && (file.path || file.url || file.name)) || '').trim();
    }

    window.RecordTimeline = {create};
})();
