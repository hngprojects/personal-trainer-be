# Recent Activities Endpoints

Two endpoints surface a chronological feed of events. They share the same data shape and pagination — only the scope differs.

| Endpoint                          | Auth                                     | Scope                                              |
| --------------------------------- | ---------------------------------------- | -------------------------------------------------- |
| `GET /api/v1/trainers/me/activities` | Bearer (must be a `trainer`)             | Events that touched the caller's trainer account.  |
| `GET /api/v1/admin/activities`       | Bearer (admin or super_admin)            | Same event types, system-wide. Includes `trainer`. |

## Event types

The feed is derived at read time from the source-of-truth tables — there is no separate event log to keep in sync.

| `type`                  | Source table                       | When emitted                                 |
| ----------------------- | ---------------------------------- | -------------------------------------------- |
| `booking_created`       | `bookings`                         | A paid session was booked with the trainer. |
| `booking_cancelled`     | `bookings`                         | `cancelled_at` was set.                      |
| `booking_rescheduled`   | `paid_booking_reschedule_history`  | One row per reschedule of a paid session.    |
| `discovery_booked`      | `discovery_bookings`               | A discovery call was assigned to the trainer.|
| `discovery_rescheduled` | `booking_reschedule_history`       | Discovery call moved.                        |
| `review_received`       | `reviews`                          | Client left a review.                        |

Adding a new type means: a new branch in the UNION query in `internal/activities/repository.go` + a summary entry in `summary.go` + a new `ActivityType` constant. No migration, no instrumentation of write paths.

## Query parameters

| Param    | Type     | Default | Notes                                                                                          |
| -------- | -------- | ------- | ---------------------------------------------------------------------------------------------- |
| `limit`  | int      | 20      | Clamped at 100. Bad value → 400.                                                               |
| `cursor` | string   | unset   | Opaque token from a previous response's `next_cursor`. Malformed → 400. Empty → first page.    |

## Response shape

```json
{
  "status": "success",
  "code": "OK",
  "message": "ok",
  "data": {
    "items": [
      {
        "id": "<activity uuid>",
        "type": "booking_created",
        "occurred_at": "2026-05-26T09:15:23.521Z",
        "target_id": "<booking uuid>",
        "target_type": "booking",
        "actor": {
          "user_id": "<client user uuid>",
          "name": "Jane Doe"
        },
        "trainer": {
          "trainer_id": "<trainer record uuid>",
          "user_id": "<trainer user uuid>",
          "name": "Trainer Mike"
        },
        "event_time": "2026-05-28T15:00:00Z",
        "extra": "confirmed",
        "summary": "Jane Doe booked a session with Trainer Mike on Thu, May 28 3:00 PM UTC"
      }
    ],
    "next_cursor": "MjAyNi0wNS0yNlQwOToxMjowMC4wMDBafGFiYy0xMjMt..."
  }
}
```

- `actor` is omitted (key absent) when the event has no human actor.
- `trainer` is omitted in the `/trainers/me/activities` feed — the trainer is implicit (the caller).
- `event_time` is the time the event refers to (e.g. the scheduled session start), distinct from `occurred_at` (when the event itself happened).
- `extra` is a free-text payload whose meaning depends on `type` — rating digit for reviews, cancellation reason, booking status, contact mode, etc.
- `summary` is server-rendered; safe to display verbatim.

## Pagination

Cursor-based. Pass back `data.next_cursor` from each response as the next request's `?cursor=`. Stop when `next_cursor` is the empty string or absent.

The cursor is opaque. Don't parse it on the client — its internal format may change. The server's strict-less-than predicate uses `(occurred_at, activity_id)` so a busy minute that overflows a page doesn't drop or duplicate any row.

## Auth notes

- The trainer endpoint 403s for non-trainer callers rather than returning an empty feed — keeps "no activity yet" distinct from "you're using the wrong account."
- The admin endpoint is gated by `SuperAdminOnly` middleware. Plain `admin` role is permitted via the `adminReadablePaths` allowlist in `internal/middleware/admin_only.go`.
