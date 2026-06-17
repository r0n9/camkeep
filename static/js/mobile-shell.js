(function () {
    const MOBILE_MAX_WIDTH = 900;
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
    let lastLayoutMode = '';

    function viewportWidth() {
        return window.visualViewport?.width || window.innerWidth || document.documentElement.clientWidth || 1024;
    }

    function getLayoutMode() {
        const width = viewportWidth();
        if (width <= MOBILE_MAX_WIDTH) return 'mobile';
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

    function getPtzPanel() {
        return document.getElementById('ptz-panel');
    }

    function getPtzMobileHost() {
        return document.getElementById('mobilePtzDock') || document.getElementById('playback-column');
    }

    function syncMobilePtzPlacement() {
        const panel = getPtzPanel();
        const host = getPtzMobileHost();
        const stage = document.getElementById('video-stage');
        if (!panel || !host || !stage) return;
        if (isMobileMode()) {
            host.hidden = false;
            host.setAttribute('aria-hidden', 'false');
            if (panel.parentElement !== host) host.appendChild(panel);
        } else {
            if (host.id === 'mobilePtzDock') {
                host.hidden = true;
                host.setAttribute('aria-hidden', 'true');
            }
            if (panel.parentElement !== stage) {
                stage.appendChild(panel);
            }
        }
    }

    function syncLayoutMode() {
        const root = document.documentElement;
        const mode = getLayoutMode();
        const modeChanged = mode !== lastLayoutMode;
        root.dataset.layoutMode = mode;
        root.classList.toggle('is-touch', hasCoarsePointer());
        root.classList.toggle('is-standalone-pwa', isStandalonePWA());
        root.dataset.mobilePage = currentPage();
        syncMobilePtzPlacement();
        syncTabUI();
        syncThemeControls();
        if (modeChanged && typeof window.refreshPTZPanel === 'function') {
            window.refreshPTZPanel(true);
        }
        lastLayoutMode = mode;
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

    function closeTopMostOverlay(options = {}) {
        if (closeSheet({fromHistory: options.fromHistory || options.skipHistory})) return true;
        return false;
    }

    function formatDateKey(date) {
        const year = date.getFullYear();
        const month = String(date.getMonth() + 1).padStart(2, '0');
        const day = String(date.getDate()).padStart(2, '0');
        return `${year}-${month}-${day}`;
    }

    function syncPageState() {
        document.documentElement.dataset.mobilePage = currentPage();
        syncTabUI();
        syncThemeControls();
    }

    window.CamKeepMobile = {
        applyRecordQuickRange: window.applyRecordQuickRange,
        closeSheet,
        closeTopMostOverlay,
        getLayoutMode,
        isMobileMode,
        openCameraPicker: window.openCameraPicker,
        openRecordActionSheet: window.openRecordActionSheet,
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
