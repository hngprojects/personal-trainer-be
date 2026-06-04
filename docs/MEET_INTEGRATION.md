# Google Meet integration — operator runbook

End-to-end setup + day-2 ops for the org-account Google Meet flow shipped
in [`feat/meet-and-messenger`](https://github.com/hngprojects/personal-trainer-be).

This is a deliberately small integration: **one** Workspace user mints
**all** Meet rooms via a single refresh token in env. No per-trainer OAuth,
no calendar events, no SDK embedding. The Meet REST API's `spaces.create`
returns a join URL and we put that in the booking. That's the whole loop.

## Architecture

```
                    POST /bookings (session_platform=google_meet)
                              │
                              ▼
              ┌──────────────────────────────┐
              │  bookings.Service            │
              │  Selector.For(_, "google_meet") │
              └──────────────┬───────────────┘
                             │
                             ▼
         ┌─────────────────────────────────────────┐
         │  pkg/googlemeet.Provider                │
         │  ▾ OAuthClient.AccessToken              │
         │    (refreshes once/hour, cached)        │
         │  ▾ POST https://meet.googleapis.com/v2  │
         │       /spaces                           │
         └─────────────────────────────────────────┘
                             │
                             ▼
                  {meetingUri, name}
                             │
                             ▼
                Persisted to bookings.zoom_meeting_link
                (column name is legacy; URL is whatever
                 platform was chosen)
```

## One-time setup per environment

### 1. Google Workspace

Sign up at workspace.google.com — Business Starter ($6/user/month) is
enough. Verify your domain via the DNS TXT record (~10 min).

Create ONE dedicated user, e.g. `meet-bot@yourdomain.com`. Don't use
it for human email. Don't share the password — the only access we
need is the OAuth refresh token below.

### 2. Google Cloud project

At [console.cloud.google.com](https://console.cloud.google.com):

1. **Create a new project** (or reuse). Name doesn't matter; we
   recommend `fitcall-meet` for clarity.
2. **Enable the Google Meet REST API**: APIs & Services → Library →
   search "Google Meet API" → Enable.
3. **Configure the OAuth consent screen**:
   - User Type: **Internal** (only your Workspace can sign in)
   - Add scope `https://www.googleapis.com/auth/meetings.space.created`
4. **Create OAuth credentials**: APIs & Services → Credentials →
   Create Credentials → OAuth Client ID:
   - Application type: **Web application**
   - Authorized redirect URIs: `http://localhost:8765/callback`
   - Copy the Client ID + Client Secret — they go into env below.

### 3. Generate the refresh token

On any machine with a browser (your laptop is fine — this is a
one-time grab, the token then lives in server env):

```bash
MEET_OAUTH_CLIENT_ID=<client-id> \
MEET_OAUTH_CLIENT_SECRET=<client-secret> \
    go run ./cmd/meet-bootstrap
```

The script:
1. Prints an authorize URL — open in browser.
2. Sign in as `meet-bot@yourdomain` (NOT your personal account).
3. Grant the Meet scope.
4. Browser redirects to `http://localhost:8765/callback`; the script
   catches it.
5. Script prints the refresh token. Paste into server `.env`.

### 4. Server `.env`

```env
MEET_ENABLED=true
MEET_OAUTH_CLIENT_ID=<from step 2>
MEET_OAUTH_CLIENT_SECRET=<from step 2>
MEET_REFRESH_TOKEN=<from step 3>
MEET_HOST_EMAIL=meet-bot@yourdomain
```

Restart the server. Boot log should show `google meet provider ready`.

### 5. Smoke test

```bash
curl -X POST https://<host>/api/v1/bookings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "trainer_id": "<uuid>",
    "scheduled_start": "2026-12-01T10:00:00Z",
    "scheduled_end":   "2026-12-01T11:00:00Z",
    "timezone":        "America/New_York",
    "session_platform": "google_meet"
  }'
```

Expect: 200 with a booking that has a `zoom_meeting_link` of the form
`https://meet.google.com/xxx-yyyy-zzz`. Open it in a browser — should
land on a working Meet room.

## Day-2 ops

### Symptom: every Meet booking returns 503

- Check `MEET_ENABLED=true` in `.env`.
- Check all four MEET_* env vars are set.
- Check server logs for `MEET_ENABLED=true but ... missing — meet
  provider disabled`.

### Symptom: every Meet booking 5xxs with "googlemeet: refresh token rejected"

The refresh token was revoked. Common causes:
- The `meet-bot` account password was changed.
- Google's anti-abuse flagged the account for unusual activity (rare
  for Workspace accounts, more common with free Gmail).
- An admin manually revoked the grant at
  [myaccount.google.com/permissions](https://myaccount.google.com/permissions).

Fix: re-run the bootstrap helper from step 3, paste the new token
into `.env`, restart. ~5 minute ops.

### Symptom: Google rate-limiting

The Meet API's `spaces.create` quota is 60 requests/min per project
by default. At normal booking volume you're nowhere near this. If you
hit it (sustained 200+ bookings/min during a launch promo):

- Request a quota increase via the Google Cloud Console quotas page.
- Or batch-create rooms in advance.

### Symptom: empty meeting URL in booking row

The provider's defensive check (empty `meetingUri` from Google →
booking fails) means you should never see this — the booking would
have failed at creation time. If you do see it in an old row, that's
from before this PR landed; just nudge the trainer to share a fresh
link out-of-band.

### Rolling back

Set `MEET_ENABLED=false` and restart. The FE's `/config/meetings`
(when wired) will hide the Meet option; any inbound booking with
`session_platform=google_meet` returns 503. Existing Meet bookings
keep their links (they're stable Google URLs).

No DB rollback needed — the migration is additive.

## Cost model

- Google Workspace: $6/user/month for ONE user, regardless of booking
  volume. Tracks no extra cost as you scale.
- Google Meet API: free for our usage tier.
- No per-booking cost.

For an MVP-stage team, $72/year. Negligible.

## Security notes

- The refresh token is the only secret in this integration. Treat it
  like a JWT signing secret — rotate by revoking + re-bootstrapping.
- Refresh tokens are bound to the OAuth client. Rotating the OAuth
  client invalidates the token; you'd need to redo step 3.
- The `meet-bot` Workspace user has access to nothing else — the
  scope we requested only grants `meetings.space.created`. The token
  cannot read mail, calendars, or any other Google service.
- If the `meet-bot` account is compromised, the attacker can mint
  Meet rooms but cannot do anything else. Revoke from the OAuth
  consent screen and rotate within an hour.

## Why no per-trainer Meet?

We considered it (mirror the Zoom-per-trainer flow) and decided
against it. The math:

- Operational complexity: 2× the OAuth pipelines, 2× the token
  storage, 2× the failure modes
- Trainer cost: each trainer would need their own Workspace seat ($6/mo) to
  authenticate via OAuth, OR use a free Gmail and accept that Google
  might revoke their token unpredictably
- User benefit: a Meet room hosted by `meet-bot@fitcall.me` is
  indistinguishable from one hosted by a specific trainer for the
  client's purposes — the trainer joins as a regular participant and
  proctors the call

The single-org-account model wins on every axis. The Zoom-per-trainer
work stays because Zoom's per-meeting concurrency cap forces it; Meet
has no such cap.
