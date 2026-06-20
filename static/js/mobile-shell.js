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

    function commitTab(next, options = {}) {
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

    function setTab(tab, options = {}) {
        const next = normalizeTab(tab);
        const yamlEditorRoot = document.getElementById('mobileYamlEditorRoot');
        if (!yamlEditorRoot?.classList.contains('hidden')) {
            window.closeMobileYamlEditor?.(false);
        }
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
        commitTab(next, options);
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

    function escapeHtmlValue(value) {
        return String(value ?? '')
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
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

    function openAbout() {
        if (!isMobileMode()) return false;
        const version = document.getElementById('appVersionText')?.textContent?.trim() || 'dev';
        const body = document.createElement('div');
        body.className = 'mobile-about-sheet';
        body.innerHTML = `
            <div class="mobile-about-hero">
                <img src="/static/image/camkeep_w80.png" alt="CamKeep">
                <div>
                    <strong>CamKeep NVR</strong>
                    <span>全面兼容 go2rtc 的自托管 NVR</span>
                </div>
            </div>
            <div class="mobile-about-copy">
                面向家庭 NAS、低功耗小主机和边缘设备，基于 Go、go2rtc 与 FFmpeg，提供本地优先的视频接入、录制、回放和设备管理能力。
            </div>
            <div class="mobile-about-points" aria-label="CamKeep 功能亮点">
                <span>go2rtc-native 接入</span>
                <span>ONVIF 控制与事件诊断</span>
                <span>24H 时间轴回放</span>
                <span>本地用户与权限</span>
            </div>
            <div class="mobile-about-meta">
                <div>
                    <span>当前版本</span>
                    <strong>${escapeHtmlValue(version)}</strong>
                </div>
                <div>
                    <span>开源协议</span>
                    <strong>MIT License</strong>
                </div>
                <div>
                    <span>部署场景</span>
                    <strong>NAS / 边缘设备</strong>
                </div>
                <div>
                    <span>隐私模式</span>
                    <strong>本地优先</strong>
                </div>
            </div>
            <div class="mobile-about-actions">
                <a href="https://github.com/r0n9/camkeep" target="_blank" rel="noopener" class="mobile-about-link">
                    <span>GitHub 项目</span>
                    <svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M7 17L17 7M9 7h8v8"></path>
                    </svg>
                </a>
                <a href="https://hub.docker.com/r/r0n9/camkeep" target="_blank" rel="noopener" class="mobile-about-link">
                    <span>Docker 镜像</span>
                    <svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M7 17L17 7M9 7h8v8"></path>
                    </svg>
                </a>
                <a href="https://github.com/r0n9/camkeep/releases/latest" target="_blank" rel="noopener" class="mobile-about-link">
                    <span>查看最新版本</span>
                    <svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M7 17L17 7M9 7h8v8"></path>
                    </svg>
                </a>
                <a href="https://github.com/AlexxIT/go2rtc" target="_blank" rel="noopener" class="mobile-about-link">
                    <span>go2rtc 项目</span>
                    <svg fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M7 17L17 7M9 7h8v8"></path>
                    </svg>
                </a>
            </div>
        `;
        return openSheet('关于', 'CamKeep NVR', body);
    }

    window.CamKeepMobile = {
        applyRecordQuickRange: window.applyRecordQuickRange,
        closeSheet,
        closeTopMostOverlay,
        getLayoutMode,
        isMobileMode,
        openCameraPicker: window.openCameraPicker,
        openAbout,
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
        if (event.key === 'Escape' && !document.getElementById('mobileYamlEditorRoot')?.classList.contains('hidden')) {
            window.closeMobileYamlEditor?.(false);
            return;
        }
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
