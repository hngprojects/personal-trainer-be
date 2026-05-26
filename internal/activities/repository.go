package activities

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Repository is the data-access seam — postgresRepo for production,
// fakeRepo in tests. The cursor argument is nil for the first page;
// implementations must page strictly descending on (occurred_at, id).
type Repository interface {
	ListForTrainer(ctx context.Context, trainerUserID uuid.UUID, cursor *Cursor, limit int) ([]Activity, error)
	ListAll(ctx context.Context, cursor *Cursor, limit int) ([]Activity, error)
}

func NewPostgresRepo(db *sql.DB) Repository {
	return &postgresRepo{db: db}
}

type postgresRepo struct {
	db *sql.DB
}

// rawRow mirrors the projected columns of the UNION below. Kept
// internal because the consumer wants Activity, not this shape.
type rawRow struct {
	activityType string
	occurredAt   time.Time
	activityID   uuid.UUID
	targetID     uuid.UUID
	targetType   string
	actorUserID  uuid.NullUUID
	actorName    sql.NullString
	eventTime    sql.NullTime
	extra        sql.NullString
	// Admin-only fields. Always selected in the admin query and left
	// zero in the trainer query so a single rawRow can serve both.
	trainerID       uuid.NullUUID
	trainerUserID   uuid.NullUUID
	trainerUserName sql.NullString
}

// activityUnionTrainerScope is the heart of the feature: one SELECT
// per event source, glued with UNION ALL, filtered to a single
// trainer via a CTE join. Add a new event source = add a UNION ALL
// branch.
//
// Notes for the next person editing this:
//   - Every branch MUST project the SAME column types in the SAME
//     order, or Postgres errors. Use explicit ::text / ::timestamptz
//     casts on literals so the planner doesn't infer surprising types.
//   - extra is a free-text payload whose meaning depends on
//     activity_type. Keep it short; it lands directly in JSON.
//   - The pagination predicate uses (occurred_at < $2) OR
//     (occurred_at = $2 AND activity_id < $3). Pair predicate breaks
//     ties on the same timestamp without losing rows.
const activityUnionTrainerScope = `
WITH t AS (
    SELECT id FROM trainers WHERE user_id = $1
)
SELECT activity_type, occurred_at, activity_id, target_id, target_type,
       actor_user_id, actor_name, event_time, extra
FROM (
    -- Paid booking created (trainer was booked by a client).
    SELECT 'booking_created'::text AS activity_type,
           b.created_at            AS occurred_at,
           b.id                    AS activity_id,
           b.id                    AS target_id,
           'booking'::text         AS target_type,
           u.id                    AS actor_user_id,
           u.name                  AS actor_name,
           b.scheduled_start       AS event_time,
           COALESCE(b.booking_status, '') AS extra
    FROM bookings b
    JOIN t ON b.trainer_id = t.id
    JOIN users u ON u.id = b.client_id
    WHERE b.created_at IS NOT NULL

    UNION ALL

    -- Paid booking cancelled. cancelled_at is the event time; the row
    -- is still in bookings but flagged cancelled.
    SELECT 'booking_cancelled'::text, b.cancelled_at, b.id, b.id, 'booking'::text,
           u.id, u.name, b.scheduled_start,
           COALESCE(b.cancellation_reason, '')
    FROM bookings b
    JOIN t ON b.trainer_id = t.id
    JOIN users u ON u.id = b.client_id
    WHERE b.cancelled_at IS NOT NULL

    UNION ALL

    -- Paid booking rescheduled. One row per history entry, so a
    -- thrice-rescheduled booking produces three activity events.
    SELECT 'booking_rescheduled'::text, h.created_at, h.id, b.id, 'booking'::text,
           u.id, u.name, h.new_start, COALESCE(h.reason, '')
    FROM paid_booking_reschedule_history h
    JOIN bookings b ON b.id = h.booking_id
    JOIN t ON b.trainer_id = t.id
    JOIN users u ON u.id = b.client_id

    UNION ALL

    -- Discovery call booked and assigned to this trainer. trainer_id
    -- on discovery_bookings can be null until matched, so the join
    -- naturally filters out unassigned calls.
    SELECT 'discovery_booked'::text, d.created_at, d.id, d.id, 'discovery_booking'::text,
           NULL::uuid, d.name, d.selected_datetime, COALESCE(d.contact_mode, '')
    FROM discovery_bookings d
    JOIN t ON d.trainer_id = t.id

    UNION ALL

    -- Discovery call rescheduled. booking_reschedule_history rows
    -- reference discovery_bookings.id via discovery_booking_id.
    SELECT 'discovery_rescheduled'::text, h.created_at, h.id, d.id, 'discovery_booking'::text,
           NULL::uuid, d.name, h.new_datetime, COALESCE(h.reason, '')
    FROM booking_reschedule_history h
    JOIN discovery_bookings d ON d.id = h.discovery_booking_id
    JOIN t ON d.trainer_id = t.id

    UNION ALL

    -- Review received. Rating is projected into extra as a single
    -- digit so the summary template can drop it in without a join.
    SELECT 'review_received'::text, r.created_at, r.id, r.id, 'review'::text,
           u.id, u.name, NULL::timestamptz, r.rating::text
    FROM reviews r
    JOIN t ON r.trainer_id = t.id
    JOIN users u ON u.id = r.client_user_id
) x
WHERE ($2::timestamptz IS NULL
       OR x.occurred_at < $2
       OR (x.occurred_at = $2 AND x.activity_id < $3::uuid))
ORDER BY x.occurred_at DESC, x.activity_id DESC
LIMIT $4
`

