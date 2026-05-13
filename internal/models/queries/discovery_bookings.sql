-- name: HasDiscoveryBookingForClientTrainer :one
SELECT EXISTS (
  SELECT 1
  FROM bookings
  WHERE trainer_id = sqlc.arg(trainer_id)
    AND client_id = sqlc.arg(client_id)
    AND is_discovery_call = true
) AS has_discovery_booking;

-- name: CreateDiscoveryBooking :one
INSERT INTO bookings (
  trainer_id,
  client_id,
  subscription_id,
  calendly_event_id,
  scheduled_start,
  scheduled_end,
  timezone,
  booking_status,
  session_platform,
  cancellation_reason,
  created_at,
  cancelled_at,
  is_discovery_call,
  meeting_join_url,
  meeting_start_url
) VALUES (
  sqlc.arg(trainer_id),
  sqlc.arg(client_id),
  NULL,
  sqlc.arg(calendly_event_id),
  sqlc.arg(scheduled_start),
  sqlc.arg(scheduled_end),
  sqlc.arg(timezone),
  sqlc.arg(booking_status),
  sqlc.arg(session_platform),
  sqlc.arg(cancellation_reason),
  sqlc.arg(created_at),
  sqlc.arg(cancelled_at),
  true,
  sqlc.arg(meeting_join_url),
  sqlc.arg(meeting_start_url)
)
RETURNING
  id,
  trainer_id,
  client_id,
  subscription_id,
  calendly_event_id,
  scheduled_start,
  scheduled_end,
  timezone,
  booking_status,
  session_platform,
  cancellation_reason,
  created_at,
  cancelled_at,
  is_discovery_call,
  meeting_join_url,
  meeting_start_url;
