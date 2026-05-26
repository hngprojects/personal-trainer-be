package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const baseURL = "http://localhost:8080/api/v1"

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run scripts/webhook_sim.go <platform> <id>")
		fmt.Println("  platform: apple | google")
		fmt.Println("  id:       apple_original_transaction_id or google_purchase_token")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  go run scripts/webhook_sim.go apple dev-262f299b-38a9-4a7e-ae97-64055969dd5f")
		fmt.Println("  go run scripts/webhook_sim.go google test-purchase-token-abc123")
		os.Exit(1)
	}

	platform := os.Args[1]
	id := os.Args[2]
	notifType := "EXPIRED"
	if len(os.Args) >= 4 {
		notifType = os.Args[3]
	}

	switch platform {
	case "apple":
		testApple(id, notifType)
	case "google":
		testGoogle(id)
	default:
		fmt.Println("platform must be apple or google")
		os.Exit(1)
	}
}

// ── Apple ─────────────────────────────────────────────────────────────────────

func testApple(txID, notifType string) {
	tx, _ := json.Marshal(map[string]any{
		"originalTransactionId": txID,
		"productId":             "com.fitcal.plan.committed.monthly",
		"expiresDate":           int64(9999999999000),
		"purchaseDate":          int64(1700000000000),
	})
	txJWS := fakeJWS(tx)

	outer, _ := json.Marshal(map[string]any{
		"notificationType": notifType,
		"subtype":          "",
		"data":             map[string]any{"signedTransactionInfo": txJWS},
	})
	outerJWS := fakeJWS(outer)

	body, _ := json.Marshal(map[string]any{"signedPayload": outerJWS})
	post(baseURL+"/webhooks/apple", body)
}

// ── Google ────────────────────────────────────────────────────────────────────

func testGoogle(purchaseToken string) {
	rtdn, _ := json.Marshal(map[string]any{
		"packageName": "com.fitcal.app",
		"subscriptionNotification": map[string]any{
			"version":          "1.0",
			"notificationType": 13, // SUBSCRIPTION_EXPIRED
			"purchaseToken":    purchaseToken,
			"subscriptionId":   "fitcal_plan_committed_monthly",
		},
	})

	body, _ := json.Marshal(map[string]any{
		"message": map[string]any{
			"data":      base64.StdEncoding.EncodeToString(rtdn),
			"messageId": "test-1",
		},
	})
	post(baseURL+"/webhooks/google", body)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func fakeJWS(payload []byte) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256"}`))
	middle := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + middle + ".fakesig"
}

func post(url string, body []byte) {
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	if len(out) > 0 {
		fmt.Println(string(out))
	}
}
