-- name: GetBookingSlots :many
SELECT
    id,
    trainer_id,
    day_of_week,
    start_time,
    end_time,
    timezone,
    is_active,
    created_at,
    updated_at
FROM booking_slots;

-- name: GetTrainersBookingSlots :many
-- Returns bookable slots for a trainer. Joins trainers to respect the global
-- is_available toggle — when a trainer sets themselves unavailable, clients
-- see an empty schedule without the underlying slots being deleted.
SELECT
    bs.id,
    bs.day_of_week,
    bs.start_time,
    bs.end_time,
    bs.timezone,
    bs.is_active,
    bs.created_at,
    bs.updated_at
FROM booking_slots bs
JOIN trainers t ON t.id = bs.trainer_id
WHERE bs.trainer_id = $1
AND t.is_available = true;

-- name: GetTrainersBookingSlotsForDate :many
-- Same as GetTrainersBookingSlots but filtered to a specific date and with
-- any slot already booked on that date removed from the result. "Booked"
-- means EITHER:
--   - a paid booking (bookings.scheduled_start/end overlaps the slot window),
--   - or a discovery booking on the same trainer overlapping the window
--     (discovery calls are fixed 30 minutes).
-- Cancelled and completed rows are ignored so a cancelled slot frees up.
--
-- Timezone normalisation: bs.start_time / bs.end_time are local TIME values
-- in the slot's own timezone (bs.timezone). To compare them to booking
-- timestamps we MUST convert the booking's timestamptz to that same slot
-- timezone — converting to the booking's own timezone instead would
-- compare apples to oranges and miss/double-flag overlaps when client and
-- slot timezones differ. The COALESCE picks the slot's tz, falling back
-- to UTC if (theoretically) unset.
SELECT
    bs.id,
    bs.day_of_week,
    bs.start_time,
    bs.end_time,
    bs.timezone,
    bs.is_active,
    bs.created_at,
    bs.updated_at
FROM booking_slots bs
JOIN trainers t ON t.id = bs.trainer_id
WHERE bs.trainer_id = $1
  AND t.is_available = true
  AND bs.is_active = true
  AND bs.day_of_week = EXTRACT(DOW FROM sqlc.arg(target_date)::DATE)::INT
  AND NOT EXISTS (
      SELECT 1 FROM bookings b
      WHERE b.trainer_id = bs.trainer_id
        AND b.booking_status NOT IN ('cancelled', 'completed')
        AND (b.scheduled_start AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::DATE = sqlc.arg(target_date)::DATE
        AND (b.scheduled_start AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::TIME < bs.end_time
        AND (b.scheduled_end   AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::TIME > bs.start_time
  )
  AND NOT EXISTS (
      SELECT 1 FROM discovery_bookings db
      WHERE db.trainer_id = bs.trainer_id
        AND db.status NOT IN ('cancelled', 'completed')
        AND (db.selected_datetime                       AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::DATE = sqlc.arg(target_date)::DATE
        AND (db.selected_datetime                       AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::TIME < bs.end_time
        AND ((db.selected_datetime + INTERVAL '30 minutes') AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::TIME > bs.start_time
  );
