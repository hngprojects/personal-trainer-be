package stripe

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/paymentintent"
)

type Client struct {
	secretKey string
}

func New(secretKey string) *Client {
	stripe.Key = secretKey
	return &Client{secretKey: secretKey}
}

type ChargeParams struct {
	Amount          int64
	Currency        string
	PaymentMethodID string
	IdempotencyKey  string
	Description     string
}

type ChargeResult struct {
	TransactionID string
	Status        string
}

func (c *Client) Charge(ctx context.Context, params ChargeParams) (*ChargeResult, error) {
	piParams := &stripe.PaymentIntentParams{
		Amount:        stripe.Int64(params.Amount),
		Currency:      stripe.String(params.Currency),
		PaymentMethod: stripe.String(params.PaymentMethodID),
		Confirm:       stripe.Bool(true),
	}
	piParams.Params.Context = ctx
	if params.IdempotencyKey != "" {
		piParams.Params.IdempotencyKey = stripe.String(params.IdempotencyKey)
	}

	pi, err := paymentintent.New(piParams)
	if err != nil {
		return nil, fmt.Errorf("stripe charge failed: %w", err)
	}

	return &ChargeResult{
		TransactionID: pi.ID,
		Status:        string(pi.Status),
	}, nil
}
