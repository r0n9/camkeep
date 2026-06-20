const CACHE_VERSION = 'v5';
const STATIC_CACHE = `camkeep-static-${CACHE_VERSION}`;

const CORE_STATIC_ASSETS = [
    '/static/css/DPlayer.min.css',
    '/static/css/camera-nodes.css',
    '/static/css/index.css',
    '/static/css/mobile-shell.css',
    '/static/css/mobile-responsive.css',
    '/static/css/neumorphism.css',
    '/static/css/ptz.css',
    '/static/css/record-timeline-24h.css',
    '/static/css/user-management.css',
    '/static/image/camkeep_w200.png',
    '/static/image/camkeep_w200_bak.png',
    '/static/image/camkeep_w80.png',
    '/static/image/camkeep_pwa_192.png',
    '/static/image/camkeep_pwa_512.png',
    '/static/js/DPlayer.min.js',
    '/static/js/camera-nodes.js',
    '/static/js/index.js',
    '/static/js/mobile-actions.js',
    '/static/js/mobile-shell.js',
    '/static/js/mpegts.min.js',
    '/static/js/ptz.js',
    '/static/js/pwa.js',
    '/static/js/record-timeline-24h.js',
    '/static/js/record-timeline.js',
    '/static/js/tailwindcss.min.js',
    '/static/js/user-management.js'
];

const BYPASS_PREFIXES = [
    '/api/',
    '/play/',
    '/play_hls/',
    '/play_transcode/',
    '/play_remux/',
    '/webrtc/',
    '/stream.html',
    '/video-stream.js',
    '/video-rtc.js',
    '/webrtc.html',
    '/login',
    '/logout'
];

const NETWORK_FIRST_STATIC_EXTENSIONS = [
    '.css',
    '.js'
];

self.addEventListener('install', event => {
    event.waitUntil(
        caches.open(STATIC_CACHE)
            .then(cache => cache.addAll(CORE_STATIC_ASSETS))
            .then(() => self.skipWaiting())
    );
});

self.addEventListener('activate', event => {
    event.waitUntil(
        caches.keys()
            .then(keys => Promise.all(keys
                .filter(key => key.startsWith('camkeep-static-') && key !== STATIC_CACHE)
                .map(key => caches.delete(key))))
            .then(() => self.clients.claim())
    );
});

self.addEventListener('fetch', event => {
    const request = event.request;
    if (request.method !== 'GET') return;

    const url = new URL(request.url);
    if (url.origin !== self.location.origin || shouldBypass(url.pathname)) return;

    if (request.mode === 'navigate') {
        event.respondWith(networkFirstNavigation(request));
        return;
    }

    if (url.pathname.startsWith('/static/')) {
        event.respondWith(shouldNetworkFirstStatic(url.pathname)
            ? networkFirstStatic(request)
            : cacheFirstStatic(request));
    }
});

function shouldBypass(pathname) {
    return BYPASS_PREFIXES.some(prefix => pathname === prefix || pathname.startsWith(prefix));
}

function shouldNetworkFirstStatic(pathname) {
    return NETWORK_FIRST_STATIC_EXTENSIONS.some(ext => pathname.endsWith(ext));
}

async function cacheFirstStatic(request) {
    const cached = await caches.match(request);
    const networkFetch = fetch(request)
        .then(response => {
            if (response && response.ok) {
                const clone = response.clone();
                caches.open(STATIC_CACHE).then(cache => cache.put(request, clone));
            }
            return response;
        })
        .catch(() => null);

    return cached || await networkFetch || offlinePlainResponse();
}

async function networkFirstStatic(request) {
    const cached = await caches.match(request);
    try {
        const response = await fetch(request);
        if (response && response.ok) {
            const clone = response.clone();
            caches.open(STATIC_CACHE).then(cache => cache.put(request, clone));
        }
        return response;
    } catch (e) {
        return cached || offlinePlainResponse();
    }
}

async function networkFirstNavigation(request) {
    try {
        return await fetch(request);
    } catch (e) {
        return new Response(offlineHtml(), {
            status: 503,
            statusText: 'Offline',
            headers: {'Content-Type': 'text/html; charset=utf-8'}
        });
    }
}

function offlinePlainResponse() {
    return new Response('CamKeep is offline.', {
        status: 503,
        statusText: 'Offline',
        headers: {'Content-Type': 'text/plain; charset=utf-8'}
    });
}

function offlineHtml() {
    return `<!doctype html>
<html lang="zh-CN">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
    <title>CamKeep 离线</title>
    <style>
        :root { color-scheme: light dark; }
        body {
            margin: 0;
            min-height: 100vh;
            display: grid;
            place-items: center;
            padding: 24px;
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
            background: #eef3f8;
            color: #172033;
        }
        main {
            width: min(360px, 100%);
            border: 1px solid rgba(15, 23, 42, .12);
            border-radius: 18px;
            background: rgba(255, 255, 255, .86);
            padding: 22px;
            box-shadow: 0 18px 45px rgba(15, 23, 42, .14);
        }
        h1 { margin: 0 0 8px; font-size: 20px; }
        p { margin: 0; line-height: 1.65; color: #475569; }
        @media (prefers-color-scheme: dark) {
            body { background: #0f172a; color: #e5edf7; }
            main { background: rgba(15, 23, 42, .9); border-color: rgba(148, 163, 184, .2); }
            p { color: #aab8cc; }
        }
    </style>
</head>
<body>
    <main>
        <h1>CamKeep 当前离线</h1>
        <p>请检查网络、反向代理或本机服务状态后重试。实时画面、录像和接口不会在离线状态下缓存。</p>
    </main>
</body>
</html>`;
}
