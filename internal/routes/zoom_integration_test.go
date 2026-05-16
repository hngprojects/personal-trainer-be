package routes_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/routes"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

const zoomEndpoint = "/api/v1/integrations/zoom/create-meeting"

type zoomResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
	Data    *struct {
		BookingID string `json:"booking_id"`
		Existing  bool   `json:"existing"`
		JoinURL   string `json:"join_url"`
		MeetingID string `json:"meeting_id"`
		Passcode  string `json:"passcode"`
	} `json:"data"`
}

type mockMeetingProvider struct {
	joinURL   string
	meetingID string
	passcode  string
}

func (m mockMeetingProvider) IsConfigured() bool { return true }
func (m mockMeetingProvider) CreateMeeting(_ context.Context, _ string, _ time.Time, _ int) (string, string, string, error) {
	return m.joinURL, m.meetingID, m.passcode, nil
}
func (m mockMeetingProvider) DeleteMeeting(_ context.Context, _ string) error { return nil }

func newServerWithMeeting(t *testing.T, db *sql.DB, p meeting.Provider) *httptest.Server {
	t.Helper()
	cfg := &config.Config{Env: "test", FrontendURL: "http://localhost:3000"}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := routes.New(cfg, log, db, nil).WithMeeting(p)
	return httptest.NewServer(r.Routes())
}

func TestCreateZoomMeeting(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	resetTables(t, db)

	clientUserID := insertUser(t, db, "zoom-client@example.com", "Client", "client")
	trainerUserID := insertUser(t, db, "zoom-trainer@example.com", "Trainer", "client")
	trainerID := insertTrainer(t, db, trainerUserID)
	clientToken := tokenFor(t, clientUserID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)
	hc := http.DefaultClient

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		res := doJSONRequest(t, hc, http.MethodPost, srv.URL+zoomEndpoint, "", map[string]any{
			"booking_id":   uuid.New(),
			"booking_type": "paid_session",
		})
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("omitted booking_id returns 400", func(t *testing.T) {
		res := doJSONRequest(t, hc, http.MethodPost, srv.URL+zoomEndpoint, clientToken, map[string]any{
			"booking_type": "paid_session",
		})
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		var body zoomResponse
		decodeJSON(t, res, &body)
		require.Equal(t, "BAD_REQUEST", body.Code)
	})

	t.Run("invalid booking_type returns 400", func(t *testing.T) {
		res := doJSONRequest(t, hc, http.MethodPost, srv.URL+zoomEndpoint, clientToken, map[string]any{
			"booking_id":   uuid.New(),
			"booking_type": "unknown_type",
		})
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		var body zoomResponse
		decodeJSON(t, res, &body)
		require.Equal(t, "BAD_REQUEST", body.Code)
	})

	t.Run("unknown paid_session booking returns 404", func(t *testing.T) {
		res := doJSONRequest(t, hc, http.MethodPost, srv.URL+zoomEndpoint, clientToken, map[string]any{
			"booking_id":   uuid.New(),
			"booking_type": "paid_session",
		})
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNotFound, res.StatusCode)
		var body zoomResponse
		decodeJSON(t, res, &body)
		require.Equal(t, "NOT_FOUND", body.Code)
	})

	t.Run("paid_session with no zoom creds returns 503", func(t *testing.T) {
		bookingID := insertBooking(t, db, trainerID, clientUserID, "confirmed", timePtr(time.Now().Add(24*time.Hour)))
		res := doJSONRequest(t, hc, http.MethodPost, srv.URL+zoomEndpoint, clientToken, map[string]any{
			"booking_id":   bookingID,
			"booking_type": "paid_session",
		})
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
		var body zoomResponse
		decodeJSON(t, res, &body)
		require.Equal(t, "SERVER_ERROR", body.Code)
	})

	t.Run("paid_session with existing meeting returns 200 and existing=true", func(t *testing.T) {
		bookingID := insertBookingWithZoom(t, db, trainerID, clientUserID, "confirmed",
			"https://zoom.us/j/99999999", "99999999", "secret1")
		res := doJSONRequest(t, hc, http.MethodPost, srv.URL+zoomEndpoint, clientToken, map[string]any{
			"booking_id":   bookingID,
			"booking_type": "paid_session",
		})
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		var body zoomResponse
		decodeJSON(t, res, &body)
		require.Equal(t, "OK", body.Code)
		require.NotNil(t, body.Data)
		require.True(t, body.Data.Existing)
		require.Equal(t, "https://zoom.us/j/99999999", body.Data.JoinURL)
		require.Equal(t, "99999999", body.Data.MeetingID)
		require.Equal(t, "secret1", body.Data.Passcode)
	})

	t.Run("paid_session new meeting creation returns 201", func(t *testing.T) {
		mock := mockMeetingProvider{
			joinURL:   "https://zoom.us/j/mock999",
			meetingID: "mock999",
			passcode:  "mockpass",
		}
		mockSrv := newServerWithMeeting(t, db, mock)
		t.Cleanup(mockSrv.Close)

		// Use a booking with scheduled_start set so EnsurePaidSessionMeeting can compute duration.
		bookingID := insertScheduledBooking(t, db, trainerID, clientUserID, "confirmed",
			time.Now().Add(48*time.Hour), time.Now().Add(49*time.Hour))
		res := doJSONRequest(t, hc, http.MethodPost, mockSrv.URL+zoomEndpoint, clientToken, map[string]any{
			"booking_id":   bookingID,
			"booking_type": "paid_session",
		})
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusCreated, res.StatusCode)
		var body zoomResponse
		decodeJSON(t, res, &body)
		require.Equal(t, "CREATED", body.Code)
		require.NotNil(t, body.Data)
		require.False(t, body.Data.Existing)
		require.Equal(t, "https://zoom.us/j/mock999", body.Data.JoinURL)
		require.Equal(t, "mock999", body.Data.MeetingID)
		require.Equal(t, "mockpass", body.Data.Passcode)
	})
}

func insertScheduledBooking(t *testing.T, db *sql.DB, trainerID, clientUserID, status string, start, end time.Time) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
INSERT INTO bookings (trainer_id, client_id, booking_status, scheduled_start, scheduled_end)
VALUES ($1, $2, $3, $4, $5)
RETURNING id
`, trainerID, clientUserID, status, start.UTC(), end.UTC()).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertBookingWithZoom(t *testing.T, db *sql.DB, trainerID, clientUserID, status, joinURL, meetingID, passcode string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
INSERT INTO bookings (trainer_id, client_id, booking_status, zoom_meeting_link, zoom_meeting_id, zoom_passcode)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id
`, trainerID, clientUserID, status, joinURL, meetingID, passcode).Scan(&id)
	require.NoError(t, err)
	return id
}
