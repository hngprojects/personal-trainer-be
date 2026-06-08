// Package iap verifies Apple App Store and Google Play in-app purchase receipts.
// Set IAP_SKIP_VERIFICATION=true in development to bypass real network calls.
//
// Apple verification uses StoreKit 2's signed JWS transactions
// (Transaction.jsonRepresentation on iOS) — see apple_storekit2.go.
// The legacy verifyReceipt endpoint is no longer wired up; it remained
// usable but Apple deprecated it in iOS 18 and StoreKit 2 has been
// the documented path since 2021.
package iap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// VerifiedPurchase is the normalised result returned after a successful
// verification with either Apple or Google.
type VerifiedPurchase struct {
	OriginalTransactionID string // Apple: originalTransactionId; Google: orderId
	ProductID             string // e.g. com.fitcal.plan.committed.monthly
	PurchasedAt           time.Time
	ExpiresAt             time.Time
	IsTrialPeriod         bool
}

// ─── Apple ──────────────────────────────────────────────────────────────────

// VerifyApple validates a StoreKit 2 signed transaction. The mobile
// client obtains the JWS via `Transaction.jsonRepresentation` after a
// successful StoreKit 2 purchase and sends it to us verbatim.
//
// expectedEnv is "production", "sandbox", or empty (accept either).
// Passing the empty string is appropriate for staging where TestFlight
// can produce either; production deploys should pin to "production"
// so a leaked sandbox transaction can't unlock entitlements.
func VerifyApple(_ context.Context, signedTransaction, expectedBundleID, expectedProductID, expectedEnv string) (*VerifiedPurchase, error) {
	return VerifyAppleSignedTransaction(signedTransaction, expectedBundleID, expectedProductID, expectedEnv)
}

// ─── Google ─────────────────────────────────────────────────────────────────

type googleSubscriptionResponse struct {
	StartTimeMillis  string `json:"startTimeMillis"`
	ExpiryTimeMillis string `json:"expiryTimeMillis"`
	PaymentState     *int   `json:"paymentState"`
	OrderID          string `json:"orderId"`
	// 0 = not acknowledged, 1 = acknowledged
	AcknowledgementState int `json:"acknowledgementState"`
}

// VerifyGoogle validates a Google Play purchase token using the Play Developer
// API. serviceAccountJSON is the full contents of the service account key file.
func VerifyGoogle(ctx context.Context, packageName, subscriptionID, purchaseToken, serviceAccountJSON string) (*VerifiedPurchase, error) {
	token, err := googleAccessToken(ctx, serviceAccountJSON)
	if err != nil {
		return nil, fmt.Errorf("google auth: %w", err)
	}

	url := fmt.Sprintf(
		"https://androidpublisher.googleapis.com/androidpublisher/v3/applications/%s/purchases/subscriptions/%s/tokens/%s",
		packageName, subscriptionID, purchaseToken,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google verify request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google play API returned %d", resp.StatusCode)
	}

	var sub googleSubscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return nil, fmt.Errorf("google verify decode: %w", err)
	}

	// paymentState: 0 = payment pending, 1 = payment received, 2 = free trial
	if sub.PaymentState == nil {
		return nil, fmt.Errorf("google purchase token is invalid or expired")
	}
	if *sub.PaymentState == 0 {
		return nil, fmt.Errorf("google subscription payment is still pending")
	}
	isTrial := *sub.PaymentState == 2

	return &VerifiedPurchase{
		OriginalTransactionID: sub.OrderID,
		ProductID:             subscriptionID,
		PurchasedAt:           msToTime(sub.StartTimeMillis),
		ExpiresAt:             msToTime(sub.ExpiryTimeMillis),
		IsTrialPeriod:         isTrial,
	}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func msToTime(ms string) time.Time {
	if ms == "" {
		return time.Time{}
	}
	var n int64
	if _, err := fmt.Sscanf(ms, "%d", &n); err != nil {
		return time.Time{}
	}
	return time.UnixMilli(n).UTC()
}
