package routes_test

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/routes"
)

type discoveryBookingResponseBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
	Data    struct {
		BookingID       string `json:"booking_id"`
		TrainerID       string `json:"trainer_id"`
		ClientID        string `json:"client_id"`
		SlotID          string `json:"slot_id"`
		MeetingID       string `json:"meeting_id"`
		MeetingJoinURL  string `json:"meeting_join_url"`
		MeetingStartURL string `json:"meeting_start_url"`
		BookingStatus   string `json:"booking_status"`
		SessionPlatform string `json:"session_platform"`
	} `json:"data"`
}

type discoveryTestEnv struct {
	db            *sql.DB
	server        *httptest.Server
	httpClient    *http.Client
	clientToken   string
	otherToken    string
	clientUserID  string
	otherClientID string
	trainerID     string
}

func TestBookDiscoveryCallHappyPath(t *testing.T) {
	zoom := fakeZoomServer(t, true)
	env := setupDiscoveryTestEnv(t, zoom.URL, 2)

	slotID := insertBookingSlot(t, env.db, env.trainerID, "available")

	res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/bookings/discovery", env.clientToken, map[string]any{
		"trainer_id": env.trainerID,
		"slot_id":    slotID,
		"timezone":   "UTC",
	})
	defer func() { _ = res.Body.Close() }()

	require.Equal(t, http.StatusCreated, res.StatusCode)

	var body discoveryBookingResponseBody
	decodeJSON(t, res, &body)
	require.Equal(t, "CREATED", body.Code)
	require.Equal(t, "success", body.Status)
	require.Equal(t, slotID, body.Data.SlotID)
	require.Equal(t, env.trainerID, body.Data.TrainerID)
	require.Equal(t, env.clientUserID, body.Data.ClientID)
	require.Equal(t, "booked", body.Data.BookingStatus)
	require.Equal(t, "zoom", body.Data.SessionPlatform)
	require.NotEmpty(t, body.Data.MeetingJoinURL)
	require.NotEmpty(t, body.Data.MeetingStartURL)
}

func TestBookDiscoveryCallFreeTrialUsedReturns403(t *testing.T) {
	zoom := fakeZoomServer(t, true)
	env := setupDiscoveryTestEnv(t, zoom.URL, 2)

	slotID := insertBookingSlot(t, env.db, env.trainerID, "available")
	insertExistingDiscoveryBooking(t, env.db, env.trainerID, env.clientUserID)

	res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/bookings/discovery", env.clientToken, map[string]any{
		"trainer_id": env.trainerID,
		"slot_id":    slotID,
		"timezone":   "UTC",
	})
	defer func() { _ = res.Body.Close() }()

	require.Equal(t, http.StatusForbidden, res.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Equal(t, "FORBIDDEN", body["code"])
	meta, ok := body["meta"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, meta["upgrade_url"], "/pricing")
}

func TestBookDiscoveryCallSlotUnavailableReturns409(t *testing.T) {
	zoom := fakeZoomServer(t, true)
	env := setupDiscoveryTestEnv(t, zoom.URL, 2)

	slotID := insertBookingSlot(t, env.db, env.trainerID, "unavailable")

	res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/bookings/discovery", env.clientToken, map[string]any{
		"trainer_id": env.trainerID,
		"slot_id":    slotID,
		"timezone":   "UTC",
	})
	defer func() { _ = res.Body.Close() }()

	require.Equal(t, http.StatusConflict, res.StatusCode)
	var body errorResponseBody
	decodeJSON(t, res, &body)
	require.Equal(t, "CONFLICT", body.Code)
}

func TestBookDiscoveryCallZoomFailureReturns503(t *testing.T) {
	zoom := fakeZoomServer(t, false)
	env := setupDiscoveryTestEnv(t, zoom.URL, 2)

	slotID := insertBookingSlot(t, env.db, env.trainerID, "available")

	res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/bookings/discovery", env.clientToken, map[string]any{
		"trainer_id": env.trainerID,
		"slot_id":    slotID,
		"timezone":   "UTC",
	})
	defer func() { _ = res.Body.Close() }()

	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	var body errorResponseBody
	decodeJSON(t, res, &body)
	require.Equal(t, "SERVER_ERROR", body.Code)

	var slotStatus string
	err := env.db.QueryRow(`SELECT status FROM booking_slots WHERE id = $1`, slotID).Scan(&slotStatus)
	require.NoError(t, err)
	require.Equal(t, "available", slotStatus)
}

