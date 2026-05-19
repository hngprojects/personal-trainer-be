package stripe

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/paymentintent"
	"github.com/stripe/stripe-go/v76/refund"
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
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled:        stripe.Bool(true),
			AllowRedirects: stripe.String("never"),
		},
	}
	piParams.Context = ctx
	if params.IdempotencyKey != "" {
		piParams.IdempotencyKey = stripe.String(params.IdempotencyKey)
	}
	if params.Description != "" {
		piParams.Description = stripe.String(params.Description)
	}

	pi, err := paymentintent.New(piParams)
	if err != nil {
		return nil, fmt.Errorf("stripe charge failed: %w", err)
	}
	if pi.Status != stripe.PaymentIntentStatusSucceeded {
		return nil, fmt.Errorf("payment intent status %q is not succeeded", pi.Status)
	}

	return &ChargeResult{
		TransactionID: pi.ID,
		Status:        string(pi.Status),
	}, nil
}

func (c *Client) Refund(ctx context.Context, paymentIntentID string) error {
	params := &stripe.RefundParams{
		PaymentIntent: stripe.String(paymentIntentID),
	}
	params.Context = ctx
	_, err := refund.New(params)
	if err != nil {
		return fmt.Errorf("stripe refund failed: %w", err)
	}
	return nil
}
