package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// fakeSetupRepo is an in-memory AccountSetupRepository. Single-token model
// is enough — every test creates a fresh repo so there's no shared state.
type fakeSetupRepo struct {
	mu         sync.Mutex
	userID     uuid.UUID
	tokenHash  string
	expiresAt  time.Time
	consumed   bool
	user       *db.User
	updateErr  error // forced error on ConsumeTokenAndSetPassword
}

func (r *fakeSetupRepo) UpsertToken(_ context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userID = userID
	r.tokenHash = tokenHash
	r.expiresAt = expiresAt
	r.consumed = false
	return nil
}

func (r *fakeSetupRepo) ConsumeTokenAndSetPassword(_ context.Context, tokenHash, _ string) (*db.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	if r.tokenHash == "" || tokenHash != r.tokenHash || r.consumed || time.Now().After(r.expiresAt) {
		return nil, auth.ErrNotFound
	}
	r.consumed = true
	if r.user == nil {
		r.user = &db.User{ID: r.userID, Email: "trainer@test.local"}
	}
	return r.user, nil
}

func (r *fakeSetupRepo) TokenStatus(_ context.Context, userID uuid.UUID) (bool, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.userID == uuid.Nil || r.userID != userID {
		return false, false, nil
	}
	return r.consumed, true, nil
}

func (r *fakeSetupRepo) PeekToken(_ context.Context, tokenHash string) (bool, time.Time, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tokenHash == "" || tokenHash != r.tokenHash {
		return false, time.Time{}, false, nil
	}
	return r.consumed, r.expiresAt, true, nil
}

// captureMailer records the last setup link sent so tests can extract the
// raw token from the URL and feed it back into the consume endpoint.
type captureMailer struct {
	mu   sync.Mutex
	to   string
	link string
	err  error
}

func (m *captureMailer) SendVerificationCode(_, _ string, _ int) error  { return nil }
func (m *captureMailer) SendAdminCredentials(_, _ string) error         { return nil }
func (m *captureMailer) SendTrainerCredentials(_, _ string) error       { return nil }
func (m *captureMailer) SendPasswordResetCode(_, _ string, _ int) error { return nil }
func (m *captureMailer) SendWaitlistConfirmation(_ string) error        { return nil }
func (m *captureMailer) SendContactConfirmation(_, _ string) error      { return nil }
func (m *captureMailer) SendDiscoveryBookingConfirmation(_, _ string, _ time.Time, _, _, _, _ string) error {
	return nil
}
func (m *captureMailer) SendDiscoveryBookingAdminNotification(_, _, _ string, _ time.Time, _, _, _, _ string) error {
	return nil
}
func (m *captureMailer) SendDiscoveryRescheduleConfirmation(_, _ string, _, _ time.Time, _, _, _, _ string) error {
	return nil
}
func (m *captureMailer) SendPaidSessionRescheduleConfirmation(_, _ string, _, _ time.Time, _, _ string) error {
	return nil
}
func (m *captureMailer) SendPaidSessionRescheduleTrainerNotification(_, _ string, _, _ time.Time, _, _ string) error {
	return nil
}
func (m *captureMailer) SendBookingConfirmation(_, _, _ string, _, _ time.Time, _, _ string) error {
	return nil
}
func (m *captureMailer) SendSessionReminder(_, _, _ string, _ time.Time, _, _ string) error {
	return nil
}
func (m *captureMailer) SendSessionReminderTrainer(_, _, _ string, _ time.Time, _, _ string) error {
	return nil
}

func (m *captureMailer) SendAccountSetupLink(to, _, link string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.to = to
	m.link = link
	return nil
}

func (m *captureMailer) snapshot() (to, link string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.to, m.link
}

func tokenFromLink(t *testing.T, link string) string {
	t.Helper()
	const marker = "token="
	idx := strings.Index(link, marker)
	require.GreaterOrEqual(t, idx, 0, "link missing token=: %s", link)
	return link[idx+len(marker):]
}

func newTestHandler(t *testing.T, repo auth.AccountSetupRepository, mailer *captureMailer) (*auth.AccountSetupHandler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := auth.NewAccountSetupHandler(
		repo,
		mailer,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		"unit-test-secret",
		"http://localhost:3000",
		24,
		nil,
	)
	r := gin.New()
	r.POST("/trainers/set-password", h.HandleSetPassword)
	r.GET("/trainers/set-password/validate", h.HandleValidateSetupToken)
	return h, r
}

