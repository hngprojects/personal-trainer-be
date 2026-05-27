-- +goose Up
ALTER TABLE notification 
    DROP CONSTRAINT IF EXISTS notification_type_check,
    ADD CONSTRAINT notification_type_check
        CHECK (type IN ('sms', 'push', 'email', 'realtime'));

-- +goose Down
ALTER TABLE notification
    DROP CONSTRAINT IF EXISTS notification_type_check,
    ADD CONSTRAINT notification_type_check
        CHECK (type IN ('sms', 'push', 'email'));