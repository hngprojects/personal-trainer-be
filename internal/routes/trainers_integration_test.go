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

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	_, err := db.Exec(q, args...)
	require.NoError(t, err)
}

func resetTables(t *testing.T, db *sql.DB) {
	t.Helper()

	// Requires the DB has already been migrated via goose.
	// CASCADE handles FKs between users/trainers/sessions/etc.
	mustExec(t, db, `TRUNCATE TABLE trainer_availability RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE trainer_invite_tokens RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE login_security RESTART IDENTITY CASCADE;`)

	// If your schema has roles/user_roles tables, clear them too.
	// Using DO blocks so tests don't fail if a table doesn't exist in a particular branch.
	mustExec(t, db, `
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'user_roles') THEN
    EXECUTE 'TRUNCATE TABLE user_roles RESTART IDENTITY CASCADE;';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'roles') THEN
    EXECUTE 'TRUNCATE TABLE roles RESTART IDENTITY CASCADE;';
  END IF;
END$$;
`)

	mustExec(t, db, `TRUNCATE TABLE trainers RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE sessions RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE verification_codes RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE users RESTART IDENTITY CASCADE;`)
}

func ensureRole(t *testing.T, db *sql.DB, roleName string) {
	t.Helper()

	// Adjust column names if your roles table differs.
	// This version assumes roles(name) unique.
	mustExec(t, db, `
INSERT INTO roles (name)
VALUES ($1)
ON CONFLICT (name) DO NOTHING;
`, roleName)
}

func assignRole(t *testing.T, db *sql.DB, userID string, roleName string) {
	t.Helper()

	ensureRole(t, db, roleName)

	// This assumes user_roles(user_id, role_name) or (user_id, role_id) is different in your schema.
	// Most common is: user_roles(user_id, role_id) referencing roles(id).
	// We'll support the role_id variant (roles has id + name).
	mustExec(t, db, `
INSERT INTO user_roles (user_id, role_id)
SELECT $1, r.id
FROM roles r
WHERE r.name = $2
ON CONFLICT DO NOTHING;
`, userID, roleName)
}

func insertUser(t *testing.T, db *sql.DB, email, name string) string {
	t.Helper()

	var id string
	err := db.QueryRow(`
INSERT INTO users (email, name, auth_provider, is_active)
VALUES ($1, $2, 'local', true)
RETURNING id
`, email, name).Scan(&id)
	require.NoError(t, err)
	return id
}

func tokenFor(t *testing.T, userID string) string {
	t.Helper()

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

	// Users
	adminID := insertUser(t, db, "admin@example.com", "Admin")
	clientID := insertUser(t, db, "client@example.com", "Client")
	trainerUserID := insertUser(t, db, "trainer1@example.com", "Trainer One")

	// Roles (new auth model)
	assignRole(t, db, adminID, "admin")
	assignRole(t, db, clientID, "client")
	assignRole(t, db, trainerUserID, "trainer")

	// Create a trainer profile row (so trainers can be returned by /trainers, etc.)
	var trainerID string
	err := db.QueryRow(`
INSERT INTO trainers (user_id, specialization, bio, years_of_experience, calendly_connected, onboarding_status)
VALUES ($1, $2, $3, $4, false, 'pending')
RETURNING id
`, trainerUserID, "Weight loss", "Helping clients lose weight safely", 5).Scan(&trainerID)
	require.NoError(t, err)
	require.NotEmpty(t, trainerID)

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

	// 2) OK for client list (clients can view trainers)
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()

		require.Equal(t, http.StatusOK, res.StatusCode)
	}

	// 3) OK for admin list
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	}

	// 4) Get by id
	{
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/trainers/"+trainerID, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	}

	// 5) Trainer login endpoint exists (does not assert success because password setup flow may be required)
	// This ensures the route is wired and returns a "non-404" response.
	{
		body := map[string]any{
			"user_id":  trainerUserID,
			"password": "BadPassword1!", // expected to fail unless you set password hash
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/trainers/login", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")

		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()

		// Could be 401 (invalid creds) or 403 (trainer not active/role issues) or 423 (locked) depending on implementation.
		// We mainly want to ensure the endpoint is present.
		require.NotEqual(t, http.StatusNotFound, res.StatusCode)
	}
}
