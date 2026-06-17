(function () {
    function canRegisterServiceWorker() {
        if (!('serviceWorker' in navigator)) return false;
        if (location.protocol === 'https:') return true;
        return ['localhost', '127.0.0.1', '::1'].includes(location.hostname);
    }

    function registerServiceWorker() {
        if (!canRegisterServiceWorker()) return;
        window.addEventListener('load', function () {
            navigator.serviceWorker.register('/sw.js', {scope: '/'})
                .catch(function (error) {
                    console.debug('CamKeep service worker registration failed:', error);
                });
        });
    }

    function trackInstallPrompt() {
        window.addEventListener('beforeinstallprompt', function (event) {
            event.preventDefault();
            window.CamKeepPWA = window.CamKeepPWA || {};
            window.CamKeepPWA.deferredInstallPrompt = event;
            window.dispatchEvent(new CustomEvent('camkeep:pwa-install-available'));
        });

        window.addEventListener('appinstalled', function () {
            if (window.CamKeepPWA) {
                window.CamKeepPWA.deferredInstallPrompt = null;
            }
            document.documentElement.classList.add('is-installed-pwa');
        });
    }

    window.CamKeepPWA = window.CamKeepPWA || {};
    registerServiceWorker();
    trackInstallPrompt();
})();
