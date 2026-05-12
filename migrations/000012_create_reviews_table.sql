-- +goose Up
CREATE TABLE IF NOT EXISTS reviews (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id     UUID        NOT NULL UNIQUE,
    trainer_id     UUID        NOT NULL,
    client_user_id UUID        NOT NULL,
    rating         INT         NOT NULL CHECK (rating BETWEEN 1 AND 5),
    review         TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT reviews_booking_id_fkey
        FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE,
    CONSTRAINT reviews_trainer_id_fkey
        FOREIGN KEY (trainer_id) REFERENCES trainers(id) ON DELETE CASCADE,
    CONSTRAINT reviews_client_user_id_fkey
        FOREIGN KEY (client_user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_reviews_trainer_created_id
    ON reviews(trainer_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_reviews_client_user_id ON reviews(client_user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_reviews_client_user_id;
DROP INDEX IF EXISTS idx_reviews_trainer_created_id;
DROP TABLE IF EXISTS reviews;
