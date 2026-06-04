// Package activities surfaces a chronological feed of high-signal
// events for two consumers:
//
//   - trainers themselves (GET /trainers/me/activities) — only events
//     that touched their account: bookings, reschedules, cancellations,
//     reviews, discovery calls assigned to them.
//   - admins (GET /admin/activities) — the same event types across
//     every trainer in the system.
//
// Events are NOT written to a dedicated event table. They're derived
// at read time from the source-of-truth tables (bookings,
// discovery_bookings, *_reschedule_history, reviews) via UNION ALL.
// That keeps every other write path in the codebase untouched and
// guarantees the feed can never drift from the actual state.
// Trade-off: heavier query. Acceptable at current scale; a write-time
// activity log is the obvious follow-up if this ever shows up in p99.
package activities

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ActivityType is the discriminator both clients filter on and the
// summary template keys off. Adding a new type means: a new branch in
// the UNION query + a summary-template entry + (probably) a new
// constant here.
type ActivityType string

const (
	BookingCreated       ActivityType = "booking_created"
	BookingCancelled     ActivityType = "booking_cancelled"
	BookingRescheduled   ActivityType = "booking_rescheduled"
	DiscoveryBooked      ActivityType = "discovery_booked"
	DiscoveryRescheduled ActivityType = "discovery_rescheduled"
	ReviewReceived       ActivityType = "review_received"
)

// Activity is the wire-format row returned in the feed. Keep optional
// fields as pointers so an absent value omits the JSON key instead of
// returning a zero placeholder that the client would have to filter.
type Activity struct {
	ID         uuid.UUID    `json:"id"`
	Type       ActivityType `json:"type"`
	OccurredAt time.Time    `json:"occurred_at"`
	TargetID   uuid.UUID    `json:"target_id"`
	TargetType string       `json:"target_type"`
	// Actor is the person who caused the event (the client who booked,
	// the user who reviewed). Nil for synthetic events without a clear
	// human actor.
	Actor *Actor `json:"actor,omitempty"`
	// Trainer is populated only in the admin-scope feed; in the
	// trainer-scope feed the trainer is implicit (the caller).
	Trainer *TrainerRef `json:"trainer,omitempty"`
	// EventTime is when the underlying event refers to (e.g. the
	// scheduled_start of the booked session) — distinct from OccurredAt
	// (when the event itself happened). Used by the mobile app to show
	// "booked for Tuesday at 9am" rather than "booked 3 minutes ago".
	EventTime *time.Time `json:"event_time,omitempty"`
	// Extra is a free-text payload whose meaning depends on Type —
	// review rating, cancellation reason, booking_status, etc. Plain
	// string instead of map[string]any to keep the wire format flat
	// and Postgres-trivial to project from the UNION.
	Extra string `json:"extra,omitempty"`
	// Summary is a server-rendered one-liner suitable for direct
	// display. Built in BuildSummary so the wording stays consistent
	// across clients and i18n can move server-side later.
	Summary string `json:"summary"`
}

type Actor struct {
	UserID *uuid.UUID `json:"user_id,omitempty"`
	Name   string     `json:"name"`
}

type TrainerRef struct {
	TrainerID uuid.UUID `json:"trainer_id"`
	UserID    uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
}

// Cursor is the (occurred_at, activity_id) pair the next-page query
// uses as a strict-less-than predicate. Pairing the id breaks ties
// when two activities share a microsecond.
type Cursor struct {
	OccurredAt time.Time
	ActivityID uuid.UUID
}

// Encode produces an opaque base64url token the client roundtrips.
// Format inside the encoding is intentionally NOT documented to
// clients so we can change it without breaking them.
func (c Cursor) Encode() string {
	raw := c.OccurredAt.Format(time.RFC3339Nano) + "|" + c.ActivityID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a token previously produced by Cursor.Encode.
// Returns a non-nil error on any parse failure so the handler can 400
// instead of silently swallowing the token and returning page 1.
func DecodeCursor(raw string) (Cursor, error) {
	if raw == "" {
		return Cursor{}, errors.New("empty cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, err
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return Cursor{}, errors.New("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return Cursor{}, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Cursor{}, err
	}
	return Cursor{OccurredAt: t, ActivityID: id}, nil
}

// ListResponse is the envelope returned by both endpoints. NextCursor
// is empty when there are no more pages, so clients can stop polling
// by checking string emptiness rather than comparing item-count to
// page-size (which would race when an event arrives mid-pagination).
type ListResponse struct {
	Items      []Activity `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}
