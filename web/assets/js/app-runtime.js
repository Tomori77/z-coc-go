(() => {
    const API_ORIGIN = 'https://z-coc.zeabur.app';
    const capacitor = window.Capacitor;
    const isNative = Boolean(capacitor && typeof capacitor.isNativePlatform === 'function' && capacitor.isNativePlatform());

    const apiUrl = (input) => {
        const value = String(input || '').trim();
        if (!value || !isNative || /^(?:https?:)?\/\//i.test(value)) return value;
        const normalized = value.replace(/^(?:\.\.\/)+/, '').replace(/^\/+/, '');
        return new URL(normalized, `${API_ORIGIN}/`).toString();
    };

    window.ZCOC_RUNTIME = Object.freeze({
        apiOrigin: API_ORIGIN,
        apiUrl,
        isNative,
        platform: isNative && typeof capacitor.getPlatform === 'function' ? capacitor.getPlatform() : 'web',
    });

    if (isNative) {
        const root = document.documentElement;
        root.classList.add('z-native-app');
        root.dataset.platform = window.ZCOC_RUNTIME.platform;

        const nativeStyle = document.createElement('style');
        nativeStyle.id = 'z-native-runtime-style';
        nativeStyle.textContent = `
            html.z-native-app {
                --z-safe-top: max(env(safe-area-inset-top, 0px), var(--safe-area-inset-top, 0px));
                --z-safe-right: max(env(safe-area-inset-right, 0px), var(--safe-area-inset-right, 0px));
                --z-safe-bottom: max(env(safe-area-inset-bottom, 0px), var(--safe-area-inset-bottom, 0px));
                --z-safe-left: max(env(safe-area-inset-left, 0px), var(--safe-area-inset-left, 0px));
                width: 100%;
                height: 100%;
                background: #020617;
            }
            html.z-native-app body {
                width: 100%;
                height: 100%;
                box-sizing: border-box;
                padding: var(--z-safe-top) var(--z-safe-right) var(--z-safe-bottom) var(--z-safe-left) !important;
                overscroll-behavior: none;
                touch-action: manipulation;
            }
            html.z-native-app body::before {
                position: fixed !important;
                inset: 0 !important;
                width: auto !important;
                height: auto !important;
                transform: translateZ(0);
                contain: strict;
                backface-visibility: hidden;
            }
            html.z-native-app #app {
                min-width: 0;
                min-height: 0;
            }
            html.z-native-app .z-launch-screen,
            html.z-native-app .z-launch-poster,
            html.z-native-app .z-launch-art,
            html.z-native-app .z-launch-art-blur,
            html.z-native-app .z-launch-rays,
            html.z-native-app .z-launch-orbit {
                -webkit-transform-style: preserve-3d;
                transform-style: preserve-3d;
            }
            html.z-native-app .app-sidebar,
            html.z-native-app .drawer-side > .menu {
                will-change: transform;
                backface-visibility: hidden;
                isolation: isolate;
            }
            html.z-native-app .app-mobile-header,
            html.z-native-app .workshop-header,
            html.z-native-app .library-header {
                will-change: backdrop-filter;
                backface-visibility: hidden;
                isolation: isolate;
            }
            html.z-native-app .workshop-page-background,
            html.z-native-app .library-page-background {
                transform: translate3d(0, 0, 0);
                backface-visibility: hidden;
                contain: strict;
            }
        `;
        document.head.appendChild(nativeStyle);

        const hideStatusBar = () => {
            const systemBars = capacitor.Plugins && capacitor.Plugins.SystemBars;
            if (!systemBars || typeof systemBars.hide !== 'function') return;
            systemBars.hide({ bar: 'StatusBar' }).catch(() => undefined);
        };

        [0, 50, 180, 500, 1200].forEach((delay) => window.setTimeout(hideStatusBar, delay));
        window.addEventListener('load', hideStatusBar, { once: true });
        document.addEventListener('resume', hideStatusBar);
        document.addEventListener('visibilitychange', () => {
            if (!document.hidden) hideStatusBar();
        });
    }
})();