func TestBookDiscoveryCallConcurrentCollisionOnlyOneSucceeds(t *testing.T) {
	zoom := fakeZoomServer(t, true)
	env := setupDiscoveryTestEnv(t, zoom.URL, 2)

	slotID := insertBookingSlot(t, env.db, env.trainerID, "available")

	start := make(chan struct{})
	statuses := make(chan int, 2)

	call := func(token string) {
		<-start
		res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/bookings/discovery", token, map[string]any{
			"trainer_id": env.trainerID,
			"slot_id":    slotID,
			"timezone":   "UTC",
		})
		defer func() { _ = res.Body.Close() }()
		statuses <- res.StatusCode
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		call(env.clientToken)
	}()
	go func() {
		defer wg.Done()
		call(env.otherToken)
	}()

	close(start)
	wg.Wait()
	close(statuses)

	var successCount int
	var conflictCount int
	for status := range statuses {
		if status == http.StatusCreated {
			successCount++
		}
		if status == http.StatusConflict {
			conflictCount++
		}
	}

	require.Equal(t, 1, successCount)
	require.Equal(t, 1, conflictCount)
}

func setupDiscoveryTestEnv(t *testing.T, zoomBaseURL string, retryAttempts int) discoveryTestEnv {
	t.Helper()

	dbConn, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	resetTables(t, dbConn)

	clientUserID := insertUser(t, dbConn, "client-discovery@example.com", "Discovery Client", "client")
	otherClientID := insertUser(t, dbConn, "client-discovery-2@example.com", "Discovery Client 2", "client")
	trainerUserID := insertUser(t, dbConn, "trainer-discovery@example.com", "Discovery Trainer", "trainer")
	trainerID := insertTrainer(t, dbConn, trainerUserID)

	server := newBookingServer(t, dbConn, zoomBaseURL, retryAttempts)
	t.Cleanup(server.Close)

	return discoveryTestEnv{
		db:            dbConn,
		server:        server,
		httpClient:    http.DefaultClient,
		clientToken:   tokenFor(t, clientUserID),
		otherToken:    tokenFor(t, otherClientID),
		clientUserID:  clientUserID,
		otherClientID: otherClientID,
		trainerID:     trainerID,
	}
}

func newBookingServer(t *testing.T, db *sql.DB, zoomBaseURL string, retryAttempts int) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		Env:                  "test",
		FrontendURL:          "http://localhost:3000",
		ZoomAccountID:        "test-account-id",
		ZoomClientID:         "test-client-id",
		ZoomClientSecret:     "test-client-secret",
		ZoomUserID:           "me",
		ZoomTokenURL:         zoomBaseURL + "/oauth/token",
		ZoomAPIBaseURL:       zoomBaseURL + "/v2",
		ZoomRetryMaxAttempts: retryAttempts,
		ZoomRetryBaseDelayMS: 1,
		ZoomRetryMaxDelayMS:  2,
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := routes.New(cfg, log, db, nil).Routes()
	return httptest.NewServer(r)
}

func fakeZoomServer(t *testing.T, createMeetingSuccess bool) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token"}`))
		case "/v2/users/me/meetings":
			if !createMeetingSuccess {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"temporary zoom outage"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"123456789","join_url":"https://zoom.us/j/123456789","start_url":"https://zoom.us/s/123456789","password":"secret"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func insertBookingSlot(t *testing.T, db *sql.DB, trainerID, status string) string {
	t.Helper()

	startsAt := time.Now().UTC().Add(90 * time.Minute)
	endsAt := startsAt.Add(30 * time.Minute)

	var slotID string
	err := db.QueryRow(`
INSERT INTO booking_slots (trainer_id, starts_at, ends_at, timezone, status)
VALUES ($1, $2, $3, 'UTC', $4)
RETURNING id
`, trainerID, startsAt, endsAt, status).Scan(&slotID)
	require.NoError(t, err)

	return slotID
}

func insertExistingDiscoveryBooking(t *testing.T, db *sql.DB, trainerID, clientID string) {
	t.Helper()

	_, err := db.Exec(`
INSERT INTO bookings (trainer_id, client_id, is_discovery_call, booking_status)
VALUES ($1, $2, true, 'booked')
`, trainerID, clientID)
	require.NoError(t, err)
}
