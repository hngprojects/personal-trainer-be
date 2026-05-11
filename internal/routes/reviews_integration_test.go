package routes_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type reviewResponseBody struct {
	Code    string            `json:"code"`
	Data    reviewResponseDTO `json:"data"`
	Message string            `json:"message"`
	Status  string            `json:"status"`
}

type reviewResponseDTO struct {
	BookingID    string    `json:"booking_id"`
	ClientUserID string    `json:"client_user_id"`
	CreatedAt    time.Time `json:"created_at"`
	ID           string    `json:"id"`
	Rating       int       `json:"rating"`
	Review       *string   `json:"review"`
	TrainerID    string    `json:"trainer_id"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type reviewsListResponseBody struct {
	Code    string              `json:"code"`
	Data    []reviewResponseDTO `json:"data"`
	Message string              `json:"message"`
	Meta    struct {
		HasMore    bool    `json:"has_more"`
		NextCursor *string `json:"next_cursor"`
	} `json:"meta"`
	Status string `json:"status"`
}

type errorResponseBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type reviewTestEnv struct {
	db           *sql.DB
	server       *httptest.Server
	httpClient   *http.Client
	clientToken  string
	clientUserID string
	otherUserID  string
	trainerID    string
}

func TestCreateReviewCreatesReviewAndRefreshesTrainerStats(t *testing.T) {
	env := setupReviewTestEnv(t)
	bookingID := insertBooking(t, env.db, env.trainerID, env.clientUserID, "completed", timePtr(time.Now().UTC()))

	payload := map[string]any{
		"booking_id": bookingID,
		"rating":     5,
		"review":     "Excellent session",
	}

	res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/reviews", env.clientToken, payload)
	defer func() { _ = res.Body.Close() }()

	require.Equal(t, http.StatusCreated, res.StatusCode)

	var body reviewResponseBody
	decodeJSON(t, res, &body)
	require.Equal(t, "CREATED", body.Code)
	require.Equal(t, "success", body.Status)
	require.Equal(t, bookingID, body.Data.BookingID)
	require.Equal(t, env.trainerID, body.Data.TrainerID)
	require.Equal(t, env.clientUserID, body.Data.ClientUserID)
	require.Equal(t, 5, body.Data.Rating)
	require.NotNil(t, body.Data.Review)
	require.Equal(t, "Excellent session", *body.Data.Review)

	avg, total := trainerReviewStats(t, env.db, env.trainerID)
	require.InDelta(t, 5.0, avg, 0.001)
	require.Equal(t, 1, total)
}

func TestCreateReviewRejectsValidationOwnershipAndDuplicates(t *testing.T) {
	t.Run("invalid rating", func(t *testing.T) {
		env := setupReviewTestEnv(t)
		bookingID := insertBooking(t, env.db, env.trainerID, env.clientUserID, "completed", timePtr(time.Now().UTC()))

		res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/reviews", env.clientToken, map[string]any{
			"booking_id": bookingID,
			"rating":     0,
		})
		defer func() { _ = res.Body.Close() }()

		require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)

		var body errorResponseBody
		decodeJSON(t, res, &body)
		require.Equal(t, "INVALID_INPUT", body.Code)
		require.Equal(t, "rating must be between 1 and 5", body.Message)
	})

	t.Run("booking belongs to another client", func(t *testing.T) {
		env := setupReviewTestEnv(t)
		bookingID := insertBooking(t, env.db, env.trainerID, env.otherUserID, "completed", timePtr(time.Now().UTC()))

		res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/reviews", env.clientToken, map[string]any{
			"booking_id": bookingID,
			"rating":     4,
		})
		defer func() { _ = res.Body.Close() }()

		require.Equal(t, http.StatusForbidden, res.StatusCode)

		var body errorResponseBody
		decodeJSON(t, res, &body)
		require.Equal(t, "FORBIDDEN", body.Code)
		require.Equal(t, "booking does not belong to authenticated client", body.Message)
	})

	t.Run("booking is not completed", func(t *testing.T) {
		env := setupReviewTestEnv(t)
		bookingID := insertBooking(t, env.db, env.trainerID, env.clientUserID, "pending", nil)

		res := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/reviews", env.clientToken, map[string]any{
			"booking_id": bookingID,
			"rating":     4,
		})
		defer func() { _ = res.Body.Close() }()

		require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)

		var body errorResponseBody
		decodeJSON(t, res, &body)
		require.Equal(t, "INVALID_INPUT", body.Code)
		require.Equal(t, "booking is not completed", body.Message)
	})

	t.Run("duplicate review for booking", func(t *testing.T) {
		env := setupReviewTestEnv(t)
		bookingID := insertBooking(t, env.db, env.trainerID, env.clientUserID, "completed", timePtr(time.Now().UTC()))
		payload := map[string]any{
			"booking_id": bookingID,
			"rating":     5,
		}

		first := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/reviews", env.clientToken, payload)
		defer func() { _ = first.Body.Close() }()
		require.Equal(t, http.StatusCreated, first.StatusCode)

		second := doJSONRequest(t, env.httpClient, http.MethodPost, env.server.URL+"/api/v1/reviews", env.clientToken, payload)
		defer func() { _ = second.Body.Close() }()
		require.Equal(t, http.StatusConflict, second.StatusCode)

		var body errorResponseBody
		decodeJSON(t, second, &body)
		require.Equal(t, "CONFLICT", body.Code)
		require.Equal(t, "review already exists for this booking", body.Message)
	})
}

func TestGetTrainerReviewsSupportsPublicCursorPagination(t *testing.T) {
	env := setupReviewTestEnv(t)

	oldest := time.Date(2025, time.January, 10, 9, 0, 0, 0, time.UTC)
	middle := oldest.Add(2 * time.Hour)
	latest := oldest.Add(4 * time.Hour)

	insertReviewFixture(t, env.db, env.trainerID, env.clientUserID, 3, "oldest", oldest)
	insertReviewFixture(t, env.db, env.trainerID, env.clientUserID, 4, "middle", middle)
	insertReviewFixture(t, env.db, env.trainerID, env.clientUserID, 5, "latest", latest)

	firstPageURL := env.server.URL + "/api/v1/trainers/" + env.trainerID + "/reviews?limit=2"
	first := doReq(t, env.httpClient, newRequest(t, http.MethodGet, firstPageURL, "", nil))
	defer func() { _ = first.Body.Close() }()

	require.Equal(t, http.StatusOK, first.StatusCode)

	var firstBody reviewsListResponseBody
	decodeJSON(t, first, &firstBody)
	require.Equal(t, "OK", firstBody.Code)
	require.Equal(t, "success", firstBody.Status)
	require.Len(t, firstBody.Data, 2)
	require.True(t, firstBody.Meta.HasMore)
	require.NotNil(t, firstBody.Meta.NextCursor)
	require.NotNil(t, firstBody.Data[0].Review)
	require.NotNil(t, firstBody.Data[1].Review)
	require.Equal(t, "latest", *firstBody.Data[0].Review)
	require.Equal(t, "middle", *firstBody.Data[1].Review)

	secondPageURL := env.server.URL + "/api/v1/trainers/" + env.trainerID + "/reviews?" + url.Values{
		"limit":  []string{"2"},
		"cursor": []string{*firstBody.Meta.NextCursor},
	}.Encode()
	second := doReq(t, env.httpClient, newRequest(t, http.MethodGet, secondPageURL, "", nil))
	defer func() { _ = second.Body.Close() }()

	require.Equal(t, http.StatusOK, second.StatusCode)

	var secondBody reviewsListResponseBody
	decodeJSON(t, second, &secondBody)
	require.Len(t, secondBody.Data, 1)
	require.False(t, secondBody.Meta.HasMore)
	require.Nil(t, secondBody.Meta.NextCursor)
	require.NotNil(t, secondBody.Data[0].Review)
	require.Equal(t, "oldest", *secondBody.Data[0].Review)
}

func TestGetTrainerReviewsRejectsInvalidPaginationInputs(t *testing.T) {
	env := setupReviewTestEnv(t)

	invalidLimit := doReq(t, env.httpClient, newRequest(t, http.MethodGet, env.server.URL+"/api/v1/trainers/"+env.trainerID+"/reviews?limit=101", "", nil))
	defer func() { _ = invalidLimit.Body.Close() }()

	require.Equal(t, http.StatusUnprocessableEntity, invalidLimit.StatusCode)

	var invalidLimitBody errorResponseBody
	decodeJSON(t, invalidLimit, &invalidLimitBody)
	require.Equal(t, "INVALID_INPUT", invalidLimitBody.Code)
	require.Equal(t, "limit must be between 1 and 100", invalidLimitBody.Message)

	invalidCursor := doReq(t, env.httpClient, newRequest(t, http.MethodGet, env.server.URL+"/api/v1/trainers/"+env.trainerID+"/reviews?cursor=not-a-valid-cursor", "", nil))
	defer func() { _ = invalidCursor.Body.Close() }()

	require.Equal(t, http.StatusUnprocessableEntity, invalidCursor.StatusCode)

	var invalidCursorBody errorResponseBody
	decodeJSON(t, invalidCursor, &invalidCursorBody)
	require.Equal(t, "INVALID_INPUT", invalidCursorBody.Code)
	require.Equal(t, "invalid pagination cursor", invalidCursorBody.Message)
}

func setupReviewTestEnv(t *testing.T) reviewTestEnv {
	t.Helper()

	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	resetTables(t, db)

	clientUserID := insertUser(t, db, "client@example.com", "Client", "client")
	otherUserID := insertUser(t, db, "other-client@example.com", "Other Client", "client")
	trainerUserID := insertUser(t, db, "trainer@example.com", "Trainer", "client")
	trainerID := insertTrainer(t, db, trainerUserID)

	server := newServer(t, db)
	t.Cleanup(server.Close)

	return reviewTestEnv{
		db:           db,
		server:       server,
		httpClient:   http.DefaultClient,
		clientToken:  tokenFor(t, clientUserID),
		clientUserID: clientUserID,
		otherUserID:  otherUserID,
		trainerID:    trainerID,
	}
}

func insertTrainer(t *testing.T, db *sql.DB, userID string) string {
	t.Helper()

	var trainerID string
	err := db.QueryRow(`
INSERT INTO trainers (user_id, specialization, onboarding_status)
VALUES ($1, 'Strength', 'approved')
RETURNING id
`, userID).Scan(&trainerID)
	require.NoError(t, err)

	return trainerID
}

func insertBooking(t *testing.T, db *sql.DB, trainerID, clientUserID, status string, completedAt *time.Time) string {
	t.Helper()

	var trainerBookingID string
	err := db.QueryRow(`
INSERT INTO bookings (trainer_id, client_user_id, status, completed_at)
VALUES ($1, $2, $3, $4)
RETURNING id
`, trainerID, clientUserID, status, completedAt).Scan(&trainerBookingID)
	require.NoError(t, err)

	return trainerBookingID
}

func insertReviewFixture(t *testing.T, db *sql.DB, trainerID, clientUserID string, rating int, review string, createdAt time.Time) string {
	t.Helper()

	bookingID := insertBooking(t, db, trainerID, clientUserID, "completed", timePtr(createdAt))

	var reviewID string
	err := db.QueryRow(`
INSERT INTO reviews (booking_id, trainer_id, client_user_id, rating, review, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING id
`, bookingID, trainerID, clientUserID, rating, review, createdAt).Scan(&reviewID)
	require.NoError(t, err)

	return reviewID
}

func trainerReviewStats(t *testing.T, db *sql.DB, trainerID string) (float64, int) {
	t.Helper()

	var average sql.NullFloat64
	var total int
	err := db.QueryRow(`
SELECT average_rating::float8, total_reviews
FROM trainers
WHERE id = $1
`, trainerID).Scan(&average, &total)
	require.NoError(t, err)
	require.True(t, average.Valid)

	return average.Float64, total
}

func doJSONRequest(t *testing.T, client *http.Client, method, requestURL, token string, payload any) *http.Response {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := newRequest(t, method, requestURL, token, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	return doReq(t, client, req)
}

func newRequest(t *testing.T, method, requestURL, token string, body io.Reader) *http.Request {
	t.Helper()

	req, err := http.NewRequest(method, requestURL, body)
	require.NoError(t, err)

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return req
}

func decodeJSON(t *testing.T, res *http.Response, dst any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(res.Body).Decode(dst))
}

func timePtr(v time.Time) *time.Time {
	return &v
}
