-- +goose Up
ALTER TABLE bookings
    DROP CONSTRAINT IF EXISTS bookings_session_platform_check;

ALTER TABLE bookings
    ADD CONSTRAINT bookings_session_platform_check
    CHECK (
        session_platform IN (
            'zoom',
            'google_meet',
            'messenger',
            'imessage',
            'whatsapp'
        )
    );

-- +goose Down
ALTER TABLE bookings
    DROP CONSTRAINT IF EXISTS bookings_session_platform_check;

ALTER TABLE bookings
    ADD CONSTRAINT bookings_session_platform_check
    CHECK (
        session_platform IN (
            'zoom',
            'google_meet',
            'messenger'
        )
    );