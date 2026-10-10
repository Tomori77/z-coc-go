# z-coc Android App

The Android app uses Capacitor 8 with a locally bundled frontend. Cloud accounts,
cloud saves, multiplayer rooms, and the public library continue to use the
production PHP API at `https://z-coc.zeabur.app`.

## Build prerequisites

- Node.js 22 or newer
- Android Studio 2025.2.1 or newer
- JDK 21 for Gradle builds (newer IDE-bundled JDKs may not be supported by the
  project's Gradle version yet)
- An Android SDK platform (API 24 or newer; API 36 is recommended)

Android Studio uses its bundled runtime for the IDE. Command-line Gradle builds
should use JDK 21 through `JAVA_HOME`. iOS builds require macOS and Xcode.

## Zeabur API origin

Set the website service environment variable below before testing account,
library, or multiplayer features from the installed app:

```text
APP_ALLOWED_ORIGINS=https://localhost,capacitor://localhost
```

Keep the existing MySQL variables unchanged.

## Commands

```text
pnpm install
pnpm mobile:sync
pnpm mobile:open
```

To build a debug APK from the command line after Android Studio and the SDK are
installed:

```text
pnpm mobile:build:debug
```

The resulting APK is written under `android/app/build/outputs/apk/debug/`.
