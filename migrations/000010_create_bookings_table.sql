-- +goose Up
CREATE TABLE IF NOT EXISTS bookings (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id     UUID        NOT NULL REFERENCES trainers(id) ON DELETE CASCADE,
    client_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status         TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'confirmed', 'completed', 'cancelled')),
    completed_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT bookings_completed_at_status_check
        CHECK (
            (status = 'completed' AND completed_at IS NOT NULL) OR
            (status <> 'completed' AND completed_at IS NULL)
        ),
    CONSTRAINT bookings_id_trainer_client_key
        UNIQUE (id, trainer_id, client_user_id)
);

CREATE INDEX IF NOT EXISTS idx_bookings_client_user_id ON bookings(client_user_id);
CREATE INDEX IF NOT EXISTS idx_bookings_trainer_id ON bookings(trainer_id);
CREATE INDEX IF NOT EXISTS idx_bookings_status ON bookings(status);

-- +goose Down
DROP INDEX IF EXISTS idx_bookings_status;
DROP INDEX IF EXISTS idx_bookings_trainer_id;
DROP INDEX IF EXISTS idx_bookings_client_user_id;
DROP TABLE IF EXISTS bookings;
