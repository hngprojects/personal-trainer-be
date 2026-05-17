-- name: CreateBookingSession :one
INSERT INTO booking_session (
    booking_id,
    actual_start
) VALUES (
    $1,
    $2
)
RETURNING
id,
booking_id,
actual_start,
actual_end,
trainer_joined,
client_joined,
status,
trainer_notes,
created_at
;

-- name: GetBookingSessionById :one
SELECT
    id,
    booking_id,
    actual_start,
    actual_end,
    trainer_joined,
    client_joined,
    status,
    trainer_notes,
    created_at
FROM booking_session
WHERE id = $1
LIMIT 1;

-- name: GetBookingSessionByBookingID :one
SELECT
    id,
    booking_id,
    actual_start,
    actual_end,
    trainer_joined,
    client_joined,
    status,
    trainer_notes,
    created_at
FROM booking_session
WHERE booking_id = $1
LIMIT 1;

-- name: DeleteBookingSessionByID :exec
DELETE FROM booking_session
WHERE id = $1;

-- name: MarkSessionAsStarted :one
UPDATE booking_session
SET
    actual_start=$1,
    trainer_joined=$2,
    status=$3
WHERE id=$4
AND status='booked'
RETURNING
    id,
    booking_id,
    actual_start,
    actual_end,
    trainer_joined,
    client_joined,
    status,
    trainer_notes,
    created_at
;

-- name: MarkSessionAsJoined :one
UPDATE booking_session
SET
    client_joined=$1,
    status=$2
WHERE id=$3
AND status='started'
RETURNING
    id,
    booking_id,
    actual_start,
    actual_end,
    trainer_joined,
    client_joined,
    status,
    trainer_notes,
    created_at
;

-- name: MarkSessionAsCompleted :one
UPDATE booking_session
SET
    actual_end=$1,
    status=$2
WHERE id=$3
AND status IN ('started', 'in-session')
RETURNING
    id,
    booking_id,
    actual_start,
    actual_end,
    trainer_joined,
    client_joined,
    status,
    trainer_notes,
    created_at
;

-- name: CollectTrainersNote :one
UPDATE booking_session
SET
    trainer_notes=$1
WHERE id=$2
AND status='completed'
RETURNING
    id,
    booking_id,
    actual_start,
    actual_end,
    trainer_joined,
    client_joined,
    status,
    trainer_notes,
    created_at
;
