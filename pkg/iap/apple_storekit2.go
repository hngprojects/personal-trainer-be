package iap

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppleRootCAG3PEM is Apple's "Apple Root CA - G3" certificate, pinned in
// the binary so the App Store Server-issued JWS chain can be validated
// offline. Distributed by Apple at:
//
//	https://www.apple.com/certificateauthority/AppleRootCA-G3.cer
//
// Valid through 2039-04-30. If Apple ever rotates the root we'd need to
// update this value — but they kept G2 alive for two decades, so this
// is not a near-term concern. Exposed as `var` (not `const`) so tests
// can swap in a fixture-signed root without standing up real Apple
// infrastructure; not intended to be rewritten in production code.
var AppleRootCAG3PEM = `-----BEGIN CERTIFICATE-----
MIICQzCCAcmgAwIBAgIILcX8iNLFS5UwCgYIKoZIzj0EAwMwZzEbMBkGA1UEAwwS
QXBwbGUgUm9vdCBDQSAtIEczMSYwJAYDVQQLDB1BcHBsZSBDZXJ0aWZpY2F0aW9u
IEF1dGhvcml0eTETMBEGA1UECgwKQXBwbGUgSW5jLjELMAkGA1UEBhMCVVMwHhcN
MTQwNDMwMTgxOTA2WhcNMzkwNDMwMTgxOTA2WjBnMRswGQYDVQQDDBJBcHBsZSBS
b290IENBIC0gRzMxJjAkBgNVBAsMHUFwcGxlIENlcnRpZmljYXRpb24gQXV0aG9y
aXR5MRMwEQYDVQQKDApBcHBsZSBJbmMuMQswCQYDVQQGEwJVUzB2MBAGByqGSM49
AgEGBSuBBAAiA2IABJjpLz1AcqTtkyJygRMc3RCV8cWjTnHcFBbZDuWmBSp3ZHtf
TjjTuxxEtX/1H7YyYl3J6YRbTzBPEVoA/VhYDKX1DyxNB0cTddqXl5dvMVztK517
IDvYuVTZXpmkOlEKMaNCMEAwHQYDVR0OBBYEFLuw3qFYM4iapIqZ3r6966/ayySr
MA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgEGMAoGCCqGSM49BAMDA2gA
MGUCMQCD6cHEFl4aXTQY2e3v9GwOAEZLuN+yRhHFD/3meoyhpmvOwgPUnPWTxnS4
at+qIxUCMG1mihDK1A3UT82NQz60imOlM27jbdoXt2QfyFMm+YhidDkLF1vLUagM
6BgD56KyKA==
-----END CERTIFICATE-----`

// JWSPurchase is the projection of Apple's JWSTransactionDecodedPayload
// that we actually consume. Apple's full payload is large and includes
// fields we don't need here (offer info, web order line item id, etc.).
type JWSPurchase struct {
	TransactionID         string
	OriginalTransactionID string
	BundleID              string
	ProductID             string
	PurchaseDate          time.Time
	ExpiresDate           time.Time
	IsTrialPeriod         bool
	Environment           string // "Sandbox" or "Production"
}

// jwsTransactionPayload mirrors the fields we need from
// Apple's `JWSTransactionDecodedPayload`. Date fields are ms-since-epoch
// integers (NOT strings — that's the legacy verifyReceipt format).
type jwsTransactionPayload struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	PurchaseDate          int64  `json:"purchaseDate"`
	ExpiresDate           int64  `json:"expiresDate"`
	// Apple's "offerType: 1" means introductory offer (trial). The
	// dedicated isTrialPeriod field was dropped between StoreKit 1 and 2;
	// in the JWS payload the closest equivalent is offerType == 1.
	OfferType int `json:"offerType"`
	// "Sandbox" or "Production". Used in logs and to refuse cross-env
	// receipts in production builds.
	Environment string `json:"environment"`
}

