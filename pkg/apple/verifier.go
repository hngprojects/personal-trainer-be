// Package apple verifies the identity tokens issued by Sign in with
// Apple. The verifier is intentionally minimal — it does the JWT
// signature check against Apple's published JWK set, validates the
// audience + issuer + expiry, and returns the claim subset the auth
// handler needs. It deliberately does not touch the database.
package apple

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	keyfunc "github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// AppleJWKSURL is Apple's JSON Web Key Set endpoint. The keys rotate
// — empirically once or twice a year — so we re-fetch in the
// background via the JWK Set storage owned by keyfunc.
var AppleJWKSURL = "https://appleid.apple.com/auth/keys"

// AppleIssuer is the expected value of the `iss` claim in the identity
// token. Apple's docs pin both forms (with and without the trailing
// slash); they accept either when validating, so we accept both too.
const (
	AppleIssuer          = "https://appleid.apple.com"
	AppleIssuerWithSlash = "https://appleid.apple.com/"
)

// Claims is the projection of the identity-token payload that the auth
// handler actually needs. Everything else Apple emits (auth_time,
// nonce_supported, real_user_status, …) is dropped.
//
// Email is empty on every sign-in after the first. EmailVerified arrives
// from Apple as the string "true" / "false" (NOT a JSON bool) — the
// verifier normalises that to a Go bool before populating this struct.
// IsPrivateEmail flags Hide-My-Email private relay addresses; callers
// can store them as-is and send mail through them, but should not show
// them in profile UI as "your email".
type Claims struct {
	Sub            string
	Email          string
	EmailVerified  bool
	IsPrivateEmail bool
}

// Verifier checks identity tokens against the configured audiences.
// One Verifier per process — it owns the JWKS client which caches keys
// in-memory and refreshes them on background ticks tied to the
// constructor context.
type Verifier struct {
	allowedAudiences []string
	keyfunc          jwt.Keyfunc
}

// VerifierOption tunes constructor behaviour for tests.
type VerifierOption func(*verifierOptions)

type verifierOptions struct {
	jwksURL         string
	keyfuncOverride jwt.Keyfunc
}

// WithJWKSURL points the verifier at a non-default JWKS endpoint —
// used by tests that serve a fixture key set from httptest.
func WithJWKSURL(u string) VerifierOption {
	return func(o *verifierOptions) { o.jwksURL = u }
}

// WithKeyfunc swaps the JWKS client out entirely. Test-only: lets a
// unit test sign with its own RSA key without needing to host a JWKS
// server.
func WithKeyfunc(kf jwt.Keyfunc) VerifierOption {
	return func(o *verifierOptions) { o.keyfuncOverride = kf }
}

// NewVerifier builds a verifier that accepts identity tokens whose
// `aud` claim matches any of the supplied bundle IDs. An empty
// audience list returns an error — refusing to construct a verifier
// that can never accept anything is safer than silently accepting
// everything. The supplied context bounds the JWKS background
// refresh; cancel it on server shutdown.
func NewVerifier(ctx context.Context, allowedAudiences []string, opts ...VerifierOption) (*Verifier, error) {
	auds := make([]string, 0, len(allowedAudiences))
	for _, a := range allowedAudiences {
		if a = strings.TrimSpace(a); a != "" {
			auds = append(auds, a)
		}
	}
	if len(auds) == 0 {
		return nil, errors.New("apple verifier: no allowed audiences configured")
	}

	o := verifierOptions{jwksURL: AppleJWKSURL}
	for _, opt := range opts {
		opt(&o)
	}

	if o.keyfuncOverride != nil {
		return &Verifier{allowedAudiences: auds, keyfunc: o.keyfuncOverride}, nil
	}

	k, err := keyfunc.NewDefaultCtx(ctx, []string{o.jwksURL})
	if err != nil {
		return nil, fmt.Errorf("apple verifier: fetch JWKS: %w", err)
	}
	return &Verifier{allowedAudiences: auds, keyfunc: k.Keyfunc}, nil
}

// Verify parses idToken, verifies its signature against the JWK set,
// and validates the standard claims. Returns a populated Claims on
// success. Any failure — bad signature, wrong audience, expired,
// wrong issuer, missing sub — returns an error and no claims.
func (v *Verifier) Verify(_ context.Context, idToken string) (*Claims, error) {
	if v == nil {
		return nil, errors.New("apple verifier: nil receiver")
	}
	if strings.TrimSpace(idToken) == "" {
		return nil, errors.New("apple verifier: empty id token")
	}
	if v.keyfunc == nil {
		return nil, errors.New("apple verifier: keyfunc not initialised")
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
	)
	tok, err := parser.Parse(idToken, v.keyfunc)
	if err != nil {
		return nil, fmt.Errorf("parse apple token: %w", err)
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		return nil, errors.New("apple verifier: token claims invalid")
	}

	if err := assertAppleIssuer(claims); err != nil {
		return nil, err
	}
	if err := assertAudienceAllowed(claims, v.allowedAudiences); err != nil {
		return nil, err
	}

	sub, _ := claims["sub"].(string)
	sub = strings.TrimSpace(sub)
	if sub == "" {
		return nil, errors.New("apple verifier: missing sub")
	}

	email, _ := claims["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))

	out := &Claims{
		Sub:            sub,
		Email:          email,
		EmailVerified:  readAppleBool(claims["email_verified"]),
		IsPrivateEmail: readAppleBool(claims["is_private_email"]),
	}
	// If Apple omitted the explicit flag but the address shape matches
	// the private relay domain, treat it as private. Belt-and-braces
	// for tokens that drop the flag on subsequent sign-ins.
	if !out.IsPrivateEmail && strings.HasSuffix(out.Email, "@privaterelay.appleid.com") {
		out.IsPrivateEmail = true
	}

	return out, nil
}

func assertAppleIssuer(claims jwt.MapClaims) error {
	iss, _ := claims["iss"].(string)
	if iss != AppleIssuer && iss != AppleIssuerWithSlash {
		return fmt.Errorf("apple verifier: unexpected issuer %q", iss)
	}
	return nil
}

func assertAudienceAllowed(claims jwt.MapClaims, allowed []string) error {
	// jwt-v5 lets `aud` be a string OR a string array. Apple has
	// historically emitted a single string, but accept both shapes
	// to stay forward-compatible.
	var auds []string
	switch v := claims["aud"].(type) {
	case string:
		auds = []string{v}
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok {
				auds = append(auds, s)
			}
		}
	case []string:
		auds = v
	}
	for _, got := range auds {
		got = strings.TrimSpace(got)
		for _, want := range allowed {
			if got == want {
				return nil
			}
		}
	}
	return fmt.Errorf("apple verifier: audience %v not in allow-list", auds)
}

// readAppleBool decodes either a bool or the string "true"/"false"
// shapes Apple has emitted across versions of the API.
func readAppleBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	default:
		return false
	}
}