// activityUnionAdminScope mirrors the trainer query but drops the
// trainer-filter join and adds (trainer_id, trainer_user_id,
// trainer_user_name) to the projection. Two near-duplicate queries
// rather than a runtime-toggled WHERE because the planner produces
// noticeably better plans when the trainer filter is a CTE join vs
// an OR-able predicate.
const activityUnionAdminScope = `
SELECT activity_type, occurred_at, activity_id, target_id, target_type,
       actor_user_id, actor_name, event_time, extra,
       trainer_id, trainer_user_id, trainer_user_name
FROM (
    SELECT 'booking_created'::text AS activity_type,
           b.created_at AS occurred_at, b.id AS activity_id,
           b.id AS target_id, 'booking'::text AS target_type,
           u.id AS actor_user_id, u.name AS actor_name,
           b.scheduled_start AS event_time,
           COALESCE(b.booking_status, '') AS extra,
           t.id AS trainer_id, tu.id AS trainer_user_id, tu.name AS trainer_user_name
    FROM bookings b
    JOIN trainers t ON t.id = b.trainer_id
    JOIN users tu ON tu.id = t.user_id
    JOIN users u ON u.id = b.client_id
    WHERE b.created_at IS NOT NULL

    UNION ALL

    SELECT 'booking_cancelled'::text, b.cancelled_at, b.id, b.id, 'booking'::text,
           u.id, u.name, b.scheduled_start, COALESCE(b.cancellation_reason, ''),
           t.id, tu.id, tu.name
    FROM bookings b
    JOIN trainers t ON t.id = b.trainer_id
    JOIN users tu ON tu.id = t.user_id
    JOIN users u ON u.id = b.client_id
    WHERE b.cancelled_at IS NOT NULL

    UNION ALL

    SELECT 'booking_rescheduled'::text, h.created_at, h.id, b.id, 'booking'::text,
           u.id, u.name, h.new_start, COALESCE(h.reason, ''),
           t.id, tu.id, tu.name
    FROM paid_booking_reschedule_history h
    JOIN bookings b ON b.id = h.booking_id
    JOIN trainers t ON t.id = b.trainer_id
    JOIN users tu ON tu.id = t.user_id
    JOIN users u ON u.id = b.client_id

    UNION ALL

    SELECT 'discovery_booked'::text, d.created_at, d.id, d.id, 'discovery_booking'::text,
           NULL::uuid, d.name, d.selected_datetime, COALESCE(d.contact_mode, ''),
           t.id, tu.id, tu.name
    FROM discovery_bookings d
    JOIN trainers t ON t.id = d.trainer_id
    JOIN users tu ON tu.id = t.user_id

    UNION ALL

    SELECT 'discovery_rescheduled'::text, h.created_at, h.id, d.id, 'discovery_booking'::text,
           NULL::uuid, d.name, h.new_datetime, COALESCE(h.reason, ''),
           t.id, tu.id, tu.name
    FROM booking_reschedule_history h
    JOIN discovery_bookings d ON d.id = h.discovery_booking_id
    JOIN trainers t ON t.id = d.trainer_id
    JOIN users tu ON tu.id = t.user_id

    UNION ALL

    SELECT 'review_received'::text, r.created_at, r.id, r.id, 'review'::text,
           u.id, u.name, NULL::timestamptz, r.rating::text,
           t.id, tu.id, tu.name
    FROM reviews r
    JOIN trainers t ON t.id = r.trainer_id
    JOIN users tu ON tu.id = t.user_id
    JOIN users u ON u.id = r.client_user_id
) x
WHERE ($1::timestamptz IS NULL
       OR x.occurred_at < $1
       OR (x.occurred_at = $1 AND x.activity_id < $2::uuid))
ORDER BY x.occurred_at DESC, x.activity_id DESC
LIMIT $3
`

