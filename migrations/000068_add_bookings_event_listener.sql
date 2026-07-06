-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_trainer_bookings_change()
RETURNS trigger AS $$
BEGIN
  PERFORM pg_notify(
    'trainer_bookings_events',
    row_to_json(NEW)::text
  );
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trainer_bookings_insert
AFTER INSERT ON bookings
FOR EACH ROW EXECUTE FUNCTION notify_trainer_bookings_change();


-- +goose Down
DROP TRIGGER IF EXISTS trainer_bookings_insert
ON bookings;

DROP FUNCTION IF EXISTS notify_trainer_bookings_change();