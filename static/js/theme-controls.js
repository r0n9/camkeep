(function () {
    let themeControlsInitialized = false;

    function getStoredMode() {
        const saved = localStorage.getItem('camkeep-theme');
        if (saved === 'light' || saved === 'dark' || saved === 'system') return saved;
        return 'system';
    }

    function getStoredSkin() {
        return localStorage.getItem('camkeep-skin') === 'classic' ? 'classic' : 'neu';
    }

    function applyMode(mode) {
        const normalized = mode === 'light' || mode === 'dark' || mode === 'system' ? mode : 'system';
        const root = document.documentElement;
        if (normalized === 'system') {
            const prefersDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches === true;
            root.classList.toggle('dark', prefersDark);
            localStorage.setItem('camkeep-theme', 'system');
        } else {
            root.classList.toggle('dark', normalized === 'dark');
            localStorage.setItem('camkeep-theme', normalized);
        }
    }

    function applySkin(skin) {
        const normalized = skin === 'classic' ? 'classic' : 'neu';
        document.documentElement.dataset.skin = normalized;
        const link = document.getElementById('neuStyle');
        if (link) link.disabled = normalized === 'classic';
        localStorage.setItem('camkeep-skin', normalized);
    }

    function setMode(mode) {
        applyMode(mode);
        syncThemeSegments();
        window.CamKeepMobile?.syncThemeControls?.();
    }

    function setSkin(skin) {
        applySkin(skin);
        syncThemeSegments();
        window.CamKeepMobile?.syncThemeControls?.();
    }

    function syncThemeSegments(root = document) {
        const mode = getStoredMode();
        const skin = getStoredSkin();
        root.querySelectorAll('.theme-seg-btn').forEach(btn => {
            const active = btn.dataset.mode === mode || btn.dataset.skin === skin;
            btn.classList.toggle('is-active', active);
            btn.setAttribute('aria-checked', active ? 'true' : 'false');
        });
    }

    function setThemeMenuOpen(menuId, open) {
        const menu = document.getElementById(menuId);
        if (!menu) return;
        const button = menu.querySelector('.theme-menu-button');
        const panel = menu.querySelector('.theme-menu-panel');
        if (!button || !panel) return;
        panel.classList.toggle('hidden', !open);
        button.classList.toggle('is-open', open);
        button.setAttribute('aria-expanded', open ? 'true' : 'false');
    }

    function closeOtherThemeMenus(activeMenuId) {
        document.querySelectorAll('.theme-menu').forEach(menu => {
            if (menu.id && menu.id !== activeMenuId) setThemeMenuOpen(menu.id, false);
        });
    }

    function toggleThemeMenu(event, menuId = 'themeMenu') {
        event?.stopPropagation();
        const menu = document.getElementById(menuId);
        const panel = menu?.querySelector('.theme-menu-panel');
        const willOpen = panel ? panel.classList.contains('hidden') : false;
        closeOtherThemeMenus(menuId);
        setThemeMenuOpen(menuId, willOpen);
    }

    function initThemeControls() {
        syncThemeSegments();
        if (themeControlsInitialized) return;
        themeControlsInitialized = true;

        document.addEventListener('click', event => {
            document.querySelectorAll('.theme-menu').forEach(menu => {
                if (!menu.contains(event.target)) setThemeMenuOpen(menu.id, false);
            });
        });
        document.addEventListener('keydown', event => {
            if (event.key === 'Escape') {
                document.querySelectorAll('.theme-menu').forEach(menu => setThemeMenuOpen(menu.id, false));
            }
        });

        if (window.matchMedia) {
            const media = window.matchMedia('(prefers-color-scheme: dark)');
            media.addEventListener('change', () => {
                if (getStoredMode() === 'system') {
                    document.documentElement.classList.toggle('dark', media.matches);
                }
            });
        }
    }

    window.getStoredMode = getStoredMode;
    window.getStoredSkin = getStoredSkin;
    window.applyMode = applyMode;
    window.applySkin = applySkin;
    window.setMode = setMode;
    window.setSkin = setSkin;
    window.syncThemeSegments = syncThemeSegments;
    window.toggleThemeMenu = toggleThemeMenu;
    window.setThemeMenuOpen = open => setThemeMenuOpen('themeMenu', open);
    window.initThemeControls = initThemeControls;

    document.addEventListener('DOMContentLoaded', initThemeControls);
    if (document.readyState !== 'loading') initThemeControls();
})();
