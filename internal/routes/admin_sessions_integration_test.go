package routes_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// insertTrainerWithSpecs seeds a trainer row, picking specializations from
// the post-000037 array catalog. Distinct from the reviews_integration_test
// helper which still references the old singular column; this one matches
// the current schema and is what the admin/trainer session tests need.
func insertTrainerWithSpecs(t *testing.T, db *sql.DB, userID string, status string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
INSERT INTO trainers (user_id, specializations, training_styles, onboarding_status)
VALUES ($1, ARRAY['strength']::text[], ARRAY[]::text[], $2)
RETURNING id`, userID, status).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertSuperAdmin(t *testing.T, db *sql.DB, email, name string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
INSERT INTO users (email, name, auth_provider, role)
VALUES ($1, $2, 'local', 'super_admin')
RETURNING id`, email, name).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertDiscoveryBooking(t *testing.T, db *sql.DB, userID, name, email string, when time.Time) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
INSERT INTO discovery_bookings (user_id, name, email, contact_mode, selected_datetime, client_timezone)
VALUES ($1, $2, $3, 'zoom_meeting', $4, 'Africa/Lagos')
RETURNING id`, userID, name, email, when).Scan(&id)
	require.NoError(t, err)
	return id
}

// TestAdminAndTrainerListingEndpoints exercises the three new paginated
// listing endpoints + the trainer name on /trainers/{id}. Skipped unless
// RUN_INTEGRATION_TESTS=1 + a test DATABASE_URL is set (same gating as the
// other integration tests in this package).
func TestAdminAndTrainerListingEndpoints(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	resetTables(t, db)
	mustExec(t, db, `TRUNCATE TABLE booking_session RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE bookings        RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE discovery_bookings RESTART IDENTITY CASCADE;`)

	// Seed users
	superAdminID := insertSuperAdmin(t, db, "super@listing.test", "Super Admin")
	plainAdminID := insertUser(t, db, "admin@listing.test", "Plain Admin", "admin")
	clientID := insertUser(t, db, "client@listing.test", "Test Client", "client")
	trainerUserID := insertUser(t, db, "trainer@listing.test", "Coach Tester", "client")
	trainerID := insertTrainerWithSpecs(t, db, trainerUserID, "approved")

	// Seed three bookings with the same trainer/client so pagination + counts
	// have something to chew on. Each one gets a booking_session row except
	// the last, so we can verify session_id surfaces as NULL when absent.
	now := time.Now().UTC()
	bookingIDs := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		start := now.Add(time.Duration(i+1) * time.Hour)
		var bid string
		err := db.QueryRow(`
INSERT INTO bookings
  (trainer_id, client_id, scheduled_start, scheduled_end, timezone,
   booking_status, session_platform, created_at)
