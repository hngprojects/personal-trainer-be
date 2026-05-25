# Zoom Integration — Backend + Mobile/Web Integration Guide

This document is the end-to-end contract between the backend and the mobile (React Native) + web clients for the FitCall Zoom integration. It covers:

1. The two new flows: **per-trainer hosting** and **in-app SDK joins**.
2. The backend HTTP API the clients call.
3. What the mobile app and web app each need to wire up to make the in-app join work without breaking older builds.

If anything in the API doesn't match what's described here, the file headers in `internal/routes/zoom_*.go` are the source of truth.

---

## 1. Concept

There are two orthogonal feature flags on the backend:

| Env var             | Values                 | What it controls                                        |
| ------------------- | ---------------------- | ------------------------------------------------------- |
| `ZOOM_MEETING_HOST` | `org` (default), `trainer` | Whose Zoom account hosts the meeting                 |
| `ZOOM_JOIN_MODE`    | `link` (default), `sdk`    | What the "Join" button in the email points at        |

They compose:

| Host mode | Join mode | User experience                                                            |
| --------- | --------- | -------------------------------------------------------------------------- |
| `org`     | `link`    | **Current behaviour.** Org Zoom account hosts; email link opens Zoom app. |
| `trainer` | `link`    | Trainer's Zoom hosts (falls back to org if not connected); email opens Zoom. |
| `org`     | `sdk`     | Org hosts; email link opens FitCall app, which joins via Meeting SDK.     |
| `trainer` | `sdk`     | **End-state.** Trainer hosts in-app; client joins in-app via SDK.         |

Setting either flag to its non-default value never breaks an existing trainer who hasn't been onboarded: the backend silently downgrades whatever piece is missing.

---

## 2. Backend setup

Three Zoom Marketplace apps are involved — they are distinct apps under the same Zoom account, do not try to share creds:

| App type                | Purpose                          | Env vars                                                                                  |
| ----------------------- | -------------------------------- | ----------------------------------------------------------------------------------------- |
| Server-to-Server OAuth  | Org-account hosting (existing)   | `ZOOM_ACCOUNT_ID`, `ZOOM_CLIENT_ID`, `ZOOM_CLIENT_SECRET`                                 |
| OAuth                   | Per-trainer hosting (new)        | `ZOOM_OAUTH_CLIENT_ID`, `ZOOM_OAUTH_CLIENT_SECRET`, `ZOOM_OAUTH_REDIRECT_URL`             |
| Meeting SDK             | In-app joins (new)               | `ZOOM_SDK_KEY`, `ZOOM_SDK_SECRET`                                                          |

Plus:

| Env var                       | Purpose                                                                  |
| ----------------------------- | ------------------------------------------------------------------------ |
| `ZOOM_TOKEN_ENCRYPTION_KEY`   | base64-encoded 32-byte key. `openssl rand -base64 32`. AES-256-GCM at rest. |
| `UNIVERSAL_LINK_DOMAIN`       | e.g. `app.fitcall.me`. Used in the email "Join" URL when `ZOOM_JOIN_MODE=sdk`. |
| `IOS_APP_BUNDLE_ID` + `IOS_APP_TEAM_ID` | Baked into `/.well-known/apple-app-site-association`.            |
| `ANDROID_APP_PACKAGE` + `ANDROID_APP_SHA256` | Baked into `/.well-known/assetlinks.json`.                   |

OAuth redirect URL on the Zoom OAuth app must exactly match `ZOOM_OAUTH_REDIRECT_URL`. Default is `http://localhost:8080/api/v1/trainers/me/zoom/callback` for local dev.

Required Zoom OAuth scopes on the per-user app: `meeting:write`, `meeting:read`, `user:read`.

---

## 3. New backend endpoints

All paths under `/api/v1` unless stated. All `Bearer` auth except where noted.

### Per-trainer OAuth

```
GET    /trainers/me/zoom/connect
       → 200 { "data": { "authorize_url": "https://zoom.us/oauth/authorize?..." } }
       Open authorize_url in an in-app browser or the system browser.

GET    /trainers/me/zoom/callback?code=...&state=...
       No auth — Zoom redirects here directly. Returns 200 JSON on success.
       On success: { "data": { "zoom_email": "...", "expires_at": "..." } }

GET    /trainers/me/zoom/status
       → 200 { "data": { "connected": true, "zoom_email": "...", "access_token_expires_at": "..." } }
       or   { "data": { "connected": false } }

DELETE /trainers/me/zoom
       → 200 on success; 404 if there was nothing to disconnect.
```

### In-app SDK join