// VerifyAppleSignedTransaction validates a StoreKit 2 signed transaction
// JWS produced by `Transaction.jsonRepresentation` on the mobile client.
//
// Verification has three layers:
//  1. Parse the JWS header `x5c` — three base64-encoded DER certs (leaf,
//     intermediate, root).
//  2. Validate the chain against the pinned Apple Root CA G3.
//  3. Verify the JWS ES256 signature using the leaf certificate's public
//     key.
//
// On success we then check bundleId matches our config and the requested
// productId appears in the payload. expectedEnv ("production" or
// "sandbox" — empty means accept either) lets prod builds reject
// sandbox-signed receipts.
func VerifyAppleSignedTransaction(signedTx, expectedBundleID, expectedProductID, expectedEnv string) (*VerifiedPurchase, error) {
	if strings.TrimSpace(signedTx) == "" {
		return nil, errors.New("apple: empty signed transaction")
	}
	if strings.TrimSpace(expectedBundleID) == "" {
		return nil, errors.New("apple: expected bundle id is required")
	}

	chain, err := parseX5CChain(signedTx)
	if err != nil {
		return nil, fmt.Errorf("apple: parse x5c chain: %w", err)
	}
	if err := verifyAppleChain(chain); err != nil {
		return nil, fmt.Errorf("apple: chain validation: %w", err)
	}

	leafPub, ok := chain[0].PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("apple: leaf certificate is not ECDSA")
	}

	parser := jwt.NewParser(jwt.WithValidMethods([]string{"ES256"}))
	payload := &jwsTransactionPayload{}
	_, err = parser.ParseWithClaims(signedTx, jwt.MapClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return leafPub, nil
	})
	if err != nil {
		return nil, fmt.Errorf("apple: jws signature: %w", err)
	}

	// jwt-v5's MapClaims doesn't help us here because Apple's payload
	// has int64 timestamps + custom field names — decode the raw
	// payload segment directly instead of round-tripping through
	// MapClaims.
	if err := decodeJWSPayload(signedTx, payload); err != nil {
		return nil, fmt.Errorf("apple: decode payload: %w", err)
	}

	if payload.BundleID != expectedBundleID {
		return nil, fmt.Errorf("apple: bundle id mismatch (got %q, want %q)", payload.BundleID, expectedBundleID)
	}
	if expectedProductID != "" && payload.ProductID != expectedProductID {
		return nil, fmt.Errorf("apple: product id mismatch (got %q, want %q)", payload.ProductID, expectedProductID)
	}
	if expectedEnv != "" && !strings.EqualFold(payload.Environment, expectedEnv) {
		return nil, fmt.Errorf("apple: environment mismatch (got %q, want %q)", payload.Environment, expectedEnv)
	}

	return &VerifiedPurchase{
		OriginalTransactionID: payload.OriginalTransactionID,
		ProductID:             payload.ProductID,
		PurchasedAt:           time.UnixMilli(payload.PurchaseDate).UTC(),
		ExpiresAt:             time.UnixMilli(payload.ExpiresDate).UTC(),
		IsTrialPeriod:         payload.OfferType == 1,
	}, nil
}

// parseX5CChain pulls the `x5c` array out of the JWS header and
// returns the three certificates in chain order (leaf, intermediate,
// root).
func parseX5CChain(jws string) ([]*x509.Certificate, error) {
	parts := strings.SplitN(jws, ".", 3)
	if len(parts) != 3 {
		return nil, errors.New("not a JWS compact serialization")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var header struct {
		Alg string   `json:"alg"`
		X5C []string `json:"x5c"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("unmarshal header: %w", err)
	}
	if header.Alg != "ES256" {
		return nil, fmt.Errorf("unexpected alg %q (want ES256)", header.Alg)
	}
	if len(header.X5C) < 2 {
		// Apple sends 3 (leaf + intermediate + root) but we only
		// strictly need leaf + intermediate; the root we pin
		// ourselves. Two is the floor.
		return nil, fmt.Errorf("x5c too short: %d entries", len(header.X5C))
	}
	chain := make([]*x509.Certificate, 0, len(header.X5C))
	for i, b64 := range header.X5C {
		// x5c entries are standard-base64 DER, NOT base64url.
		der, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("decode x5c[%d]: %w", i, err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("parse x5c[%d]: %w", i, err)
		}
		chain = append(chain, cert)
	}
	return chain, nil
}

// verifyAppleChain runs full X.509 chain validation against the pinned
// Apple Root CA. Failing this means either Apple rotated the root, or
// the JWS is forged.
func verifyAppleChain(chain []*x509.Certificate) error {
	roots := x509.NewCertPool()
	block, _ := pem.Decode([]byte(AppleRootCAG3PEM))
	if block == nil {
		return errors.New("pinned apple root PEM failed to decode (binary corruption?)")
	}
	rootCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse pinned root: %w", err)
	}
	roots.AddCert(rootCert)

	intermediates := x509.NewCertPool()
	for i := 1; i < len(chain); i++ {
		intermediates.AddCert(chain[i])
	}

	leaf := chain[0]
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		// Apple's StoreKit leaf cert doesn't include ExtKeyUsage
		// values; ExtKeyUsageAny disables the EKU check so chain
		// validation goes on signature + name + validity alone.
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return err
	}
	return nil
}

// decodeJWSPayload pulls the payload segment of the JWS and
// unmarshals it into target. The JWS has ALREADY been signature-
// verified by the caller — we're only doing this for the typed
// fields (Apple's int64 timestamps + custom field names) that
// jwt.MapClaims doesn't model well.
func decodeJWSPayload(jws string, target interface{}) error {
	parts := strings.SplitN(jws, ".", 3)
	if len(parts) != 3 {
		return errors.New("not a JWS compact serialization")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	return json.Unmarshal(payloadBytes, target)
}
