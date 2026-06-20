(function () {
    const MOBILE_MAX_WIDTH = 900;
    const MOBILE_TABS = new Set(['monitor', 'devices', 'records', 'settings']);

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
        return MOBILE_TABS.has(tab) ? tab : 'monitor';
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
            if (host.id === 'mobilePtzDock') {
                host.classList.toggle('is-expanded', panel.classList.contains('is-expanded'));
                host.classList.toggle('is-collapsed', panel.classList.contains('is-collapsed'));
            }
        } else {
            if (host.id === 'mobilePtzDock') {
                host.hidden = true;
                host.setAttribute('aria-hidden', 'true');
                host.classList.remove('is-expanded', 'is-collapsed');
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
        const themeSummary = document.getElementById('mobileThemeSummary');
        if (themeSummary) {
            const skinLabel = skin === 'classic' ? '原始' : '拟态';
            const modeLabel = mode === 'dark' ? '深色' : mode === 'light' ? '浅色' : '跟随系统';
            themeSummary.textContent = `${skinLabel} · ${modeLabel}`;
        }
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
            if (userTitle) userTitle.textContent = userView === 'detail' ? '用户详情' : '用户列表';
            if (userSub) userSub.textContent = userView === 'detail' ? '编辑账号、权限与密码' : '选择用户查看详情';
            if (userAction) userAction.textContent = '新增';
            if (userBack) userBack.setAttribute('aria-label', userView === 'detail' ? '返回用户列表' : '返回设置');
        }
    }

    function setTab(tab, options = {}) {
        const next = normalizeTab(tab);
        const page = currentPage();
        if (page === 'config' && typeof window.confirmLeaveConfigIfDirty === 'function') {
            const blocked = window.confirmLeaveConfigIfDirty(() => {
                window.showDashboardPage?.();
                setTab(next, {...options, skipSubpageExit: true});
            });
            if (blocked) return;
        }
        if (!options.skipSubpageExit && page !== 'dashboard') {
            if (typeof window.showDashboardPage === 'function') {
                window.showDashboardPage();
            } else {
                document.getElementById('configPage')?.classList.add('hidden');
                document.getElementById('userPage')?.classList.add('hidden');
                document.getElementById('dashboardPage')?.classList.remove('hidden');
                document.documentElement.dataset.mobilePage = 'dashboard';
                document.documentElement.dataset.mobileSubpage = '';
                document.documentElement.dataset.mobileUserView = 'list';
            }
        }
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

    let lastYamlNavBypassAt = 0;

    function eventPoint(event) {
        const touch = event.changedTouches?.[0] || event.touches?.[0];
        if (touch) return {x: touch.clientX, y: touch.clientY};
        if (Number.isFinite(event.clientX) && Number.isFinite(event.clientY)) {
            return {x: event.clientX, y: event.clientY};
        }
        return null;
    }

    function pointInRect(point, rect, pad = 0) {
        return point.x >= rect.left - pad
            && point.x <= rect.right + pad
            && point.y >= rect.top - pad
            && point.y <= rect.bottom + pad;
    }

    function runAfterYamlTouch(action) {
        document.activeElement?.blur?.();
        window.setTimeout(action, 0);
    }

    function handleConfigYamlNavBypass(event) {
        const configPage = document.getElementById('configPage');
        if (!isMobileMode() || currentPage() !== 'config' || !configPage?.classList.contains('is-config-mode-yaml')) return;

        const now = Date.now();
        if (now - lastYamlNavBypassAt < 450) {
            event.preventDefault();
            event.stopImmediatePropagation?.();
            return;
        }

        const point = eventPoint(event);
        if (!point) return;

        const backButton = document.querySelector('.mobile-subpage-appbar--config .mobile-subpage-back');
        if (backButton && pointInRect(point, backButton.getBoundingClientRect(), 12)) {
            lastYamlNavBypassAt = now;
            event.preventDefault();
            event.stopImmediatePropagation?.();
            runAfterYamlTouch(() => window.closeConfig?.());
            return;
        }

        const tabbar = document.getElementById('mobileTabbar');
        if (!tabbar || !pointInRect(point, tabbar.getBoundingClientRect())) return;
        const targetButton = Array.from(tabbar.querySelectorAll('[data-mobile-tab-target]'))
            .find(button => pointInRect(point, button.getBoundingClientRect()));
        const targetTab = targetButton?.dataset.mobileTabTarget;
        if (!targetTab) return;

        lastYamlNavBypassAt = now;
        event.preventDefault();
        event.stopImmediatePropagation?.();
        runAfterYamlTouch(() => setTab(targetTab, {instant: true}));
    }

    function handleConfigYamlNavStart(event) {
        if (event.type === 'mousedown' && event.button !== 0) return;
        handleConfigYamlNavBypass(event);
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

    function openThemeSettings() {
        if (!isMobileMode()) return false;
        const skin = document.documentElement.dataset.skin === 'classic' ? 'classic' : 'neu';
        const savedMode = localStorage.getItem('camkeep-theme');
        const mode = savedMode === 'dark' || savedMode === 'light' ? savedMode : 'system';
        const body = document.createElement('div');
        body.className = 'mobile-theme-sheet';
        body.innerHTML = `
            <div class="mobile-theme-sheet-group">
                <div class="mobile-theme-sheet-head">
                    <strong>界面风格</strong>
                    <span>影响整体视觉层次</span>
                </div>
                <div class="mobile-settings-seg mobile-theme-sheet-seg" role="group" aria-label="界面风格">
                    <button type="button" data-mobile-skin="neu" class="${skin === 'neu' ? 'is-active' : ''}" onclick="setSkin('neu'); window.CamKeepMobile?.syncThemeControls?.()">拟态</button>
                    <button type="button" data-mobile-skin="classic" class="${skin === 'classic' ? 'is-active' : ''}" onclick="setSkin('classic'); window.CamKeepMobile?.syncThemeControls?.()">原始</button>
                </div>
            </div>
            <div class="mobile-theme-sheet-group">
                <div class="mobile-theme-sheet-head">
                    <strong>明暗模式</strong>
                    <span>支持跟随系统</span>
                </div>
                <div class="mobile-settings-seg mobile-theme-sheet-seg" role="group" aria-label="明暗模式">
                    <button type="button" data-mobile-mode="system" class="${mode === 'system' ? 'is-active' : ''}" onclick="setMode('system'); window.CamKeepMobile?.syncThemeControls?.()">系统</button>
                    <button type="button" data-mobile-mode="light" class="${mode === 'light' ? 'is-active' : ''}" onclick="setMode('light'); window.CamKeepMobile?.syncThemeControls?.()">浅色</button>
                    <button type="button" data-mobile-mode="dark" class="${mode === 'dark' ? 'is-active' : ''}" onclick="setMode('dark'); window.CamKeepMobile?.syncThemeControls?.()">深色</button>
                </div>
            </div>
            <div class="mobile-theme-sheet-footer">
                <button type="button" class="mobile-theme-sheet-close" onclick="window.CamKeepMobile?.closeSheet?.()">完成</button>
            </div>
        `;
        return openSheet('主题设置', '选择风格与明暗模式', body);
    }

    window.CamKeepMobile = {
        applyRecordQuickRange: window.applyRecordQuickRange,
        closeSheet,
        closeTopMostOverlay,
        getLayoutMode,
        isMobileMode,
        openCameraPicker: window.openCameraPicker,
        openRecordActionSheet: window.openRecordActionSheet,
        openThemeSettings,
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
    document.addEventListener('pointerdown', handleConfigYamlNavStart, true);
    document.addEventListener('touchstart', handleConfigYamlNavStart, true);
    document.addEventListener('mousedown', handleConfigYamlNavStart, true);
    document.addEventListener('pointerup', handleConfigYamlNavBypass, true);
    document.addEventListener('touchend', handleConfigYamlNavBypass, true);
    document.addEventListener('click', handleConfigYamlNavBypass, true);

    document.addEventListener('DOMContentLoaded', () => {
        const saved = normalizeTab(document.documentElement.dataset.mobileTab || localStorage.getItem('camkeep-mobile-tab') || 'devices');
        document.documentElement.dataset.mobileTab = saved;
        syncLayoutMode();
    });

    if (document.readyState !== 'loading') {
        syncLayoutMode();
    }
})();