```
GET /sessions/{session_id}/join-info
    → 200 {
        "data": {
          "sdk_key":        "abc...",          # public; safe to log
          "meeting_number": "98765432100",
          "signature":      "<JWT>",           # short-lived, ~2h
          "role":           0 | 1,             # 0 = participant, 1 = host
          "join_url":       "https://us05web.zoom.us/j/..."   # fallback
        }
      }
    Auth: caller must be the booking's client OR the booking's trainer.
    503 if the Meeting SDK key isn't configured server-side.
```

### Client-facing config

```
GET /config/zoom
    → 200 {
        "data": {
          "join_mode":             "link" | "sdk",
          "sdk_configured":        true | false,
          "sdk_key":               "abc...",
          "universal_link_domain": "app.fitcall.me"
        }
      }
    The app polls this on launch (or caches it) to decide which join flow to use.
```

### Universal-link claim files

These live at the **root** of the universal-link domain, not under `/api/v1`. The backend serves them generated from config (rotate keys without a redeploy):

```
GET /.well-known/apple-app-site-association
GET /.well-known/assetlinks.json
```

Both return `Content-Type: application/json` and a `404 {}` body if their respective env vars aren't set.

---

## 4. Mobile app integration (React Native)

### 4.1 Install the Zoom SDK

The React Native ecosystem has two options:

| Package                       | Status                            |
| ----------------------------- | --------------------------------- |
| `@zoom/react-native-videosdk` | Official; recommended for new code.|
| Community `react-native-zoom-us` | Bridges the iOS/Android Meeting SDK; older but more battle-tested. |

Pick `@zoom/react-native-videosdk` unless you have a hard requirement on the Meeting SDK's full feature set. The integration shape below applies to either — only the `init()` and `joinMeeting()` API names differ.

```bash
yarn add @zoom/react-native-videosdk
cd ios && pod install
```

iOS-specific build settings the Zoom SDK requires:
- iOS deployment target ≥ 12.0.
- `NSCameraUsageDescription` and `NSMicrophoneUsageDescription` in `Info.plist`.
- "Privacy - Bluetooth Always Usage Description" if you want hands-free pairing.

Android:
- minSdkVersion ≥ 23.
- `android:largeHeap="true"` on `<application>`.

### 4.2 Universal links

**iOS:**

1. In Xcode → target → Signing & Capabilities → add **Associated Domains**.
2. Add `applinks:app.fitcall.me` (replace with your `UNIVERSAL_LINK_DOMAIN`).
3. In `Info.plist`, ensure your app's bundle ID matches `IOS_APP_BUNDLE_ID` and your team ID matches `IOS_APP_TEAM_ID`.
4. On a clean install, iOS fetches `https://<domain>/.well-known/apple-app-site-association` and caches it. Re-fetch happens periodically but you can force one with `_:performActionFor:completionHandler:` during dev.

Verify with: `swcutil dl -d app.fitcall.me` on macOS.

**Android:**

In `android/app/src/main/AndroidManifest.xml`, on the activity that hosts the join screen:

```xml
<intent-filter android:autoVerify="true">
    <action android:name="android.intent.action.VIEW" />
    <category android:name="android.intent.category.DEFAULT" />
    <category android:name="android.intent.category.BROWSABLE" />
    <data android:scheme="https"
          android:host="app.fitcall.me"
          android:pathPattern="/sessions/.*/join" />
</intent-filter>
```

The `android:host` and the package name must match what the backend serves in `/.well-known/assetlinks.json`, and the signing-cert SHA-256 fingerprint must match `ANDROID_APP_SHA256`. Get the fingerprint with:

```bash
keytool -list -v -keystore my-release.keystore | grep SHA256
```

Verify with: `adb shell pm get-app-links com.fitcall.app` after a fresh install.

### 4.3 On-launch config fetch

```ts
const { data } = await api.get("/config/zoom");
//   { join_mode: "sdk", sdk_configured: true, sdk_key: "...", universal_link_domain: "app.fitcall.me" }
appConfig.zoom = data;
```

Cache this in state — there is no need to re-fetch per request, but do re-fetch on app foreground if the cache is older than ~1 hour.

### 4.4 Joining a meeting

When the user taps the "Join" button OR a universal link opens the app, you end up with a `session_id` (UUID). From there:

```ts
// 1. Ask the backend for join info (auth required)
const { data } = await api.get(`/sessions/${sessionId}/join-info`);
const { sdk_key, meeting_number, signature, role, join_url } = data;

// 2. If we got an SDK signature AND the SDK is bundled in this build, join in-app.
if (appConfig.zoom.join_mode === "sdk" && appConfig.zoom.sdk_configured && ZoomSDK) {
  await ZoomSDK.initialize({ sdkKey: sdk_key, sdkSecret: "" }); // secret stays server-side
  await ZoomSDK.joinMeeting({
    meetingNumber: meeting_number,
    signature,
    role,
    userName: currentUser.displayName,
    userEmail: currentUser.email,
  });
  return;
}

// 3. Fallback: open the raw Zoom join URL (works on every build).
Linking.openURL(join_url);
```

The SDK secret is NEVER shipped to the client — only the JWT signature is. If your SDK wrapper insists on a secret parameter, pass an empty string; the signature already encodes everything Zoom needs.

### 4.5 Per-trainer Connect Zoom (trainer-only screen)

Inside the trainer's profile / settings screen:

```ts
// On render:
const { data } = await api.get("/trainers/me/zoom/status");
// → { connected: true, zoom_email: "trainer@example.com", access_token_expires_at: "..." }
//   or { connected: false }

// On "Connect Zoom" tap:
const { data } = await api.get("/trainers/me/zoom/connect");
// → { authorize_url: "https://zoom.us/oauth/authorize?..." }
await InAppBrowser.open(data.authorize_url);
//   The browser will redirect to ZOOM_OAUTH_REDIRECT_URL on success.
//   That URL hits the backend's callback handler which closes out the OAuth flow
//   and returns JSON. The backend deployment typically wraps this in a small
//   landing page that says "You can close this window" — for mobile you should
//   poll /trainers/me/zoom/status after the in-app-browser closes, OR have the
//   callback redirect to a deep link like fitcall://zoom/connected that the app
//   intercepts.

// On "Disconnect Zoom":
await api.delete("/trainers/me/zoom");
```

### 4.6 Email "Join" link handling

When the user taps the "Join" button in their booking confirmation email:

- **If `ZOOM_JOIN_MODE=link`** (or universal links aren't claimed): the email contains the raw Zoom URL. iOS/Android opens Zoom proper. The app is not involved.
- **If `ZOOM_JOIN_MODE=sdk`**: the email contains `https://<UNIVERSAL_LINK_DOMAIN>/sessions/<session_id>/join`. The OS routes this to your app via universal-/app-links. Extract the `session_id`, then call the join flow in 4.4.

If the app isn't installed, the URL opens in the browser. To handle that gracefully, deploy a tiny static page at `/sessions/*/join` on `UNIVERSAL_LINK_DOMAIN` that says "Open in app / install the app." The current backend does NOT serve a fallback at that path — you'll get a 404 from gin. **TODO: add a fallback page in a follow-up PR.**

---

## 5. Web app integration

The web app uses Zoom's [Meeting SDK for Web](https://developers.zoom.us/docs/meeting-sdk/web/) instead of the React Native SDK, but the backend contract is identical: hit `/sessions/{id}/join-info` and pass the result into `ZoomMtg.init` / `ZoomMtg.join`.

The web app does NOT need universal-link handling — the booking email link goes through the normal browser flow. When `ZOOM_JOIN_MODE=sdk` and the user is on the web app, the deep-link URL opens an in-app route that calls `/sessions/{id}/join-info` and embeds the Zoom Meeting SDK iframe.

---

## 6. Troubleshooting

| Symptom                                                       | Likely cause                                                          |
| ------------------------------------------------------------- | --------------------------------------------------------------------- |
| `/config/zoom` says `sdk_configured: false`                   | Backend `ZOOM_SDK_KEY` / `ZOOM_SDK_SECRET` missing. Fall back to link. |
| Email opens Zoom app instead of FitCall                       | Either `ZOOM_JOIN_MODE != sdk` OR universal-link verification failed. Check `/.well-known/apple-app-site-association` returns a non-empty body with your bundle/team IDs, and that the SHA-256 in `assetlinks.json` matches your signing cert. |
| In-app join 401s from Zoom                                    | The SDK JWT we minted is for a different `sdk_key` than what the client initialised with. Check `/config/zoom`'s `sdk_key` matches what you pass to `ZoomSDK.initialize`. |
| Trainer's Zoom dashboard doesn't show the new meeting         | Either `ZOOM_MEETING_HOST=org` OR the trainer hasn't connected. Check `/trainers/me/zoom/status`. |
| Trainer connects but next booking still uses org account      | Refresh-token race or expired credentials. Check the backend logs for `zoomflow: refresh failed` — that drops the connection and surfaces `last_failure_reason` on `user_zoom_credentials`. |