func postJSON(t *testing.T, r http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		payload = b
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// IssueAndSend
// ---------------------------------------------------------------------------

func TestIssueAndSend_PersistsAndMails(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, _ := newTestHandler(t, repo, mailer)

	userID := uuid.New()
	require.NoError(t, h.IssueAndSend(context.Background(), userID, "trainer@test.local", "Tester"))

	require.Equal(t, userID, repo.userID, "token row should be keyed by user_id")
	require.NotEmpty(t, repo.tokenHash, "token hash must be persisted")
	require.True(t, repo.expiresAt.After(time.Now()), "expiry must be in the future")

	to, link := mailer.snapshot()
	require.Equal(t, "trainer@test.local", to)
	require.True(t, strings.HasPrefix(link, "http://localhost:3000/trainers/set-password?token="),
		"link must point at FE set-password page: %s", link)
	require.NotContains(t, link, repo.tokenHash, "link must carry the raw token, not the stored hash")
}

func TestIssueAndSend_MailerErrorBubbles(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{err: errors.New("smtp down")}
	h, _ := newTestHandler(t, repo, mailer)

	err := h.IssueAndSend(context.Background(), uuid.New(), "trainer@test.local", "Tester")
	require.Error(t, err, "mailer failure must propagate so caller can return 500")
}

// ---------------------------------------------------------------------------
// HandleSetPassword
// ---------------------------------------------------------------------------

func TestHandleSetPassword_HappyPath(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, router := newTestHandler(t, repo, mailer)

	userID := uuid.New()
	require.NoError(t, h.IssueAndSend(context.Background(), userID, "trainer@test.local", "Tester"))
	_, link := mailer.snapshot()
	token := tokenFromLink(t, link)

	rec := postJSON(t, router, "/trainers/set-password", map[string]any{
		"token":        token,
		"new_password": "Strong1Password",
	})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.True(t, repo.consumed, "token must be marked consumed on success")
}

func TestHandleSetPassword_ReplayRejected(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, router := newTestHandler(t, repo, mailer)

	require.NoError(t, h.IssueAndSend(context.Background(), uuid.New(), "trainer@test.local", "Tester"))
	_, link := mailer.snapshot()
	token := tokenFromLink(t, link)

	body := map[string]any{"token": token, "new_password": "Strong1Password"}
	rec1 := postJSON(t, router, "/trainers/set-password", body)
	require.Equal(t, http.StatusOK, rec1.Code)

	rec2 := postJSON(t, router, "/trainers/set-password", body)
	require.Equal(t, http.StatusBadRequest, rec2.Code, "second use of same token must 400")
}

func TestHandleSetPassword_ExpiredToken(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, router := newTestHandler(t, repo, mailer)

	// Issue a real token so its HMAC matches what the handler computes,
	// then backdate the persisted expiry so consume must reject on the
	// "expired" branch — not the "unknown token" branch.
	require.NoError(t, h.IssueAndSend(context.Background(), uuid.New(), "trainer@test.local", "Tester"))
	_, link := mailer.snapshot()
	repo.mu.Lock()
	repo.expiresAt = time.Now().Add(-1 * time.Hour)
	repo.mu.Unlock()

	rec := postJSON(t, router, "/trainers/set-password", map[string]any{
		"token":        tokenFromLink(t, link),
		"new_password": "Strong1Password",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, repo.consumed, "expired token must not be consumed")
}

func TestHandleSetPassword_UnknownToken(t *testing.T) {
	_, router := newTestHandler(t, &fakeSetupRepo{}, &captureMailer{})

	rec := postJSON(t, router, "/trainers/set-password", map[string]any{
		"token":        "fake-token-never-issued",
		"new_password": "Strong1Password",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleSetPassword_WeakPasswordValidationErrors(t *testing.T) {
	cases := []struct {
		name     string
		password string
	}{
		{"too short", "Ab1"},
		{"no digit", "NoDigitsHere"},
		{"no upper", "all-lower-1"},
		{"no lower", "ALL-UPPER-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSetupRepo{}
			mailer := &captureMailer{}
			h, router := newTestHandler(t, repo, mailer)
			require.NoError(t, h.IssueAndSend(context.Background(), uuid.New(), "trainer@test.local", "Tester"))
			_, link := mailer.snapshot()

			rec := postJSON(t, router, "/trainers/set-password", map[string]any{
				"token":        tokenFromLink(t, link),
				"new_password": tc.password,
			})
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.False(t, repo.consumed, "weak password must not consume the token")
		})
	}
}

func TestHandleSetPassword_MissingToken(t *testing.T) {
	_, router := newTestHandler(t, &fakeSetupRepo{}, &captureMailer{})

	rec := postJSON(t, router, "/trainers/set-password", map[string]any{
		"new_password": "Strong1Password",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// IsActivated
// ---------------------------------------------------------------------------

func TestIsActivated_FlipsOnConsume(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, router := newTestHandler(t, repo, mailer)

	userID := uuid.New()
	require.NoError(t, h.IssueAndSend(context.Background(), userID, "trainer@test.local", "Tester"))

	activated, err := h.IsActivated(context.Background(), userID)
	require.NoError(t, err)
	require.False(t, activated, "fresh token must not show as activated")

	_, link := mailer.snapshot()
	rec := postJSON(t, router, "/trainers/set-password", map[string]any{
		"token":        tokenFromLink(t, link),
		"new_password": "Strong1Password",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	activated, err = h.IsActivated(context.Background(), userID)
	require.NoError(t, err)
	require.True(t, activated, "after successful consume, IsActivated must be true")
}

// ---------------------------------------------------------------------------
// HandleValidateSetupToken
// ---------------------------------------------------------------------------

// validateResponse mirrors the success payload from HandleValidateSetupToken
// — pulled into its own type so each test asserts on the same shape.
type validateResponse struct {
	Data struct {
		Status    string `json:"status"`
		Valid     bool   `json:"valid"`
		ExpiresAt string `json:"expires_at,omitempty"`
	} `json:"data"`
}

func getValidate(t *testing.T, r http.Handler, token string) (*httptest.ResponseRecorder, validateResponse) {
	t.Helper()
	target := "/trainers/set-password/validate"
	if token != "" {
		// url.Values handles query-reserved characters correctly. Raw
		// concatenation would silently corrupt tokens that ever contain
		// '+', '&', '=', etc — base64-url-safe encoding mostly dodges
		// this today, but a future token format shouldn't break the
		// test helper.
		q := url.Values{}
		q.Set("token", token)
		target += "?" + q.Encode()
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var body validateResponse
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	}
	return rec, body
}

func TestHandleValidateSetupToken_ValidToken(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, router := newTestHandler(t, repo, mailer)

	require.NoError(t, h.IssueAndSend(context.Background(), uuid.New(), "trainer@test.local", "Tester"))
	_, link := mailer.snapshot()

	rec, body := getValidate(t, router, tokenFromLink(t, link))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "valid", body.Data.Status)
	require.True(t, body.Data.Valid)
	require.NotEmpty(t, body.Data.ExpiresAt, "expires_at must be returned for a known token")
	require.False(t, repo.consumed, "validate must NOT consume the token")
}

func TestHandleValidateSetupToken_ExpiredToken(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, router := newTestHandler(t, repo, mailer)

	require.NoError(t, h.IssueAndSend(context.Background(), uuid.New(), "trainer@test.local", "Tester"))
	_, link := mailer.snapshot()
	// Backdate persisted expiry; same trick as the consume-expired test.
	repo.mu.Lock()
	repo.expiresAt = time.Now().Add(-1 * time.Hour)
	repo.mu.Unlock()

	rec, body := getValidate(t, router, tokenFromLink(t, link))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "expired", body.Data.Status)
	require.False(t, body.Data.Valid)
}

func TestHandleValidateSetupToken_ConsumedToken(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, router := newTestHandler(t, repo, mailer)

	require.NoError(t, h.IssueAndSend(context.Background(), uuid.New(), "trainer@test.local", "Tester"))
	_, link := mailer.snapshot()
	token := tokenFromLink(t, link)

	// Consume it via the real handler so the state mirrors a real flow.
	rec := postJSON(t, router, "/trainers/set-password", map[string]any{
		"token":        token,
		"new_password": "Strong1Password",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	rec2, body := getValidate(t, router, token)
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "consumed", body.Data.Status)
	require.False(t, body.Data.Valid)
}

func TestHandleValidateSetupToken_UnknownToken(t *testing.T) {
	_, router := newTestHandler(t, &fakeSetupRepo{}, &captureMailer{})

	rec, body := getValidate(t, router, "never-issued-token")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "invalid", body.Data.Status)
	require.False(t, body.Data.Valid)
}

func TestHandleValidateSetupToken_MissingTokenIs400(t *testing.T) {
	_, router := newTestHandler(t, &fakeSetupRepo{}, &captureMailer{})

	rec, _ := getValidate(t, router, "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleValidateSetupToken_BoundaryMatchesConsume pins the contract
// that validate and consume agree at the exact expiry instant: the consume
// query uses `expires_at > NOW()`, so a token at expires_at == now must
// fail consume, and therefore validate must already report "expired" so
// the FE doesn't render the password form on a token the next POST will
// reject.
func TestHandleValidateSetupToken_BoundaryMatchesConsume(t *testing.T) {
	repo := &fakeSetupRepo{}
	mailer := &captureMailer{}
	h, router := newTestHandler(t, repo, mailer)

	require.NoError(t, h.IssueAndSend(context.Background(), uuid.New(), "trainer@test.local", "Tester"))
	_, link := mailer.snapshot()

	// Pin expires_at to a moment already in the past relative to the
	// handler's clock; intent is "the boundary has been reached or
	// crossed" which is exactly the case the consume query rejects.
	repo.mu.Lock()
	repo.expiresAt = time.Now().Add(-time.Microsecond)
	repo.mu.Unlock()

	rec, body := getValidate(t, router, tokenFromLink(t, link))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "expired", body.Data.Status, "validate must agree with consume at the expiry boundary")
}

// Suppress unused-import warning if any earlier test stops referencing errors.
var _ = errors.New
