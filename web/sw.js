const CACHE_VERSION = 'z-coc-shell-v25';
const APP_SHELL = [
    './',
    './index.html',
    './Library/index.html',
    './Workshop/index.html',
    './404.html',
    './manifest.json',
    './favicon.svg',
    './assets/css/tailwind.css',
    './assets/art/generated/launch-ritual-moon-archive-v1.webp'
];

self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE_VERSION)
            .then((cache) => cache.addAll(APP_SHELL))
            .then(() => self.skipWaiting())
    );
});

self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys()
            .then((keys) => Promise.all(
                keys.filter((key) => key !== CACHE_VERSION).map((key) => caches.delete(key))
            ))
            .then(() => self.clients.claim())
    );
});

self.addEventListener('fetch', (event) => {
    const { request } = event;
    if (request.method !== 'GET' || request.headers.has('range')) return;

    const url = new URL(request.url);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return;
    if (url.origin === self.location.origin && url.pathname.endsWith('.php')) return;

    if (request.mode === 'navigate') {
        event.respondWith(
            fetch(request)
                .then(async (response) => {
                    if (response.ok) {
                        const cachedResponse = response.clone();
                        try {
                            const cache = await caches.open(CACHE_VERSION);
                            await cache.put(request, cachedResponse);
                        } catch (_) {
                            // A cache write failure must not block navigation.
                        }
                    }
                    return response;
                })
                .catch(async () => (
                    await caches.match(request)
                    || await caches.match('./index.html')
                ))
        );
        return;
    }

    const cacheableDestination = ['script', 'style', 'font', 'image', 'audio'].includes(request.destination);
    if (!cacheableDestination) return;

    const fetchAndCache = async () => {
        const response = await fetch(request);
        if (response.ok || response.type === 'opaque') {
            const cachedResponse = response.clone();
            await caches.open(CACHE_VERSION)
                .then((cache) => cache.put(request, cachedResponse));
        }
        return response;
    };

    const cachedResponse = caches.match(request);
    const networkResponse = fetchAndCache();
    event.waitUntil(networkResponse.then(() => undefined).catch(() => undefined));
    event.respondWith(cachedResponse.then((cached) => cached || networkResponse));
});
