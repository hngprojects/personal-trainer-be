package routes_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/routes"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	// Hard opt-in: this suite is destructive (TRUNCATE ... CASCADE).
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("integration tests disabled; set RUN_INTEGRATION_TESTS=1 to enable (destructive TRUNCATE)")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping trainers integration test")
	}

	// Safety check: refuse to run against a DB that doesn't look like an isolated test database.
	u, err := url.Parse(dsn)
	require.NoError(t, err)

	dbName := strings.TrimPrefix(u.Path, "/")
	require.NotEmpty(t, dbName, "DATABASE_URL must include a database name")

	lower := strings.ToLower(dbName)
	if !strings.Contains(lower, "test") && !strings.Contains(lower, "_it") && !strings.Contains(lower, "integration") {
		t.Fatalf("refusing to run destructive integration test against database %q (DATABASE_URL); use a dedicated test DB (name should contain 'test'/'_it'/'integration')", dbName)
	}

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 10*time.Second, 200*time.Millisecond)

	return db, func() { _ = db.Close() }
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	_, err := db.Exec(q)
	require.NoError(t, err)
}

func resetTables(t *testing.T, db *sql.DB) {
	t.Helper()

	// Requires the DB has already been migrated via goose.
	// CASCADE handles FKs between users/trainers/sessions/etc.
	mustExec(t, db, `TRUNCATE TABLE trainers RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE sessions RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE verification_codes RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE users RESTART IDENTITY CASCADE;`)
}

func insertUser(t *testing.T, db *sql.DB, email, name, role string) string {
	t.Helper()

	var id string
	err := db.QueryRow(`
INSERT INTO users (email, name, auth_provider, role)
VALUES ($1, $2, 'local', $3)
RETURNING id
`, email, name, role).Scan(&id)
	require.NoError(t, err)
	return id
}

func tokenFor(t *testing.T, userID string) string {
	t.Helper()
	auth.Configure("test-secret")
	tok, err := auth.GenerateJWTToken(userID, auth.AccessToken)
	require.NoError(t, err)
	return tok
}

func newServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		Env:         "test",
		FrontendURL: "http://localhost:3000",
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := routes.New(cfg, log, db, nil).Routes()

	return httptest.NewServer(r)
}

func doReq(t *testing.T, client *http.Client, req *http.Request) *http.Response {
	t.Helper()
	res, err := client.Do(req)
	require.NoError(t, err)
	return res
}

func TestTrainersEndpoints(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	// Do NOT create schema here; use goose migrations outside the test.
	resetTables(t, db)

	adminID := insertUser(t, db, "admin@example.com", "Admin", "admin")
	clientID := insertUser(t, db, "client@example.com", "Client", "client")
	trainerUserID := insertUser(t, db, "trainer1@example.com", "Trainer One", "client")

	adminToken := tokenFor(t, adminID)
	clientToken := tokenFor(t, clientID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)

	httpClient := http.DefaultClient

	// 1) Unauthorized without token
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	}

	// 2) Forbidden for non-admin
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()

		require.Equal(t, http.StatusForbidden, res.StatusCode)
	}

	// 3) OK for admin list
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	}

	// 4) Create trainer
	var trainerID string
	{
		createBody := map[string]any{
			"user_id":             trainerUserID,
			"specialization":      "Weight loss",
			"bio":                 "Helping clients lose weight safely",
			"years_of_experience": 5,
			"intro_video_url":     "https://example.com/intro.mp4",
			"display_picture":     "https://example.com/pic.jpg",
			"calendly_connected":  false,
			"calendly_link":       "",
			"onboarding_status":   "pending",
		}

		b, err := json.Marshal(createBody)
		require.NoError(t, err)

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/trainers", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")

		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusCreated, res.StatusCode)

		// New shape: { "data": { "id": "..." }, ... }
		var createResp struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&createResp))
		require.NotEmpty(t, createResp.Data.ID)

		trainerID = createResp.Data.ID
	}

	// 5) Get by id
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers/"+trainerID, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	}

	// 6) Update (use PATCH to match spec)
	{
		updateBody := map[string]any{
			"onboarding_status":  "approved",
			"calendly_connected": true,
			"calendly_link":      "https://calendly.com/trainer-one/intro",
		}

		b, err := json.Marshal(updateBody)
		require.NoError(t, err)

		req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/trainers/"+trainerID, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")

		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	}

	// 7) Delete (204)
	{
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/trainers/"+trainerID, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNoContent, res.StatusCode)
	}
}
