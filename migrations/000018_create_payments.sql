-- +goose Up
CREATE TABLE payments (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id             UUID REFERENCES bookings(id),
    subscription_id        UUID REFERENCES subscriptions(id),
    payer_id               UUID NOT NULL REFERENCES users(id),
    provider               VARCHAR NOT NULL,
    provider_transaction_id VARCHAR,
    idempotency_key        VARCHAR NOT NULL UNIQUE,
    currency               VARCHAR NOT NULL,
    total_amount           BIGINT NOT NULL,
    trainer_earning        BIGINT NOT NULL,
    platform_fee           BIGINT NOT NULL,
    payment_status         VARCHAR NOT NULL,
    paid_at                TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Constraint for mutual exclusivity
    CONSTRAINT chk_payment_source CHECK (
        (booking_id IS NULL AND subscription_id IS NOT NULL) OR 
        (booking_id IS NOT NULL AND subscription_id IS NULL)
    )
);

-- +goose Down
DROP TABLE IF EXISTS payments;