VALUES ($1, $2, $3, $4, 'Africa/Lagos', 'confirmed', 'google_meet', NOW())
RETURNING id`,
			trainerID, clientID, start, start.Add(time.Hour),
		).Scan(&bid)
		require.NoError(t, err)
		bookingIDs = append(bookingIDs, bid)
	}
	// session row for the first two only — the third stays sessionless.
	insertBookingSession(t, db, bookingIDs[0])
	insertBookingSession(t, db, bookingIDs[1])

	// Seed two discovery bookings for the admin discovery listing.
	insertDiscoveryBooking(t, db, clientID, "Test Client", "client@listing.test", now.Add(2*time.Hour))
	insertDiscoveryBooking(t, db, clientID, "Test Client", "client@listing.test", now.Add(48*time.Hour))

	superToken := tokenFor(t, superAdminID)
	adminToken := tokenFor(t, plainAdminID)
	clientToken := tokenFor(t, clientID)
	trainerToken := tokenFor(t, trainerUserID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)
	httpClient := http.DefaultClient
	apiBase := srv.URL + "/api/v1"

	// -----------------------------------------------------------------
	// GET /admin/sessions
	// -----------------------------------------------------------------
	t.Run("admin sessions: super_admin sees all bookings with names + session_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/admin/sessions?page=1&limit=2", nil)
		req.Header.Set("Authorization", "Bearer "+superToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var body struct {
			Data []map[string]any `json:"data"`
			Meta struct {
				Page       int `json:"page"`
				PerPage    int `json:"per_page"`
				TotalCount int `json:"total_count"`
				TotalPages int `json:"total_pages"`
			} `json:"meta"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
		require.Len(t, body.Data, 2, "page 1 with limit 2 should return 2 rows")
		require.Equal(t, 3, body.Meta.TotalCount, "should count all 3 seeded bookings")
		require.Equal(t, 2, body.Meta.TotalPages)

		// Names should be joined in.
		for _, row := range body.Data {
			require.Equal(t, "Coach Tester", row["trainer_name"])
			require.Equal(t, "Test Client", row["client_name"])
		}

		// session_id presence: at least one row in the first 2 (newest-first)
		// should have a session_id since 2/3 bookings got sessions.
		seenSession := 0
		for _, row := range body.Data {
			if _, ok := row["session_id"]; ok {
				seenSession++
			}
		}
		require.GreaterOrEqual(t, seenSession, 1, "at least one row should expose session_id")
	})

	t.Run("admin sessions: accepts plain admin too (not just super_admin)", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/admin/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	})

	t.Run("admin sessions: rejects non-admin (client role)", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/admin/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	t.Run("admin sessions: rejects unauthenticated", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/admin/sessions", nil)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("admin sessions: invalid pagination returns 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/admin/sessions?limit=500", nil)
		req.Header.Set("Authorization", "Bearer "+superToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	// -----------------------------------------------------------------
	// GET /admin/discovery-bookings
	// -----------------------------------------------------------------
	t.Run("admin discovery: super_admin sees all rows with total count", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/admin/discovery-bookings", nil)
		req.Header.Set("Authorization", "Bearer "+superToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var body struct {
			Data []map[string]any `json:"data"`
			Meta struct {
				TotalCount int `json:"total_count"`
			} `json:"meta"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
		require.Equal(t, 2, body.Meta.TotalCount)
		require.Len(t, body.Data, 2)
	})

	t.Run("admin discovery: accepts plain admin too", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/admin/discovery-bookings", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	})

	t.Run("admin discovery: rejects non-admin (client role)", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/admin/discovery-bookings", nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	// -----------------------------------------------------------------
	// GET /trainers/me/sessions
	// -----------------------------------------------------------------
	t.Run("trainer me sessions: returns trainer's own bookings with session_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/trainers/me/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var body struct {
			Data []map[string]any `json:"data"`
			Meta struct {
				TotalCount int `json:"total_count"`
			} `json:"meta"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
		require.Equal(t, 3, body.Meta.TotalCount)
		require.Len(t, body.Data, 3)
		for _, row := range body.Data {
			require.Equal(t, "Test Client", row["client_name"])
		}

		// At least one of the rows must expose session_id (2/3 have sessions).
		seenSession := 0
		for _, row := range body.Data {
			if _, ok := row["session_id"]; ok {
				seenSession++
			}
		}
		require.Equal(t, 2, seenSession, "exactly 2 bookings have associated sessions")
	})

	t.Run("trainer me sessions: 404 when caller has no trainer profile", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/trainers/me/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNotFound, res.StatusCode)
	})

	t.Run("trainer me sessions: 401 without token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/trainers/me/sessions", nil)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	// -----------------------------------------------------------------
	// GET /trainers — paginated + includes name
	// -----------------------------------------------------------------
	t.Run("get trainers: returns paginated list with name + email", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/trainers?page=1&limit=10", nil)
		req.Header.Set("Authorization", "Bearer "+superToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var body struct {
			Data []map[string]any `json:"data"`
			Meta struct {
				TotalCount int `json:"total_count"`
				Page       int `json:"page"`
			} `json:"meta"`
		}
		raw, _ := io.ReadAll(res.Body)
		require.NoError(t, json.Unmarshal(raw, &body), "response: %s", string(raw))
		require.GreaterOrEqual(t, body.Meta.TotalCount, 1)
		require.NotEmpty(t, body.Data)
		require.Equal(t, "Coach Tester", body.Data[0]["name"])
		require.Equal(t, "trainer@listing.test", body.Data[0]["email"])
	})

	// -----------------------------------------------------------------
	// GET /trainers/{id} — includes name
	// -----------------------------------------------------------------
	t.Run("get trainer by id: includes name + email + benefits", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/trainers/"+trainerID, nil)
		req.Header.Set("Authorization", "Bearer "+superToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var body struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
		require.Equal(t, "Coach Tester", body.Data["name"])
		require.Equal(t, "trainer@listing.test", body.Data["email"])
	})

	// Sanity: pagination math is correct (page 2 of limit 2 returns the
	// remaining 1 row). Putting this last so the earlier assertions own
	// the ergonomic happy-path checks.
	t.Run("admin sessions: page 2 returns remainder", func(t *testing.T) {
		url := fmt.Sprintf("%s/admin/sessions?page=2&limit=2", apiBase)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+superToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var body struct {
			Data []map[string]any `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
		require.Len(t, body.Data, 1, "3 rows, limit 2, page 2 should return 1")
	})
}
