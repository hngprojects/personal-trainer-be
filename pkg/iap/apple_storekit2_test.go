package iap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signedTxFixture mints a self-signed Apple-shaped JWS chain
// (leaf → intermediate → root) and returns the JWS string plus the
// root PEM. Tests temporarily swap AppleRootCAG3PEM with the fixture
// root so the chain validates without needing real Apple certs.
type signedTxFixture struct {
	jws     string
	rootPEM string
	t       *testing.T
}

// makeChainAndJWS builds a fresh chain of three certs (root, intermediate,
// leaf) and produces a signed JWS over claims, with the chain embedded
// in the x5c header.
func makeChainAndJWS(t *testing.T, claims map[string]interface{}) *signedTxFixture {
	t.Helper()

	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("root key: %v", err)
	}
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Apple Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("root cert: %v", err)
	}
	rootCert, _ := x509.ParseCertificate(rootDER)

	intKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	intTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test Apple Intermediate"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	intDER, err := x509.CreateCertificate(rand.Reader, intTmpl, rootCert, &intKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("int cert: %v", err)
	}
	intCert, _ := x509.ParseCertificate(intDER)

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Test Apple Leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, intCert, &leafKey.PublicKey, intKey)
	if err != nil {
		t.Fatalf("leaf cert: %v", err)
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims(claims))
	tok.Header["x5c"] = []string{
		base64.StdEncoding.EncodeToString(leafDER),
		base64.StdEncoding.EncodeToString(intDER),
		base64.StdEncoding.EncodeToString(rootDER),
	}
	jws, err := tok.SignedString(leafKey)
	if err != nil {
		t.Fatalf("sign jws: %v", err)
	}

	rootPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}))
	return &signedTxFixture{jws: jws, rootPEM: rootPEM, t: t}
}

// withPinnedRoot swaps AppleRootCAG3PEM for the duration of a test so
// the fixture chain validates. Restore via the returned func.
func withPinnedRoot(_ *testing.T, pem string) func() {
	orig := AppleRootCAG3PEM
	AppleRootCAG3PEM = pem // global var; tests run serially in this package
	return func() { AppleRootCAG3PEM = orig }
}

func validClaims() map[string]interface{} {
	now := time.Now()
	return map[string]interface{}{
		"transactionId":         "1000000123456789",
		"originalTransactionId": "1000000123456789",
		"bundleId":              "com.fitcal.app",
		"productId":             "com.fitcal.plan.committed.monthly",
		"purchaseDate":          now.UnixMilli(),
		"expiresDate":           now.AddDate(0, 1, 0).UnixMilli(),
		"environment":           "Production",
		"offerType":             0,
	}
}

func TestVerify_HappyPath(t *testing.T) {
	fx := makeChainAndJWS(t, validClaims())
	defer withPinnedRoot(t, fx.rootPEM)()

	got, err := VerifyAppleSignedTransaction(fx.jws, "com.fitcal.app", "com.fitcal.plan.committed.monthly", "production")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.OriginalTransactionID != "1000000123456789" {
		t.Errorf("oti: got %q", got.OriginalTransactionID)
	}
	if got.ProductID != "com.fitcal.plan.committed.monthly" {
		t.Errorf("product: got %q", got.ProductID)
	}
	if got.PurchasedAt.IsZero() || got.ExpiresAt.IsZero() {
		t.Errorf("dates not populated: %+v", got)
	}
}

func TestVerify_DetectsTrialOffer(t *testing.T) {
	claims := validClaims()
	claims["offerType"] = 1 // introductory / trial
	fx := makeChainAndJWS(t, claims)
	defer withPinnedRoot(t, fx.rootPEM)()

	got, err := VerifyAppleSignedTransaction(fx.jws, "com.fitcal.app", "com.fitcal.plan.committed.monthly", "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !got.IsTrialPeriod {
		t.Errorf("offerType=1 should map to IsTrialPeriod=true")
	}
}

func TestVerify_RejectsBundleMismatch(t *testing.T) {
	fx := makeChainAndJWS(t, validClaims())
	defer withPinnedRoot(t, fx.rootPEM)()

	_, err := VerifyAppleSignedTransaction(fx.jws, "com.someone-else.app", "com.fitcal.plan.committed.monthly", "")
	if err == nil || !strings.Contains(err.Error(), "bundle id mismatch") {
		t.Fatalf("expected bundle id mismatch, got %v", err)
	}
}

