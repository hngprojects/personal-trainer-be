# Apple integration

How the backend talks to Apple, what `.p8` keys it needs, and what the
mobile app must do.

This doc covers two surfaces:

1. **Sign in with Apple (SIWA)** — identity-token verification at sign-in
   (already works without any `.p8`), plus optional **token revocation**
   when a user deletes their account (Apple Review Guideline 5.1.1 (v)
   compliance — requires the `.p8`).
2. **APNs direct push** — bypasses Firebase to deliver iOS notifications
   straight to Apple. Falls back to FCM if not configured.

Apple IAP (App Store Server API + Notifications V2) is **not** wired up
yet; see the deferred-work note at the bottom.

---

## The `.p8` key

Apple Developer keys live at <https://developer.apple.com/account/resources/authkeys>.
A single key can have multiple services ticked at creation time — the
combinations that matter for this backend:

| Service                                  | Used by                       |
|------------------------------------------|-------------------------------|
| Apple Push Notifications service (APNs)  | Phase A2 (direct APNs)        |
| Sign in with Apple                       | Phase B (SIWA revocation)     |

Tick both when creating the key, then use the **same file** for
`APPLE_APNS_KEY_P8` and `APPLE_SIWA_KEY_P8`. The Key ID is the same in
both env vars too.

### What the `.p8` cannot do

The **App Store Server API** (modern IAP — refunds, subscription status,
ASNv2 webhook signing) requires a *separate* key from a *different*
portal: App Store Connect → Users and Access → Integrations → "In-App
Purchase" key type. The Developer Portal `.p8` will not work there. We
have config slots for it (`APPLE_API_KEY_ID`, `APPLE_API_KEY_P8`,
`APPLE_API_ISSUER_ID`) but no code reads them yet.

Once you can't tick more services on an existing key — services are
locked at creation. To add Sign in with Apple to an existing APNs-only
key, revoke + recreate.

### Encoding the `.p8` for `.env`

Two formats are accepted (auto-detected):

- **Raw PEM** — paste the file contents including the
  `-----BEGIN PRIVATE KEY-----` header. Works for single-line `.env`
  parsers but escaping newlines is painful in most YAML/k8s configs.
- **Base64** — wrap the raw PEM with base64 for clean single-line env
  values:

  ```powershell
  [Convert]::ToBase64String([IO.File]::ReadAllBytes("AuthKey_HR6V8D4T9T.p8")) | Set-Clipboard
  ```

  ```bash
  base64 -w0 AuthKey_HR6V8D4T9T.p8 | clip   # Windows Git Bash
  base64 -i  AuthKey_HR6V8D4T9T.p8 | pbcopy # macOS
  ```

---

## Phase B — Sign in with Apple revocation

### Env vars

```
APPLE_TEAM_ID=ABCDE12345        # 10-char team id (Apple Developer → Membership)
APPLE_SIWA_CLIENT_ID=com.fitcal.app
APPLE_SIWA_KEY_ID=HR6V8D4T9T    # 10-char key id
APPLE_SIWA_KEY_P8=<raw PEM or base64>
```

All four required for revocation to work. Missing config is non-fatal
(identity-token sign-in keeps working); the boot log notes when the
pipeline isn't wired.

`APPLE_SIWA_CLIENT_ID` is:
- the iOS app's **bundle id** for the native AuthenticationServices
  flow,
- the **Services ID** (registered in Apple Developer → Identifiers →
  Services IDs) for web "Sign in with Apple JS".

If you support both, register the Services ID in Apple Developer and
configure this env to it; native iOS sign-in still works because Apple
treats the bundle id as the audience and we match either.

### Mobile app changes required

The iOS app needs to forward `authorization_code` alongside
`identity_token` on the FIRST sign-in for each user:

```swift
// At the success callback of ASAuthorizationController:
guard
    let credential = authorization.credential as? ASAuthorizationAppleIDCredential,
    let identityData = credential.identityToken,
    let codeData = credential.authorizationCode,
    let identityToken = String(data: identityData, encoding: .utf8),
    let authCode = String(data: codeData, encoding: .utf8)
else { return }

let body: [String: Any] = [
    "id_token": identityToken,
    "authorization_code": authCode,       // ← NEW: include on first sign-in only
    "user": ["name": credential.fullName?.givenName ?? ""]
]
```

Apple delivers `authorizationCode` ONCE — on the first authorization for
a given Apple ID + app pair. On every subsequent sign-in `authorizationCode`
is `nil`; the backend silently skips the exchange in that case.

If your iOS team can't ship this immediately, no rush: identity-token
sign-in continues to work unchanged. Users who signed in before the
mobile update lands won't have a revoke-on-delete path, but their
deletion still succeeds — only the Apple-side cleanup is skipped.

### What happens server-side

1. **At sign-in** (`POST /auth/apple`): if `authorization_code` is
   present and the OAuth pipeline is configured, the backend POSTs to
   `https://appleid.apple.com/auth/token` with a `.p8`-signed
   `client_secret` JWT, receives a `refresh_token`, encrypts it with
   AES-256-GCM (reuses `ZOOM_TOKEN_ENCRYPTION_KEY`), and stores it in
   `users.apple_refresh_token_enc`.
