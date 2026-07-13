# Feature flags

Runtime kill-switches the admin can flip without a deploy. Backed by
the `feature_flags` table (Postgres) with a 5-minute Redis cache on
the read path.

## Current flags

| Key                | Default | What it controls                                              |
|--------------------|---------|---------------------------------------------------------------|
| `payment_enabled`  | `false` | Whether `POST /subscriptions` accepts purchase verifications. |

Off-by-default is deliberate: a fresh DB doesn't accept any payments
until the operator flips them on, so a misconfigured staging
environment can't accidentally process production purchases.

## Endpoints

### `GET /api/v1/feature-flags` — public

No auth. Returns a flat map of `{key: bool}` — mobile clients call this
at startup (and before showing any flag-gated UI) to decide which
features to surface.

```json
{
  "status": "success",
  "code": "OK",
  "message": "feature flags retrieved",
  "data": {
    "payment_enabled": false
  }
}
```

**The mobile app must not trust this read for security.** It's a UX
hint — the backend still enforces every flag at the relevant
write/action endpoint. Treating the flag as a UI gate is correct;
treating it as the only check would let a motivated client bypass it.

### `GET /api/v1/admin/feature-flags` — admin / super_admin

Same map plus audit metadata (`updated_by`, `updated_at`, `notes`) for
each flag. Used by the admin dashboard's kill-switch page.

### `PUT /api/v1/admin/feature-flags/{key}` — admin / super_admin

```json
{
  "enabled": true,
  "notes": "Re-enabling after merchant outage 14:32"
}
```

- Key must be one of the server's known flags — typos return 400.
- Records the caller as `updated_by` for audit.
- Cache is invalidated post-commit; the next public GET sees the new
  value within seconds.

## Server-side enforcement

Every flag has at least one server-side enforcement point. The mobile
client hiding the UI is a UX nicety; the real gate is here.

| Flag              | Enforced at                                           |
|-------------------|-------------------------------------------------------|
| `payment_enabled` | `POST /subscriptions` — returns 503 when off          |

To add a new flag with server-side enforcement:

1. Add a constant to `internal/feature_flags/service.go`.
2. Add the key to `isKnownKey` in
   `internal/feature_flags/handler.go` so the admin endpoint accepts
   PUTs for it.
3. Seed the row in a new migration (default `FALSE` for safety).
4. Call `s.featureFlagsSvc.IsEnabled(ctx, "<key>")` at the relevant
   handler entry point and 503/refuse when it's off.

## Operational notes

- **The Redis cache is best-effort.** Reads bypass cache and hit
  Postgres directly if Redis is down — slower, still correct.
- **Default is OFF.** If Redis AND Postgres are both unreachable
  (full DB outage), `IsEnabled` returns false. This means a flag
  outage refuses to grant gated features rather than letting them
  through unchecked. For `payment_enabled` that's the right
  conservative default: refuse purchases rather than process them
  without verification.
- **Boot doesn't depend on Redis.** A fresh environment with no Redis
  reads from Postgres on every check (cache layer is invisible). It's
  fine to leave Redis off in dev.
