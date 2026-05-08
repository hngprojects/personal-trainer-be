package routes_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
	"io"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/routes"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	ctx := context.Background()

	pg, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
	)
	require.NoError(t, err)

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)

	// Wait until db is ready
	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 10*time.Second, 200*time.Millisecond)

	cleanup := func() {
		_ = db.Close()
		_ = pg.Terminate(ctx)
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

	// auth.ValidateToken() in your middleware expects the same signing secret used by auth.GenerateJWTToken.
	// Ensure JWT secret is set for the test process.
	if os.Getenv("JWT_SECRET") == "" {
		_ = os.Setenv("JWT_SECRET", "test-secret")
	}

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
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := routes.New(cfg, log, db).Routes()
	return httptest.NewServer(r)
}

func TestTrainersEndpoints(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	applySchema(t, db)

	adminID := insertUser(t, db, "admin@example.com", "Admin", "admin")
	clientID := insertUser(t, db, "client@example.com", "Client", "client")
	trainerUserID := insertUser(t, db, "trainer1@example.com", "Trainer One", "client")

	adminToken := tokenFor(t, adminID)
	clientToken := tokenFor(t, clientID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)

	// 1) Unauthorized without token
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)

	// 2) Forbidden for non-admin
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
	req.Header.Set("Authorization", "Bearer "+clientToken)
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, res.StatusCode)

	// 3) OK for admin list (empty)
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	// 4) Create trainer
	createBody := map[string]any{
		"user_id":             trainerUserID,
		"specialization":      "Weight loss",
		"bio":                 "Helping clients lose weight safely",
		"years_of_experience": 5,
		"calendly_connected":  false,
		"calendly_link":       "",
		"onboarding_status":   "pending",
	}
	b, _ := json.Marshal(createBody)
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/api/v1/trainers", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, res.StatusCode)

	var createResp struct {
		Data struct {
			Trainer struct {
				ID string `json:"id"`
			} `json:"trainer"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&createResp))
	require.NotEmpty(t, createResp.Data.Trainer.ID)
	trainerID := createResp.Data.Trainer.ID

	// 5) Get by id
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers/"+trainerID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	// 6) Update
	updateBody := map[string]any{
		"onboarding_status":  "approved",
		"calendly_connected": true,
		"calendly_link":      "https://calendly.com/trainer-one/intro",
	}
	b, _ = json.Marshal(updateBody)
	req, _ = http.NewRequest(http.MethodPut, srv.URL+"/api/v1/trainers/"+trainerID, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	// 7) Delete
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/trainers/"+trainerID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
}