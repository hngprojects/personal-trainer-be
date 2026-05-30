package routes_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAdminDashboardEndpoints exercises the three admin dashboard endpoints:
//   GET /admin/user/trainer/count
//   GET /admin/active/subscriptions/count
//   GET /admin/revenue
func TestAdminDashboardEndpoints(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	resetTables(t, db)
	mustExec(t, db, `TRUNCATE TABLE subscriptions RESTART IDENTITY CASCADE;`)

	// ── seed ────────────────────────────────────────────────────────────────
	superAdminID := insertSuperAdmin(t, db, "superadmin@dashboard.test", "Super Admin")
	clientID1    := insertUser(t, db, "client1@dashboard.test", "Client One", "client")
	clientID2    := insertUser(t, db, "client2@dashboard.test", "Client Two", "client")
	trainerUserID := insertUser(t, db, "trainer@dashboard.test", "Trainer One", "client")

	var trainerID string
	err := db.QueryRow(`
		INSERT INTO trainers (user_id, specializations, training_styles, onboarding_status)
		VALUES ($1, ARRAY['strength']::text[], ARRAY[]::text[], 'approved')
		RETURNING id`, trainerUserID).Scan(&trainerID)
	require.NoError(t, err)

	now := time.Now().UTC()

	// active subscription (monthly_12)
	_, err = db.Exec(`
		INSERT INTO subscriptions
		      (client_id, trainer_id, plan_type, amount, currency, status,
		       current_period_start, current_period_end)
		VALUES ($1, $2, 'monthly_12', 8000, 'USD', 'active', $3, $4)`,
		clientID1, trainerID,
		now.Add(-15*24*time.Hour), now.Add(15*24*time.Hour))
	require.NoError(t, err)

	// expired subscription (single / one-time) — counts in revenue, not in active
	_, err = db.Exec(`
		INSERT INTO subscriptions
		      (client_id, trainer_id, plan_type, amount, currency, status,
		       current_period_start, current_period_end)
		VALUES ($1, $2, 'single', 2000, 'USD', 'expired', $3, $4)`,
		clientID2, trainerID,
		now.Add(-60*24*time.Hour), now.Add(-30*24*time.Hour))
	require.NoError(t, err)

	superToken  := tokenFor(t, superAdminID)
	clientToken := tokenFor(t, clientID1)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)
	hc := http.DefaultClient

	getJSON := func(path, token string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1"+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err2 := hc.Do(req)
		require.NoError(t, err2)
		defer func() { _ = res.Body.Close() }()
		raw, _ := io.ReadAll(res.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}

	// ── GET /admin/user/trainer/count ────────────────────────────────────────
	t.Run("user_trainer_count/200", func(t *testing.T) {
		code, body := getJSON("/admin/user/trainer/count", superToken)
		require.Equal(t, http.StatusOK, code)
		data := body["data"].(map[string]any)
		// superAdmin is not a client role; clientID1, clientID2, trainerUserID are all 'client'
		require.EqualValues(t, 3, data["total_clients"])
		require.EqualValues(t, 1, data["total_approved_trainers"])
	})

	t.Run("user_trainer_count/401_no_token", func(t *testing.T) {
		code, _ := getJSON("/admin/user/trainer/count", "")
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("user_trainer_count/403_client", func(t *testing.T) {
		code, _ := getJSON("/admin/user/trainer/count", clientToken)
		require.Equal(t, http.StatusForbidden, code)
	})

	// ── GET /admin/active/subscriptions/count ────────────────────────────────
	t.Run("active_subscriptions/200", func(t *testing.T) {
		code, body := getJSON("/admin/active/subscriptions/count", superToken)
		require.Equal(t, http.StatusOK, code)
		data := body["data"].(map[string]any)
		require.EqualValues(t, 1, data["active_subscriptions"]) // only the non-expired one
	})

	t.Run("active_subscriptions/401_no_token", func(t *testing.T) {
		code, _ := getJSON("/admin/active/subscriptions/count", "")
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("active_subscriptions/403_client", func(t *testing.T) {
		code, _ := getJSON("/admin/active/subscriptions/count", clientToken)
		require.Equal(t, http.StatusForbidden, code)
	})

	// ── GET /admin/revenue ───────────────────────────────────────────────────
	t.Run("revenue/200", func(t *testing.T) {
		code, body := getJSON("/admin/revenue", superToken)
		require.Equal(t, http.StatusOK, code)
		data := body["data"].(map[string]any)

		rev := data["revenue"].(map[string]any)
		require.EqualValues(t, 10000, rev["total"])
		require.EqualValues(t, 8000, rev["subscriptions"])
		require.EqualValues(t, 2000, rev["one_time_sessions"])
		require.EqualValues(t, 0, rev["trial_conversions"])

		latest := data["latest_payment"].(map[string]any)
		require.NotEmpty(t, latest["id"])
		require.NotEmpty(t, latest["status"])
	})

	t.Run("revenue/401_no_token", func(t *testing.T) {
		code, _ := getJSON("/admin/revenue", "")
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("revenue/403_client", func(t *testing.T) {
		code, _ := getJSON("/admin/revenue", clientToken)
		require.Equal(t, http.StatusForbidden, code)
	})
}
