// routes/trainers_integration_test.go
package routes_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/routes"
)

// setupTestDB connects to DATABASE_URL.
// It will skip the test if DATABASE_URL is not set.
func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping trainers integration test")
	}

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 10*time.Second, 200*time.Millisecond)

	cleanup := func() {
		_ = db.Close()
	}

	return db, cleanup
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	_, err := db.Exec(q)
	require.NoError(t, err)
}

func applySchema(t *testing.T, db *sql.DB) {
	t.Helper()

	// Minimal schema for these tests.
	// NOTE: This assumes your DB user can create extensions/tables in the target database.
	mustExec(t, db, `
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  email TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  auth_provider TEXT NOT NULL DEFAULT 'local',
  role TEXT NOT NULL DEFAULT 'client',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS trainers (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  specialization TEXT,
  bio TEXT,
  years_of_experience INT,
  intro_video_url TEXT,
  display_picture TEXT,
  calendly_connected BOOLEAN NOT NULL DEFAULT false,
  calendly_link TEXT,
  onboarding_status TEXT NOT NULL DEFAULT 'pending',
  average_rating NUMERIC NOT NULL DEFAULT 0,
  total_reviews INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`)
}

func resetTables(t *testing.T, db *sql.DB) {
	t.Helper()
	// Ensure reruns don’t fail due to UNIQUE constraints / leftover rows.
	mustExec(t, db, `TRUNCATE TABLE trainers RESTART IDENTITY CASCADE;`)
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

	// IMPORTANT:
	// Set whichever env var your auth package expects for signing/verifying JWTs.
	// If your repo uses a different variable name than JWT_SECRET, update this.
	if os.Getenv("JWT_SECRET") == "" {
		_ = os.Setenv("JWT_SECRET", "test-secret")
	}

	tok, err := auth.GenerateJWTToken(userID, auth.AccessToken)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	return tok
}

func newServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		Env:         "test",
		FrontendURL: "http://localhost:3000",
	}
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := routes.New(cfg, log, db).Routes()
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

	applySchema(t, db)
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
		defer res.Body.Close()
		require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	}

	// 2) Forbidden for non-admin
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer res.Body.Close()
		require.Equal(t, http.StatusForbidden, res.StatusCode)
	}

	// 3) OK for admin list
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		res := doReq(t, httpClient, req)
		defer res.Body.Close()
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
		defer res.Body.Close()
		require.Equal(t, http.StatusCreated, res.StatusCode)

		// This assumes your response shape is:
		// { "data": { "trainer": { "id": "..." } } }
		var createResp struct {
			Data struct {
				Trainer struct {
					ID string `json:"id"`
				} `json:"trainer"`
			} `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&createResp))
		require.NotEmpty(t, createResp.Data.Trainer.ID)

		trainerID = createResp.Data.Trainer.ID
	}

	// 5) Get by id
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers/"+trainerID, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		res := doReq(t, httpClient, req)
		defer res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)
	}

	// 6) Update
	{
		updateBody := map[string]any{
			"onboarding_status":  "approved",
			"calendly_connected": true,
			"calendly_link":      "https://calendly.com/trainer-one/intro",
		}

		b, err := json.Marshal(updateBody)
		require.NoError(t, err)

		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/trainers/"+trainerID, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")

		res := doReq(t, httpClient, req)
		defer res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)
	}

	// 7) Delete (your handler returns 204)
	{
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/trainers/"+trainerID, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		res := doReq(t, httpClient, req)
		defer res.Body.Close()
		require.Equal(t, http.StatusNoContent, res.StatusCode)
	}
}