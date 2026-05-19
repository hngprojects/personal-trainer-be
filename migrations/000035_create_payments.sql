-- +goose Up
CREATE TABLE IF NOT EXISTS payments (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id             UUID REFERENCES bookings(id),
    subscription_id        UUID REFERENCES subscriptions(id),
    payer_id               UUID NOT NULL REFERENCES users(id),
    payment_type VARCHAR NOT NULL,
    provider               VARCHAR NOT NULL,
    provider_transaction_id VARCHAR,
    idempotency_key        VARCHAR NOT NULL UNIQUE,
    currency               VARCHAR NOT NULL,
    total_amount           BIGINT NOT NULL,
    trainer_earning        BIGINT NOT NULL,
    platform_fee           BIGINT NOT NULL,
    CONSTRAINT chk_amounts_non_negative CHECK (total_amount >= 0 AND trainer_earning >= 0 AND platform_fee >= 0),
    CONSTRAINT chk_payment_split CHECK (trainer_earning + platform_fee = total_amount),
    payment_status         VARCHAR NOT NULL,
    paid_at                TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Constraint for mutual exclusivity
    CONSTRAINT chk_payment_source CHECK (
        (booking_id IS NULL AND subscription_id IS NOT NULL) OR 
        (booking_id IS NOT NULL AND subscription_id IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_payments_booking_id ON payments(booking_id) WHERE booking_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_payments_subscription_id ON payments(subscription_id) WHERE subscription_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_payments_payer_id ON payments(payer_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(payment_status);

-- +goose Down
DROP TABLE IF EXISTS payments;