func TestVerify_RejectsProductMismatch(t *testing.T) {
	fx := makeChainAndJWS(t, validClaims())
	defer withPinnedRoot(t, fx.rootPEM)()

	_, err := VerifyAppleSignedTransaction(fx.jws, "com.fitcal.app", "com.someone-else.plan", "")
	if err == nil || !strings.Contains(err.Error(), "product id mismatch") {
		t.Fatalf("expected product id mismatch, got %v", err)
	}
}

func TestVerify_RejectsSandboxInProductionPinning(t *testing.T) {
	claims := validClaims()
	claims["environment"] = "Sandbox"
	fx := makeChainAndJWS(t, claims)
	defer withPinnedRoot(t, fx.rootPEM)()

	_, err := VerifyAppleSignedTransaction(fx.jws, "com.fitcal.app", "com.fitcal.plan.committed.monthly", "production")
	if err == nil || !strings.Contains(err.Error(), "environment mismatch") {
		t.Fatalf("expected environment mismatch, got %v", err)
	}
}

func TestVerify_AcceptsAnyEnvWhenUnpinned(t *testing.T) {
	for _, env := range []string{"Sandbox", "Production"} {
		claims := validClaims()
		claims["environment"] = env
		fx := makeChainAndJWS(t, claims)
		restore := withPinnedRoot(t, fx.rootPEM)
		_, err := VerifyAppleSignedTransaction(fx.jws, "com.fitcal.app", "com.fitcal.plan.committed.monthly", "")
		restore()
		if err != nil {
			t.Errorf("env=%q should be accepted when expectedEnv is empty: %v", env, err)
		}
	}
}

func TestVerify_RejectsChainNotAnchoredToAppleRoot(t *testing.T) {
	fx := makeChainAndJWS(t, validClaims())
	// Do NOT swap the pinned root — chain validation must fail
	// because the fixture root won't match the real Apple Root CA G3.
	_, err := VerifyAppleSignedTransaction(fx.jws, "com.fitcal.app", "com.fitcal.plan.committed.monthly", "")
	if err == nil {
		t.Fatal("chain not anchored to pinned root must be rejected")
	}
}

func TestVerify_RejectsBadSignature(t *testing.T) {
	fx := makeChainAndJWS(t, validClaims())
	defer withPinnedRoot(t, fx.rootPEM)()

	// Flip the last byte of the signature segment.
	parts := strings.Split(fx.jws, ".")
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sig[len(sig)-1] ^= 0xFF
	tampered := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(sig)

	_, err := VerifyAppleSignedTransaction(tampered, "com.fitcal.app", "com.fitcal.plan.committed.monthly", "")
	if err == nil {
		t.Fatal("tampered signature must be rejected")
	}
	if !errors.Is(err, jwt.ErrTokenSignatureInvalid) && !strings.Contains(err.Error(), "signature") {
		t.Logf("(non-fatal) expected jwt signature error, got: %v", err)
	}
}

func TestVerify_RejectsRSAAlg(t *testing.T) {
	// Only ES256 is acceptable for Apple JWS. Hand-craft a header
	// claiming RS256 so the parser refuses before signature verification.
	header := map[string]interface{}{
		"alg": "RS256",
		"x5c": []string{"AAAA"},
	}
	hb, _ := json.Marshal(header)
	tampered := base64.RawURLEncoding.EncodeToString(hb) + ".eyJmb28iOiJiYXIifQ.AAAA"

	_, err := VerifyAppleSignedTransaction(tampered, "com.fitcal.app", "", "")
	if err == nil {
		t.Fatal("RS256 must be rejected")
	}
}

func TestVerify_RejectsEmptyInputs(t *testing.T) {
	if _, err := VerifyAppleSignedTransaction("", "com.fitcal.app", "", ""); err == nil {
		t.Error("empty jws should be rejected")
	}
	if _, err := VerifyAppleSignedTransaction("a.b.c", "", "", ""); err == nil {
		t.Error("empty bundle id should be rejected")
	}
}
