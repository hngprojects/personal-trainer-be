// Package iap verifies Apple App Store and Google Play in-app purchase receipts.
// Set IAP_SKIP_VERIFICATION=true in development to bypass real network calls.
package iap

import (
	"bytes"
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
	OriginalTransactionID string    // Apple: original_transaction_id; Google: orderId
	ProductID             string    // e.g. com.fitcal.plan.committed.monthly
	PurchasedAt           time.Time
	ExpiresAt             time.Time
	IsTrialPeriod         bool
}

// ─── Apple ──────────────────────────────────────────────────────────────────

const (
	appleProductionURL = "https://buy.itunes.apple.com/verifyReceipt"
	appleSandboxURL    = "https://sandbox.itunes.apple.com/verifyReceipt"
)

type appleVerifyRequest struct {
	ReceiptData            string `json:"receipt-data"`
	Password               string `json:"password"`
	ExcludeOldTransactions bool   `json:"exclude-old-transactions"`
}

type appleReceiptInfo struct {
	OriginalTransactionID string `json:"original_transaction_id"`
	ProductID             string `json:"product_id"`
	PurchaseDateMS        string `json:"purchase_date_ms"`
	ExpiresDateMS         string `json:"expires_date_ms"`
	IsTrialPeriod         string `json:"is_trial_period"`
	CancellationDate      string `json:"cancellation_date"`
}

type appleVerifyResponse struct {
	Status            int                `json:"status"`
	LatestReceiptInfo []appleReceiptInfo `json:"latest_receipt_info"`
}

// VerifyApple validates a base64-encoded App Store receipt against Apple's
// servers and returns the most recent transaction for the given productID.
func VerifyApple(ctx context.Context, receiptData, sharedSecret, productID string) (*VerifiedPurchase, error) {
	result, err := appleVerify(ctx, appleProductionURL, receiptData, sharedSecret)
	if err != nil {
		return nil, err
	}
	// status 21007 means receipt is from sandbox — retry against sandbox
	if result.Status == 21007 {
		result, err = appleVerify(ctx, appleSandboxURL, receiptData, sharedSecret)
		if err != nil {
			return nil, err
		}
	}
	if result.Status != 0 {
		return nil, fmt.Errorf("apple receipt validation failed: status %d", result.Status)
	}

	var latest *appleReceiptInfo
	for i := range result.LatestReceiptInfo {
		info := &result.LatestReceiptInfo[i]
		if info.ProductID == productID && info.CancellationDate == "" {
			if latest == nil || msToTime(info.ExpiresDateMS).After(msToTime(latest.ExpiresDateMS)) {
				latest = info
			}
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no active receipt found for product %s", productID)
	}

	purchasedAt := msToTime(latest.PurchaseDateMS)
	expiresAt := msToTime(latest.ExpiresDateMS)

	return &VerifiedPurchase{
		OriginalTransactionID: latest.OriginalTransactionID,
		ProductID:             latest.ProductID,
		PurchasedAt:           purchasedAt,
		ExpiresAt:             expiresAt,
		IsTrialPeriod:         latest.IsTrialPeriod == "true",
	}, nil
}

func appleVerify(ctx context.Context, url, receiptData, sharedSecret string) (*appleVerifyResponse, error) {
	body, _ := json.Marshal(appleVerifyRequest{
		ReceiptData:            receiptData,
		Password:               sharedSecret,
		ExcludeOldTransactions: true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apple verify request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result appleVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("apple verify decode: %w", err)
	}
	return &result, nil
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