2. **At delete** (`DELETE /users/me`): for users with
   `auth_provider='apple'` AND a refresh token on file, the backend
   POSTs to `https://appleid.apple.com/auth/revoke` with the
   client_secret JWT and the decrypted refresh token. Apple invalidates
   the token, and the app stops appearing under the user's
   "Apps using your Apple ID".
3. **Best-effort**: any failure at step 2 (Apple down, token already
   revoked, decryption error) is logged but doesn't block the account
   deletion. The user took the action; the audit log is the only
   record of a revocation failure.

### Schema impact

Migration `000070_add_apple_refresh_token.sql` adds a nullable
`users.apple_refresh_token_enc TEXT` column. Idempotent — re-running on
a DB that already has it is a no-op.

---

## Phase A2 — Direct APNs push

### What changes vs FCM

Today's notification path (`pkg/notification`) calls FCM for every
device regardless of platform; Firebase forwards iOS messages to APNs
under the hood. With direct APNs wired up:

- Device rows with `platform="ios"` route through `pkg/apple.APNSClient`
  (one HTTP/2 hop to `api.push.apple.com`).
- Device rows with `platform="android"` or `"web"` stay on FCM.
- Per-token dead-token cleanup works the same way on both channels —
  `BadDeviceToken` / `Unregistered` on APNs sets the row inactive
  identically to FCM's `registration-token-not-registered`.

### Env vars

```
APPLE_APNS_KEY_ID=HR6V8D4T9T
APPLE_APNS_KEY_P8=<raw PEM or base64>
APPLE_APNS_ENVIRONMENT=production    # or "sandbox"
```

`APPLE_TEAM_ID` and `APPLE_BUNDLE_ID` are also required (both already
set elsewhere in `.env`). Missing config is non-fatal — iOS push falls
back to FCM, and the boot log notes that direct APNs is disabled.

### sandbox vs production — getting this wrong is the #1 footgun

Apple has two completely separate APNs endpoints. Tokens issued by
different iOS provisioning profiles only work against the matching
endpoint:

| iOS build type                        | Token type   | `APPLE_APNS_ENVIRONMENT` |
|---------------------------------------|--------------|---------------------------|
| Xcode debug build / dev provisioning  | Sandbox      | `sandbox`                 |
| TestFlight                            | Production   | `production`              |
| App Store release                     | Production   | `production`              |

Sending a sandbox token to the production endpoint (or vice versa)
fails per-message with `BadDeviceToken`. The boot log includes the
configured environment so this is auditable.

If you support both — e.g. dev builds against staging and prod builds
against prod — set the right env per environment in their respective
`.env` files. Mixed registrations in a single backend aren't supported
in this version (would need a per-device tag and dual-pool dispatch).

### Mobile app changes required

Currently the iOS app registers an **FCM token** with the backend at
`POST /api/v1/register/device`. For direct APNs, it must register an
**APNs device token** instead:

```swift
// AppDelegate
func application(_ app: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
    let tokenHex = deviceToken.map { String(format: "%02x", $0) }.joined()
    // POST to /api/v1/register/device with body:
    //   { "device_token": tokenHex, "platform": "ios" }
    api.registerDevice(token: tokenHex, platform: "ios")
}
```

The token format is different (~64 hex characters for APNs vs the longer
Firebase-shaped FCM token); the registration endpoint stores whatever
the client sends and the backend routes by `platform`, so this works
out-of-the-box once the iOS team makes the SDK swap.

**Backwards compatibility**: existing rows with FCM tokens AND
`platform="ios"` will be rejected as `BadDeviceToken` when the backend
tries them against APNs — they'll be auto-deactivated on first send.
Mobile should re-register on the first launch after the SDK swap;
nothing on the server side breaks during the transition.

### Why not just stay on FCM?

Direct APNs has lower latency (one fewer hop) and gives us first-party
error visibility (Apple returns the reason directly, FCM normalizes
them). The trade-off is more complexity — you now maintain two send
paths instead of one — and a one-time mobile migration. The fallback
path means you can defer this rollout indefinitely; the FCM path
continues to work as long as Firebase has the APNs `.p8` uploaded to
Firebase Console.

---

## Phase C — Payment kill-switch (covered in [FEATURE_FLAGS.md](FEATURE_FLAGS.md))

Brief: `GET /api/v1/feature-flags` returns `{payment_enabled: bool}`;
the admin endpoint `PUT /api/v1/admin/feature-flags/payment_enabled`
flips it. The backend also enforces the flag server-side at
`POST /subscriptions` so a client that ignores the flag can't bypass
the kill-switch.

---

## Deferred — App Store Server API + Notifications V2

This needs the **second** `.p8` from App Store Connect (see the warning
at the top). Once you have it, set:

```
APPLE_API_KEY_ID=...
APPLE_API_KEY_P8=...
APPLE_API_ISSUER_ID=...
```

The slots exist in config but no code reads them yet. Adding them lets
us:
- Validate subscriptions server-to-server before granting access (fraud
  prevention).
- Accept ASNv2 webhooks at `POST /webhooks/apple/asn2` so refunds and
  cancellations expire access immediately instead of at the next
  client-driven check.

Separate PR — flag it when you're ready.
