(function () {
    const MOBILE_MAX_WIDTH = 640;
    const COMPACT_MAX_WIDTH = 900;
    const TAB_TITLES = {
        monitor: {title: '监控', mode: 'LIVE'},
        devices: {title: '设备', mode: 'ENTRY'},
        records: {title: '录像', mode: 'RECORDS'},
        settings: {title: '设置', mode: 'SYSTEM'}
    };

    let resizeFrame = 0;
    let sheetOpen = false;
    let lastFocusedElement = null;
    let closingFromHistory = false;
    let cameraPickerFilter = 'all';

    function viewportWidth() {
        return window.visualViewport?.width || window.innerWidth || document.documentElement.clientWidth || 1024;
    }

    function getLayoutMode() {
        const width = viewportWidth();
        if (width <= COMPACT_MAX_WIDTH) return 'mobile';
        return 'desktop';
    }

    function hasCoarsePointer() {
        return window.matchMedia?.('(pointer: coarse)').matches === true;
    }

    function isStandalonePWA() {
        return window.matchMedia?.('(display-mode: standalone)').matches === true || window.navigator.standalone === true;
    }

    function normalizeTab(tab) {
        return Object.prototype.hasOwnProperty.call(TAB_TITLES, tab) ? tab : 'monitor';
    }

    function currentPage() {
        if (!document.getElementById('configPage')?.classList.contains('hidden')) return 'config';
        if (!document.getElementById('userPage')?.classList.contains('hidden')) return 'users';
        return 'dashboard';
    }

    function syncLayoutMode() {
        const root = document.documentElement;
        const mode = getLayoutMode();
        root.dataset.layoutMode = mode;
        root.classList.toggle('is-touch', hasCoarsePointer());
        root.classList.toggle('is-standalone-pwa', isStandalonePWA());
        root.dataset.mobilePage = currentPage();
        syncTabUI();
        syncThemeControls();
        window.dispatchEvent(new CustomEvent('camkeep:layoutchange', {
            detail: {mode, touch: hasCoarsePointer(), standalone: isStandalonePWA()}
        }));
    }

    function scheduleLayoutSync() {
        if (resizeFrame) cancelAnimationFrame(resizeFrame);
        resizeFrame = requestAnimationFrame(() => {
            resizeFrame = 0;
            syncLayoutMode();
        });
    }

    function syncThemeControls() {
        const skin = document.documentElement.dataset.skin === 'classic' ? 'classic' : 'neu';
        const savedMode = localStorage.getItem('camkeep-theme');
        const mode = savedMode === 'dark' || savedMode === 'light' ? savedMode : 'system';
        document.querySelectorAll('[data-mobile-skin]').forEach(button => {
            button.classList.toggle('is-active', button.dataset.mobileSkin === skin);
        });
        document.querySelectorAll('[data-mobile-mode]').forEach(button => {
            button.classList.toggle('is-active', button.dataset.mobileMode === mode);
        });
    }

    function syncTabUI() {
        const root = document.documentElement;
        const tab = normalizeTab(root.dataset.mobileTab);
        root.dataset.mobileTab = tab;
        const meta = TAB_TITLES[tab];
        const title = document.getElementById('mobileAppbarTabTitle');
        const mode = document.getElementById('mobileAppbarMode');
        if (title) title.textContent = meta.title;
        if (mode) mode.textContent = meta.mode;
        document.querySelectorAll('[data-mobile-tab-target]').forEach(button => {
            const active = button.dataset.mobileTabTarget === tab;
            button.classList.toggle('is-active', active);
            button.setAttribute('aria-current', active ? 'page' : 'false');
        });
        syncSubpageUI();
    }

    function syncSubpageUI() {
        const page = document.documentElement.dataset.mobilePage || 'dashboard';
        const userView = document.documentElement.dataset.mobileUserView || 'list';
        const userTitle = document.querySelector('.mobile-subpage-appbar--users .mobile-subpage-title span');
        const userSub = document.querySelector('.mobile-subpage-appbar--users .mobile-subpage-title small');
        const userAction = document.querySelector('.mobile-subpage-appbar--users .mobile-subpage-action');
        const userBack = document.querySelector('.mobile-subpage-appbar--users .mobile-subpage-back');
        if (page === 'users') {
            if (userTitle) userTitle.textContent = userView === 'detail' ? '用户详情' : '用户管理';
            if (userSub) userSub.textContent = userView === 'detail' ? '编辑账号、权限与密码' : '账号列表';
            if (userAction) userAction.textContent = '新增';
            if (userBack) userBack.setAttribute('aria-label', userView === 'detail' ? '返回账号列表' : '返回监控');
        }
    }

    function setTab(tab, options = {}) {
        const next = normalizeTab(tab);
        if (next !== 'monitor') {
            closeTopMostOverlay({skipHistory: true});
        }
        document.documentElement.dataset.mobileTab = next;
        try {
            localStorage.setItem('camkeep-mobile-tab', next);
        } catch (e) {
        }
        syncTabUI();
        if (!options.preserveScroll) {
            window.scrollTo({top: 0, behavior: options.instant ? 'auto' : 'smooth'});
        }
    }

    function isMobileMode() {
        return document.documentElement.dataset.layoutMode === 'mobile' || getLayoutMode() === 'mobile';
    }

    function setBodySheetLock(locked) {
        document.body.classList.toggle('mobile-sheet-lock', locked);
        document.documentElement.classList.toggle('mobile-sheet-lock', locked);
    }

    function getSheetRoot() {
        return document.getElementById('mobileSheetRoot');
    }

    function getSheetBody() {
        return document.getElementById('mobileSheetBody');
    }

    function setSheetHeader(title, subtitle = '') {
        const titleEl = document.getElementById('mobileSheetTitle');
        const subtitleEl = document.getElementById('mobileSheetSubtitle');
        if (titleEl) titleEl.textContent = title;
        if (subtitleEl) subtitleEl.textContent = subtitle;
    }

    function openSheet(title, subtitle, body) {
        if (!isMobileMode()) return false;
        const root = getSheetRoot();
        const bodyEl = getSheetBody();
        if (!root || !bodyEl) return false;
        lastFocusedElement = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        setSheetHeader(title, subtitle);
        if (typeof body === 'string') {
            bodyEl.innerHTML = body;
        } else {
            bodyEl.innerHTML = '';
            if (body) bodyEl.appendChild(body);
        }
        root.classList.remove('hidden');
        root.setAttribute('aria-hidden', 'false');
        setBodySheetLock(true);
        requestAnimationFrame(() => root.classList.add('is-open'));
        sheetOpen = true;
        if (!history.state?.camkeepMobileOverlay) {
            history.pushState({...(history.state || {}), camkeepMobileOverlay: 'sheet'}, '', location.href);
        }
        return true;
    }

    function closeSheet(options = {}) {
        const root = getSheetRoot();
        const bodyEl = getSheetBody();
        if (!root || !sheetOpen) return false;
        root.classList.remove('is-open');
        root.setAttribute('aria-hidden', 'true');
        setBodySheetLock(false);
        window.setTimeout(() => {
            if (!root.classList.contains('is-open')) {
                root.classList.add('hidden');
                if (bodyEl) bodyEl.innerHTML = '';
            }
        }, 220);
        sheetOpen = false;
        if (!options.fromHistory && history.state?.camkeepMobileOverlay === 'sheet') {
            closingFromHistory = true;
            history.back();
            window.setTimeout(() => {
                closingFromHistory = false;
            }, 0);
        }
        if (!options.skipFocus && lastFocusedElement?.isConnected) {
            lastFocusedElement.focus({preventScroll: true});
        }
        lastFocusedElement = null;
        return true;
    }

    function escapeHtmlValue(value) {
        return String(value == null ? '' : value).replace(/[&<>"']/g, char => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#39;'
        })[char]);
    }

    function getCameraEntries() {
        if (typeof window.getMobileCameraEntries === 'function') return window.getMobileCameraEntries();
        if (!Array.isArray(window.latestCameraStatusEntries)) return [];
        return window.latestCameraStatusEntries;
    }

    function cameraOnlineState(cam) {
        const running = cam?.is_running === true || cam?.running === true;
        const streamState = String(cam?.stream_state || cam?.state || '').toLowerCase();
        const recordState = String(cam?.record_state || '').toLowerCase();
        if (running || streamState === 'online' || streamState === 'running') return 'online';
        if (recordState === 'recording') return 'online';
        if (streamState === 'offline' || cam?.online === false) return 'offline';
        return 'idle';
    }

    function cameraMetaText(cam) {
        const parts = [];
        if (cam?.mode) parts.push(cam.mode);
        if (cam?.record_state) parts.push(cam.record_state);
        const state = cameraOnlineState(cam);
        parts.push(state === 'online' ? '在线' : (state === 'offline' ? '离线' : '待连接'));
        return parts.join(' · ');
    }

    function openCameraPicker() {
        const entries = getCameraEntries();
        const wrapper = document.createElement('div');
        wrapper.className = 'mobile-picker-wrap';
        const toolbar = document.createElement('div');
        toolbar.className = 'mobile-picker-toolbar';
        toolbar.innerHTML = `
            <button type="button" class="mobile-picker-filter is-active" data-filter="all">全部</button>
            <button type="button" class="mobile-picker-filter" data-filter="online">在线</button>
            <button type="button" class="mobile-picker-filter" data-filter="idle">待连接</button>
            <button type="button" class="mobile-picker-filter" data-filter="offline">离线</button>
        `;
        toolbar.querySelectorAll('[data-filter]').forEach(button => {
            button.addEventListener('click', () => {
                cameraPickerFilter = button.dataset.filter || 'all';
                renderCameraPickerList(wrapper, entries);
            });
        });
        wrapper.appendChild(toolbar);
        const list = document.createElement('div');
        list.className = 'mobile-picker-list';
        wrapper.appendChild(list);
        renderCameraPickerList(wrapper, entries);
        openSheet('选择设备', `${entries.length} 个实时节点`, wrapper);
    }

    function renderCameraPickerList(wrapper, entries) {
        const list = wrapper?.querySelector('.mobile-picker-list');
        const toolbar = wrapper?.querySelector('.mobile-picker-toolbar');
        if (!list || !toolbar) return;
        toolbar.querySelectorAll('[data-filter]').forEach(button => {
            button.classList.toggle('is-active', button.dataset.filter === cameraPickerFilter);
        });
        const filtered = entries.filter(({cam}) => cameraPickerFilter === 'all' || cameraOnlineState(cam) === cameraPickerFilter);
        list.innerHTML = '';
        if (!entries.length) {
            list.innerHTML = '<div class="mobile-sheet-empty">暂无可访问的摄像头</div>';
            return;
        }
        if (!filtered.length) {
            list.innerHTML = '<div class="mobile-sheet-empty">当前筛选条件下没有摄像头</div>';
            return;
        }

        filtered.forEach(({id, cam}) => {
            const state = cameraOnlineState(cam);
            const item = document.createElement('div');
            const selectedCam = typeof window.getCurrentSelectedCam === 'function' ? window.getCurrentSelectedCam() : window.currentSelectedCam;
            item.className = `mobile-picker-item ${selectedCam === id ? 'is-selected' : ''}`;
            item.innerHTML = `
                <span class="mobile-picker-dot is-${state === 'online' ? 'online' : state === 'offline' ? 'offline' : 'idle'}" aria-hidden="true">
                    <svg width="17" height="17" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 10l4.5-2.25A1 1 0 0121 8.64v6.72a1 1 0 01-1.5.87L15 14M5 6h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2z"></path>
                    </svg>
                </span>
                <span class="mobile-picker-main">
                    <strong class="mobile-picker-title">${escapeHtmlValue(id)}</strong>
                    <span class="mobile-picker-meta">${escapeHtmlValue(cameraMetaText(cam))}</span>
                </span>
                <span class="mobile-picker-actions">
                    <button type="button" class="mobile-picker-action mobile-picker-action--primary" data-preview-camera="${escapeHtmlValue(id)}">直播</button>
                    <button type="button" class="mobile-picker-action" data-select-camera="${escapeHtmlValue(id)}">录像</button>
                </span>
            `;
            item.querySelector('[data-select-camera]')?.addEventListener('click', event => {
                event.stopPropagation();
                if (typeof window.openCameraRecordsFromNode === 'function') {
                    window.openCameraRecordsFromNode(id);
                } else {
                    window.selectCamera?.(id);
                    setTab('records', {instant: true});
                }
                closeSheet();
            });
            item.querySelector('[data-preview-camera]')?.addEventListener('click', event => {
                event.stopPropagation();
                if (typeof window.openCameraLiveFromNode === 'function') {
                    window.openCameraLiveFromNode(id);
                } else {
                    window.previewLive?.(id);
                    setTab('monitor', {instant: true});
                }
                closeSheet();
            });
            item.addEventListener('click', () => {
                if (typeof window.openCameraLiveFromNode === 'function') {
                    window.openCameraLiveFromNode(id);
                } else {
                    window.previewLive?.(id);
                    setTab('monitor', {instant: true});
                }
                closeSheet();
            });
            list.appendChild(item);
        });
    }

    function openPTZSheet() {
        if (!isMobileMode()) return false;
        const camId = window.getActiveLiveCamId?.() || '';
        if (!camId) {
            openSheet('云台控制', '请先播放一个单画面直播', '<div class="mobile-sheet-empty">当前没有可控制的单画面直播。</div>');
            return false;
        }
        closeSheet({skipFocus: true});
        setTab('monitor', {preserveScroll: true});
        document.getElementById('video-stage')?.classList.add('mobile-ptz-sheet-host');
        window.PTZ?.refreshPanel?.({force: true, expanded: true});
        if (!history.state?.camkeepMobileOverlay) {
            history.pushState({...(history.state || {}), camkeepMobileOverlay: 'ptz'}, '', location.href);
        }
        return true;
    }

    function closeTopMostOverlay(options = {}) {
        if (closeSheet({fromHistory: options.fromHistory || options.skipHistory})) return true;
        const stage = document.getElementById('video-stage');
        if (stage?.classList.contains('mobile-ptz-sheet-host')) {
            stage.classList.remove('mobile-ptz-sheet-host');
            window.PTZ?.hidePanel?.();
            if (!options.fromHistory && !options.skipHistory && history.state?.camkeepMobileOverlay === 'ptz') {
                closingFromHistory = true;
                history.back();
                window.setTimeout(() => {
                    closingFromHistory = false;
                }, 0);
            }
            return true;
        }
        return false;
    }

    function formatDateKey(date) {
        const year = date.getFullYear();
        const month = String(date.getMonth() + 1).padStart(2, '0');
        const day = String(date.getDate()).padStart(2, '0');
        return `${year}-${month}-${day}`;
    }

    function applyRecordQuickRange(kind) {
        const startInput = document.getElementById('recordStartDate');
        const endInput = document.getElementById('recordEndDate');
        if (!startInput || !endInput) return;
        const today = new Date();
        const start = new Date(today);
        if (kind === 'yesterday') {
            start.setDate(today.getDate() - 1);
            today.setDate(today.getDate() - 1);
        } else if (kind === '3d') {
            start.setDate(today.getDate() - 2);
        } else if (kind === '7d') {
            start.setDate(today.getDate() - 6);
        }
        startInput.value = formatDateKey(start);
        endInput.value = formatDateKey(today);
        document.querySelectorAll('.mobile-record-range-quick button').forEach(button => {
            button.classList.toggle('is-active', button.getAttribute('onclick')?.includes(`'${kind}'`) === true);
        });
        window.applyRecordRange?.();
    }

    function openRecordActionSheet(recordPath, camId = (typeof window.getCurrentSelectedCam === 'function' ? window.getCurrentSelectedCam() : window.currentSelectedCam) || '') {
        recordPath = String(recordPath || '').trim();
        if (!recordPath) return false;
        const fileName = recordPath.split('/').pop() || 'record';
        const canDelete = typeof window.canAdmin === 'function' ? window.canAdmin() : false;
        const wrapper = document.createElement('div');
        wrapper.className = 'mobile-sheet-actions';
        wrapper.innerHTML = `
            <button type="button" class="mobile-sheet-action" data-action="play">
                <span class="mobile-sheet-action-icon"><svg width="17" height="17" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 5v14l11-7-11-7z"></path></svg></span>
                <span class="mobile-sheet-action-copy"><strong>播放录像</strong><span>${escapeHtmlValue(fileName)}</span></span>
            </button>
            <button type="button" class="mobile-sheet-action" data-action="download">
                <span class="mobile-sheet-action-icon"><svg width="17" height="17" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1M8 12l4 4m0 0l4-4m-4 4V4"></path></svg></span>
                <span class="mobile-sheet-action-copy"><strong>下载录像</strong><span>保存原始录像文件</span></span>
            </button>
            ${canDelete ? `<button type="button" class="mobile-sheet-action mobile-sheet-action--danger" data-action="delete">
                <span class="mobile-sheet-action-icon"><svg width="17" height="17" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg></span>
                <span class="mobile-sheet-action-copy"><strong>删除录像</strong><span>永久删除，释放存储空间</span></span>
            </button>` : ''}
        `;
        wrapper.querySelector('[data-action="play"]')?.addEventListener('click', () => {
            closeSheet();
            window.playRecordByPath?.(recordPath);
        });
        wrapper.querySelector('[data-action="download"]')?.addEventListener('click', () => {
            closeSheet();
            window.downloadRecord?.({stopPropagation() {}}, recordPath);
        });
        wrapper.querySelector('[data-action="delete"]')?.addEventListener('click', () => {
            closeSheet();
            window.deleteRecord?.({stopPropagation() {}}, camId, recordPath);
        });
        return openSheet('录像操作', fileName, wrapper);
    }

    function syncPageState() {
        document.documentElement.dataset.mobilePage = currentPage();
        syncTabUI();
        syncThemeControls();
    }

    window.CamKeepMobile = {
        applyRecordQuickRange,
        closeSheet,
        closeTopMostOverlay,
        getLayoutMode,
        isMobileMode,
        openCameraPicker,
        openPTZSheet,
        openRecordActionSheet,
        openSheet,
        syncLayoutMode,
        syncPageState,
        syncThemeControls,
        setTab
    };

    window.addEventListener('resize', scheduleLayoutSync);
    window.addEventListener('orientationchange', scheduleLayoutSync);
    window.visualViewport?.addEventListener('resize', scheduleLayoutSync);
    window.addEventListener('camkeep:pagechange', syncPageState);
    window.addEventListener('popstate', event => {
        if (closingFromHistory) return;
        if (closeTopMostOverlay({fromHistory: true})) {
            event.preventDefault?.();
            return;
        }
        if (document.documentElement.dataset.mobileTab !== 'monitor' && isMobileMode()) {
            setTab('monitor', {instant: true});
        }
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') closeTopMostOverlay();
    });

    document.addEventListener('DOMContentLoaded', () => {
        const saved = normalizeTab(document.documentElement.dataset.mobileTab || localStorage.getItem('camkeep-mobile-tab') || 'devices');
        document.documentElement.dataset.mobileTab = saved;
        syncLayoutMode();
    });

    if (document.readyState !== 'loading') {
        syncLayoutMode();
    }
})();
