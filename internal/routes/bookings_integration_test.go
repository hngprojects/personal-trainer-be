package routes_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// insertBookingSlotForTime seeds a booking_slot whose day/start_time/end_time match the
// given scheduled start (and start+1h end) in Africa/Lagos. The slot is marked inactive
// (is_active=false) so it looks "booked" — cancel will flip it back to true.
func insertBookingSlotForTime(t *testing.T, db *sql.DB, trainerID string, start time.Time) {
	t.Helper()
	_, err := db.Exec(`
INSERT INTO booking_slots
    (trainer_id, day_of_week, start_time, end_time, timezone, is_active)
VALUES (
    $1,
    EXTRACT(DOW FROM $2::TIMESTAMPTZ AT TIME ZONE 'Africa/Lagos')::SMALLINT,
    ($2::TIMESTAMPTZ AT TIME ZONE 'Africa/Lagos')::TIME,
    ($3::TIMESTAMPTZ AT TIME ZONE 'Africa/Lagos')::TIME,
    'Africa/Lagos',
    false
)`,
		trainerID, start, start.Add(time.Hour),
	)
	require.NoError(t, err)
}

// insertConfirmedBooking seeds a booking with scheduled_start/end for cancel & reschedule tests.
func insertConfirmedBooking(t *testing.T, db *sql.DB, trainerID, clientID, subID string, start time.Time) string {
	t.Helper()
	insertBookingSlotForTime(t, db, trainerID, start)
	var id string
	err := db.QueryRow(`
INSERT INTO bookings
    (trainer_id, client_id, subscription_id,
     scheduled_start, scheduled_end, timezone,
     booking_status, session_platform, created_at)
VALUES ($1, $2, $3, $4, $5, 'Africa/Lagos', 'confirmed', 'google_meet', NOW())
RETURNING id`,
		trainerID, clientID, subID, start, start.Add(time.Hour),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// insertSubscription seeds an active subscription.
func insertSubscription(t *testing.T, db *sql.DB, clientID, trainerID string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
INSERT INTO subscriptions
    (client_id, trainer_id, plan_type, sessions_per_month, amount,
     currency, status, current_period_start, current_period_end)
VALUES ($1, $2, 'monthly_12', 8, 10000, 'USD', 'active', $3, $4)
RETURNING id`,
		clientID, trainerID,
		time.Now().UTC(),
		time.Now().UTC().AddDate(0, 1, 0),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// insertBookingSession seeds a booking_session row for a given booking.
func insertBookingSession(t *testing.T, db *sql.DB, bookingID string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
INSERT INTO booking_session (booking_id, status) VALUES ($1, 'booked') RETURNING id`,
		bookingID,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// clearBookingTables truncates booking-specific tables while preserving users/trainers.
func clearBookingTables(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `TRUNCATE TABLE booking_session RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE bookings        RESTART IDENTITY CASCADE;`)
	mustExec(t, db, `TRUNCATE TABLE subscriptions   RESTART IDENTITY CASCADE;`)
}

// ---------------------------------------------------------------------------
// TestBookingIntegration — full booking lifecycle over real HTTP
// ---------------------------------------------------------------------------

func TestBookingIntegration(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	resetTables(t, db)
	clearBookingTables(t, db)

	// seed users & trainer
	clientID := insertUser(t, db, "client@booking.test", "Client User", "client")
	trainerUserID := insertUser(t, db, "trainer@booking.test", "Trainer User", "client")
	trainerID := insertTrainer(t, db, trainerUserID)
	subID := insertSubscription(t, db, clientID, trainerID)

	clientToken := tokenFor(t, clientID)
	trainerToken := tokenFor(t, trainerUserID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)
	httpClient := http.DefaultClient
	apiBase := srv.URL + "/api/v1"

	// ================================================================
	// POST /bookings — create booking
	// ================================================================
	t.Run("create booking unauthenticated → 401", func(t *testing.T) {
		start := time.Now().UTC().Add(25 * time.Hour)
		body, _ := json.Marshal(map[string]interface{}{
			"trainer_id":       trainerID,
			"subscription_id":  subID,
			"scheduled_start":  start.Format(time.RFC3339),
			"scheduled_end":    start.Add(time.Hour).Format(time.RFC3339),
			"session_platform": "google_meet",
			"timezone":         "Africa/Lagos",
		})
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/bookings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("create booking missing trainer_id → 400", func(t *testing.T) {
		start := time.Now().UTC().Add(25 * time.Hour)
		body, _ := json.Marshal(map[string]interface{}{
			"subscription_id":  subID,
			"scheduled_start":  start.Format(time.RFC3339),
			"scheduled_end":    start.Add(time.Hour).Format(time.RFC3339),
			"session_platform": "google_meet",
			"timezone":         "Africa/Lagos",
		})
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/bookings", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("create booking invalid platform → 400", func(t *testing.T) {
		start := time.Now().UTC().Add(25 * time.Hour)
		body, _ := json.Marshal(map[string]interface{}{
			"trainer_id":       trainerID,
			"subscription_id":  subID,
			"scheduled_start":  start.Format(time.RFC3339),
			"scheduled_end":    start.Add(time.Hour).Format(time.RFC3339),
			"session_platform": "carrier_pigeon",
			"timezone":         "Africa/Lagos",
		})
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/bookings", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("create booking success → 200", func(t *testing.T) {
		start := time.Now().UTC().Add(25 * time.Hour)
		body, _ := json.Marshal(map[string]interface{}{
			"trainer_id":       trainerID,
			"subscription_id":  subID,
			"scheduled_start":  start.Format(time.RFC3339),
			"scheduled_end":    start.Add(time.Hour).Format(time.RFC3339),
			"session_platform": "google_meet",
			"timezone":         "Africa/Lagos",
		})
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/bookings", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	})

	// ================================================================
	// PUT /bookings/:id/cancel
	// ================================================================
	t.Run("cancel booking not found → 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"reason": "other"})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/bookings/%s/cancel", apiBase, uuid.New()),
			bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNotFound, res.StatusCode)
	})

	t.Run("cancel booking wrong user → 403", func(t *testing.T) {
		bookingID := insertConfirmedBooking(t, db, trainerID, clientID, subID, time.Now().UTC().Truncate(24*time.Hour).Add(2*24*time.Hour+10*time.Hour))
		body, _ := json.Marshal(map[string]interface{}{"reason": "other"})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/bookings/%s/cancel", apiBase, bookingID),
			bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+trainerToken) // trainer != client
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	t.Run("cancel booking success → 200", func(t *testing.T) {
		bookingID := insertConfirmedBooking(t, db, trainerID, clientID, subID, time.Now().UTC().Truncate(24*time.Hour).Add(3*24*time.Hour+10*time.Hour))
		body, _ := json.Marshal(map[string]interface{}{"reason": "schedule_conflict"})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/bookings/%s/cancel", apiBase, bookingID),
			bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		require.Equal(t, "success", resp["status"])
	})

	t.Run("cancel already-cancelled booking → 409", func(t *testing.T) {
		bookingID := insertConfirmedBooking(t, db, trainerID, clientID, subID, time.Now().UTC().Truncate(24*time.Hour).Add(4*24*time.Hour+10*time.Hour))
		// cancel once
		body, _ := json.Marshal(map[string]interface{}{"reason": "other"})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/bookings/%s/cancel", apiBase, bookingID),
			bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", "application/json")
		_ = doReq(t, httpClient, req).Body.Close()

		// cancel again → conflict
		req2, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/bookings/%s/cancel", apiBase, bookingID),
			bytes.NewReader(body))
		req2.Header.Set("Authorization", "Bearer "+clientToken)
		req2.Header.Set("Content-Type", "application/json")
		res2 := doReq(t, httpClient, req2)
		defer func() { _ = res2.Body.Close() }()
		require.Equal(t, http.StatusConflict, res2.StatusCode)
	})

	// ================================================================
	// PUT /bookings/:id/reschedule
	// ================================================================
	t.Run("reschedule booking unauthenticated → 401", func(t *testing.T) {
		bookingID := insertConfirmedBooking(t, db, trainerID, clientID, subID, time.Now().UTC().Truncate(24*time.Hour).Add(5*24*time.Hour+10*time.Hour))
		body, _ := json.Marshal(map[string]interface{}{
			"new_datetime": time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339),
			"reason":       "work_conflict",
			"timezone":     "Africa/Lagos",
		})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/bookings/%s/reschedule", apiBase, bookingID),
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("reschedule booking success → 200", func(t *testing.T) {
		bookingID := insertConfirmedBooking(t, db, trainerID, clientID, subID, time.Now().UTC().Truncate(24*time.Hour).Add(6*24*time.Hour+10*time.Hour))
		body, _ := json.Marshal(map[string]interface{}{
			"new_datetime": time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339),
			"reason":       "work_conflict",
			"timezone":     "Africa/Lagos",
		})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/bookings/%s/reschedule", apiBase, bookingID),
			bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		require.Equal(t, "success", resp["status"])
	})

	// ================================================================
	// Session lifecycle: GET → start → join → complete → notes
	// ================================================================
	bookingID := insertConfirmedBooking(t, db, trainerID, clientID, subID, time.Now().UTC().Truncate(24*time.Hour).Add(24*time.Hour+10*time.Hour))
	sessionID := insertBookingSession(t, db, bookingID)

	t.Run("get session not found → 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet,
			fmt.Sprintf("%s/sessions/%s", apiBase, uuid.New()), nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNotFound, res.StatusCode)
	})

	t.Run("get session → 200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet,
			fmt.Sprintf("%s/sessions/%s", apiBase, sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		require.Equal(t, "success", resp["status"])
	})

	t.Run("join session before trainer starts → 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/join", apiBase, sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("complete session before starting → 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/complete", apiBase, sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("notes on incomplete session → 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"note": "too early"})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/notes", apiBase, sessionID),
			bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("start session → 200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/start", apiBase, sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		data, _ := resp["data"].(map[string]interface{})
		require.Equal(t, "started", data["status"])
	})

	t.Run("start session again → 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/start", apiBase, sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("join session → 200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/join", apiBase, sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		data, _ := resp["data"].(map[string]interface{})
		require.Equal(t, "in-session", data["status"])
	})

	t.Run("complete session → 200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/complete", apiBase, sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		data, _ := resp["data"].(map[string]interface{})
		require.Equal(t, "completed", data["status"])
	})

	t.Run("trainer adds notes after completion → 200", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"note": "Client showed great improvement in form today.",
		})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/notes", apiBase, sessionID),
			bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	})

	t.Run("empty note body → 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"note": ""})
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/sessions/%s/notes", apiBase, sessionID),
			bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	// ================================================================
	// GET /booking-slots/:trainerId
	// ================================================================
	t.Run("get trainer booking slots → 200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet,
			fmt.Sprintf("%s/booking-slots/%s", apiBase, trainerID), nil)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	})
}