func (r *postgresRepo) ListForTrainer(ctx context.Context, trainerUserID uuid.UUID, cursor *Cursor, limit int) ([]Activity, error) {
	cursorTime, cursorID := splitCursor(cursor)
	rows, err := r.db.QueryContext(ctx, activityUnionTrainerScope, trainerUserID, cursorTime, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("activities trainer-scope query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRows(rows, false)
}

func (r *postgresRepo) ListAll(ctx context.Context, cursor *Cursor, limit int) ([]Activity, error) {
	cursorTime, cursorID := splitCursor(cursor)
	rows, err := r.db.QueryContext(ctx, activityUnionAdminScope, cursorTime, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("activities admin-scope query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRows(rows, true)
}

// splitCursor expands an optional cursor into the two query
// parameters. Passing NullTime / NullUUID with Valid=false lets the
// SQL `$N::timestamptz IS NULL` branch fire and skip the predicate
// entirely on page 1.
func splitCursor(c *Cursor) (sql.NullTime, uuid.NullUUID) {
	if c == nil {
		return sql.NullTime{}, uuid.NullUUID{}
	}
	return sql.NullTime{Time: c.OccurredAt, Valid: true},
		uuid.NullUUID{UUID: c.ActivityID, Valid: true}
}

func scanRows(rows *sql.Rows, adminScope bool) ([]Activity, error) {
	out := make([]Activity, 0, 32)
	for rows.Next() {
		var r rawRow
		var err error
		if adminScope {
			err = rows.Scan(
				&r.activityType, &r.occurredAt, &r.activityID, &r.targetID, &r.targetType,
				&r.actorUserID, &r.actorName, &r.eventTime, &r.extra,
				&r.trainerID, &r.trainerUserID, &r.trainerUserName,
			)
		} else {
			err = rows.Scan(
				&r.activityType, &r.occurredAt, &r.activityID, &r.targetID, &r.targetType,
				&r.actorUserID, &r.actorName, &r.eventTime, &r.extra,
			)
		}
		if err != nil {
			return nil, fmt.Errorf("scan activity row: %w", err)
		}
		out = append(out, rowToActivity(r))
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}

// rowToActivity translates the flat SQL projection into the wire
// shape, including running BuildSummary so handlers don't have to
// remember to call it.
func rowToActivity(r rawRow) Activity {
	a := Activity{
		ID:         r.activityID,
		Type:       ActivityType(r.activityType),
		OccurredAt: r.occurredAt,
		TargetID:   r.targetID,
		TargetType: r.targetType,
		Extra:      r.extra.String,
	}
	if r.actorName.Valid && r.actorName.String != "" {
		actor := &Actor{Name: r.actorName.String}
		if r.actorUserID.Valid {
			id := r.actorUserID.UUID
			actor.UserID = &id
		}
		a.Actor = actor
	}
	if r.eventTime.Valid {
		t := r.eventTime.Time
		a.EventTime = &t
	}
	if r.trainerID.Valid && r.trainerUserID.Valid {
		a.Trainer = &TrainerRef{
			TrainerID: r.trainerID.UUID,
			UserID:    r.trainerUserID.UUID,
			Name:      r.trainerUserName.String,
		}
	}
	a.Summary = BuildSummary(a)
	return a
}
