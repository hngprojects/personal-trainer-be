package bookings_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/bookings"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// Mock repository — implements bookings.Repository
// ---------------------------------------------------------------------------

type mockBookingsRepo struct {
	getBookingByIDFn           func(ctx context.Context, id uuid.UUID) (db.Booking, error)
	checkPaidBookingConflictFn func(ctx context.Context, arg db.CheckPaidBookingConflictParams) (int64, error)
	reschedulePaidBookingFn    func(ctx context.Context, arg db.ReschedulePaidBookingParams) (db.Booking, error)
	createPaidRescheduleHistFn func(ctx context.Context, arg db.CreatePaidRescheduleHistoryParams) error
	getUserByIDFn              func(ctx context.Context, id uuid.UUID) (db.User, error)
	getTrainerByIDFn           func(ctx context.Context, id uuid.UUID) (db.Trainer, error)
	findBookingSlotsByTrainerFn func(ctx context.Context, trainerID uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error)
	createBookingFn            func(ctx context.Context, args db.CreateBookingParams) (*db.Booking, error)
	getSubscriptionDetailsFn   func(ctx context.Context, subID uuid.UUID) (db.Subscription, error)
	getTrainerDetailsFn        func(ctx context.Context, trainerID uuid.UUID) (db.GetTrainerUserDetailsRow, error)
	updateBookingZoomFn        func(ctx context.Context, arg db.UpdateBookingZoomParams) (db.Booking, error)
	createBookingSessionFn     func(ctx context.Context, arg db.CreateBookingSessionParams) (db.BookingSession, error)
}

func (m *mockBookingsRepo) GetBookingByID(ctx context.Context, id uuid.UUID) (db.Booking, error) {
	if m.getBookingByIDFn != nil {
		return m.getBookingByIDFn(ctx, id)
	}
	return db.Booking{}, sql.ErrNoRows
}
func (m *mockBookingsRepo) CheckPaidBookingConflict(ctx context.Context, arg db.CheckPaidBookingConflictParams) (int64, error) {
	if m.checkPaidBookingConflictFn != nil {
		return m.checkPaidBookingConflictFn(ctx, arg)
	}
	return 0, nil
}
func (m *mockBookingsRepo) ReschedulePaidBooking(ctx context.Context, arg db.ReschedulePaidBookingParams) (db.Booking, error) {
	if m.reschedulePaidBookingFn != nil {
		return m.reschedulePaidBookingFn(ctx, arg)
	}
	return db.Booking{}, nil
}
func (m *mockBookingsRepo) CreatePaidRescheduleHistory(ctx context.Context, arg db.CreatePaidRescheduleHistoryParams) error {
	if m.createPaidRescheduleHistFn != nil {
		return m.createPaidRescheduleHistFn(ctx, arg)
	}
	return nil
}
func (m *mockBookingsRepo) GetUserByID(ctx context.Context, id uuid.UUID) (db.User, error) {
	if m.getUserByIDFn != nil {
		return m.getUserByIDFn(ctx, id)
	}
	return db.User{}, nil
}
func (m *mockBookingsRepo) GetTrainerByID(ctx context.Context, id uuid.UUID) (db.Trainer, error) {
	if m.getTrainerByIDFn != nil {
		return m.getTrainerByIDFn(ctx, id)
	}
	return db.Trainer{}, nil
}
func (m *mockBookingsRepo) FindBookingSlotByTrainerID(ctx context.Context, trainerID uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error) {
	if m.findBookingSlotsByTrainerFn != nil {
		return m.findBookingSlotsByTrainerFn(ctx, trainerID)
	}
	return nil, nil
}
func (m *mockBookingsRepo) CreateBooking(ctx context.Context, args db.CreateBookingParams) (*db.Booking, error) {
	if m.createBookingFn != nil {
		return m.createBookingFn(ctx, args)
	}
	b := db.Booking{ID: uuid.New()}
	return &b, nil
}
func (m *mockBookingsRepo) GetSubscriptionDetails(ctx context.Context, subID uuid.UUID) (db.Subscription, error) {
	if m.getSubscriptionDetailsFn != nil {
		return m.getSubscriptionDetailsFn(ctx, subID)
	}
	return db.Subscription{Status: "active"}, nil
}
func (m *mockBookingsRepo) GetTrainerDetails(ctx context.Context, trainerID uuid.UUID) (db.GetTrainerUserDetailsRow, error) {
	if m.getTrainerDetailsFn != nil {
		return m.getTrainerDetailsFn(ctx, trainerID)
	}
	return db.GetTrainerUserDetailsRow{}, nil
}
func (m *mockBookingsRepo) UpdateBookingZoom(ctx context.Context, arg db.UpdateBookingZoomParams) (db.Booking, error) {
	if m.updateBookingZoomFn != nil {
		return m.updateBookingZoomFn(ctx, arg)
	}
	return db.Booking{}, nil
}
func (m *mockBookingsRepo) CreateBookingSession(ctx context.Context, arg db.CreateBookingSessionParams) (db.BookingSession, error) {
	if m.createBookingSessionFn != nil {
		return m.createBookingSessionFn(ctx, arg)
	}
	return db.BookingSession{}, nil
}

// ---------------------------------------------------------------------------
// Mock booking service — implements bookings.BookingService
// ---------------------------------------------------------------------------

type mockBookingService struct {
	createBookingFn      func(ctx context.Context, args db.CreateBookingParams, user db.User, trainer db.GetTrainerUserDetailsRow) (*db.Booking, error)
	getTrainerDetailsFn  func(ctx context.Context, id uuid.UUID) (*db.GetTrainerUserDetailsRow, error)
	checkSubscriptionFn  func(ctx context.Context, subID uuid.UUID) (bool, error)
	getUserByIDFn        func(ctx context.Context, id uuid.UUID) (*db.User, error)
}

func (m *mockBookingService) CreateBooking(ctx context.Context, args db.CreateBookingParams, user db.User, trainer db.GetTrainerUserDetailsRow) (*db.Booking, error) {
	if m.createBookingFn != nil {
		return m.createBookingFn(ctx, args, user, trainer)
	}
	b := db.Booking{ID: uuid.New()}
	return &b, nil
}
func (m *mockBookingService) GetTrainerDetails(ctx context.Context, id uuid.UUID) (*db.GetTrainerUserDetailsRow, error) {
	if m.getTrainerDetailsFn != nil {
		return m.getTrainerDetailsFn(ctx, id)
	}
	row := db.GetTrainerUserDetailsRow{Name: "Trainer"}
	return &row, nil
}
func (m *mockBookingService) CheckSubscription(ctx context.Context, subID uuid.UUID) (bool, error) {
	if m.checkSubscriptionFn != nil {
		return m.checkSubscriptionFn(ctx, subID)
	}
	return true, nil
}
func (m *mockBookingService) GetUserByID(ctx context.Context, id uuid.UUID) (*db.User, error) {
	if m.getUserByIDFn != nil {
		return m.getUserByIDFn(ctx, id)
	}
	user := db.User{Name: "Test User", Email: "user@example.com"}
	return &user, nil
}

// ---------------------------------------------------------------------------
// Null mailer — satisfies email.Mailer
// ---------------------------------------------------------------------------

type nullMailer struct{}

func (nullMailer) SendVerificationCode(_, _ string, _ int) error { return nil }
func (nullMailer) SendAdminCredentials(_, _ string) error        { return nil }
func (nullMailer) SendPasswordResetCode(_, _ string, _ int) error { return nil }
func (nullMailer) SendWaitlistConfirmation(_ string) error        { return nil }
func (nullMailer) SendContactConfirmation(_, _ string) error      { return nil }
func (nullMailer) SendDiscoveryBookingConfirmation(_, _ string, _ time.Time, _, _, _, _ string) error {
	return nil
}
func (nullMailer) SendDiscoveryBookingAdminNotification(_, _, _ string, _ time.Time, _, _, _, _ string) error {
	return nil
}
func (nullMailer) SendDiscoveryRescheduleConfirmation(_, _ string, _, _ time.Time, _, _, _, _ string) error {
	return nil
}
func (nullMailer) SendPaidSessionRescheduleConfirmation(_, _ string, _, _ time.Time, _, _ string) error {
	return nil
}
func (nullMailer) SendPaidSessionRescheduleTrainerNotification(_, _ string, _, _ time.Time, _, _ string) error {
	return nil
}
func (nullMailer) SendBookingConfirmation(_, _, _ string, _, _ time.Time, _, _ string) error {
	return nil
}
func (nullMailer) SendTrainerCredentials(_, _ string) error { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func confirmedBooking(clientID, trainerID uuid.UUID) db.Booking {
	start := time.Now().UTC().Add(24 * time.Hour)
	end := start.Add(60 * time.Minute)
	return db.Booking{
		ID:              uuid.New(),
		ClientID:        clientID,
		TrainerID:       trainerID,
		BookingStatus:   sql.NullString{String: "confirmed", Valid: true},
		ScheduledStart:  sql.NullTime{Time: start, Valid: true},
		ScheduledEnd:    sql.NullTime{Time: end, Valid: true},
		Timezone:        sql.NullString{String: "UTC", Valid: true},
		RescheduleCount: 0,
	}
}

func validRescheduleBody(newTime time.Time) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"new_datetime": newTime.Format(time.RFC3339),
		"reason":       "work_conflict",
		"timezone":     "Africa/Lagos",
	})
	return b
}

func ginCtxWithUser(w *httptest.ResponseRecorder, userID uuid.UUID, body []byte) *gin.Context {
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(common.ContextKeyUserID), userID)
	return c
}

// ---------------------------------------------------------------------------
// TryReschedulePaidSession tests
// ---------------------------------------------------------------------------

func TestReschedule_BookingNotFound_ReturnsFalse(t *testing.T) {
	h := bookings.NewHandler(&mockBookingsRepo{}, meeting.NoOp{}, nullMailer{}, testLogger())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodPut, "/", nil)
	c.Set(string(common.ContextKeyUserID), uuid.New())

	if h.TryReschedulePaidSession(c, openapi_types.UUID(uuid.New())) {
		t.Error("expected false (fall-through) when booking not found")
	}
}

func TestReschedule_NoUserInContext_401(t *testing.T) {
	b := confirmedBooking(uuid.New(), uuid.New())
	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodPut, "/", nil)

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestReschedule_WrongUser_403(t *testing.T) {
	clientID := uuid.New()
	b := confirmedBooking(clientID, uuid.New())
	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	w := httptest.NewRecorder()
	c := ginCtxWithUser(w, uuid.New() /* different user */, validRescheduleBody(time.Now().Add(25*time.Hour)))

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestReschedule_CancelledBooking_403(t *testing.T) {
	clientID := uuid.New()
	b := confirmedBooking(clientID, uuid.New())
	b.BookingStatus = sql.NullString{String: "cancelled", Valid: true}
	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	w := httptest.NewRecorder()
	c := ginCtxWithUser(w, clientID, validRescheduleBody(time.Now().Add(25*time.Hour)))

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestReschedule_WithinLockWindow_403(t *testing.T) {
	clientID := uuid.New()
	b := confirmedBooking(clientID, uuid.New())
	b.ScheduledStart = sql.NullTime{Time: time.Now().UTC().Add(6 * time.Hour), Valid: true}
	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	w := httptest.NewRecorder()
	c := ginCtxWithUser(w, clientID, validRescheduleBody(time.Now().Add(48*time.Hour)))

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 (within 12h lock), got %d", w.Code)
	}
}

func TestReschedule_MaxReschedulesReached_429(t *testing.T) {
	clientID := uuid.New()
	b := confirmedBooking(clientID, uuid.New())
	b.RescheduleCount = 3
	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	w := httptest.NewRecorder()
	c := ginCtxWithUser(w, clientID, validRescheduleBody(time.Now().Add(25*time.Hour)))

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestReschedule_NewTimeInPast_400(t *testing.T) {
	clientID := uuid.New()
	b := confirmedBooking(clientID, uuid.New())
	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	w := httptest.NewRecorder()
	c := ginCtxWithUser(w, clientID, validRescheduleBody(time.Now().Add(-1*time.Hour)))

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReschedule_TrainerConflict_409(t *testing.T) {
	clientID := uuid.New()
	b := confirmedBooking(clientID, uuid.New())
	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
		checkPaidBookingConflictFn: func(_ context.Context, _ db.CheckPaidBookingConflictParams) (int64, error) {
			return 1, nil
		},
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	w := httptest.NewRecorder()
	c := ginCtxWithUser(w, clientID, validRescheduleBody(time.Now().Add(25*time.Hour)))

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestReschedule_InvalidTimezone_400(t *testing.T) {
	clientID := uuid.New()
	b := confirmedBooking(clientID, uuid.New())
	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	body, _ := json.Marshal(map[string]interface{}{
		"new_datetime": time.Now().Add(25 * time.Hour).Format(time.RFC3339),
		"reason":       "work_conflict",
		"timezone":     "Not/AReal_Timezone",
	})
	w := httptest.NewRecorder()
	c := ginCtxWithUser(w, clientID, body)

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReschedule_Success_200(t *testing.T) {
	clientID := uuid.New()
	trainerID := uuid.New()
	b := confirmedBooking(clientID, trainerID)
	newStart := time.Now().Add(25 * time.Hour)
	rescheduled := b
	rescheduled.ScheduledStart = sql.NullTime{Time: newStart, Valid: true}
	rescheduled.RescheduleCount = 1

	h := bookings.NewHandler(&mockBookingsRepo{
		getBookingByIDFn: func(_ context.Context, _ uuid.UUID) (db.Booking, error) { return b, nil },
		checkPaidBookingConflictFn: func(_ context.Context, _ db.CheckPaidBookingConflictParams) (int64, error) {
			return 0, nil
		},
		reschedulePaidBookingFn: func(_ context.Context, _ db.ReschedulePaidBookingParams) (db.Booking, error) {
			return rescheduled, nil
		},
		getUserByIDFn: func(_ context.Context, _ uuid.UUID) (db.User, error) {
			return db.User{Name: "Client", Email: "client@example.com"}, nil
		},
		getTrainerByIDFn: func(_ context.Context, _ uuid.UUID) (db.Trainer, error) {
			return db.Trainer{ID: trainerID, UserID: uuid.New()}, nil
		},
	}, meeting.NoOp{}, nullMailer{}, testLogger())

	w := httptest.NewRecorder()
	c := ginCtxWithUser(w, clientID, validRescheduleBody(newStart))

	if !h.TryReschedulePaidSession(c, openapi_types.UUID(b.ID)) {
		t.Fatal("expected handled=true")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp api.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("bad response shape: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status=success, got %q", resp.Status)
	}
}

// ---------------------------------------------------------------------------
// HandleCreateBookingSession tests
// ---------------------------------------------------------------------------

func validCreateBookingBody(trainerID, subscriptionID uuid.UUID) []byte {
	start := time.Now().Add(2 * time.Hour)
	end := start.Add(60 * time.Minute)
	b, _ := json.Marshal(map[string]interface{}{
		"trainer_id":      trainerID,
		"subscription_id": subscriptionID,
		"scheduled_start": start.Format(time.RFC3339),
		"scheduled_end":   end.Format(time.RFC3339),
		"session_platform": "zoom",
		"timezone":        "Africa/Lagos",
	})
	return b
}

func newCreateBookingCtx(w *httptest.ResponseRecorder, userID uuid.UUID, body []byte) *gin.Context {
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodPost, "/bookings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(common.ContextKeyUserID), userID)
	return c
}

func TestCreateBooking_MissingTrainerID_400(t *testing.T) {
	svc := &mockBookingService{
		checkSubscriptionFn: func(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil },
	}
	h := bookings.NewBookingHandler(svc, testLogger())
	body, _ := json.Marshal(map[string]interface{}{
		"subscription_id":  uuid.New(),
		"scheduled_start":  time.Now().Add(2 * time.Hour).Format(time.RFC3339),
		"scheduled_end":    time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		"session_platform": "zoom",
		"timezone":         "Africa/Lagos",
		// trainer_id omitted → uuid.Nil
	})
	w := httptest.NewRecorder()
	c := newCreateBookingCtx(w, uuid.New(), body)
	h.HandleCreateBookingSession(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing trainer_id, got %d", w.Code)
	}
}

func TestCreateBooking_MissingTimezone_400(t *testing.T) {
	svc := &mockBookingService{
		checkSubscriptionFn: func(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil },
	}
	h := bookings.NewBookingHandler(svc, testLogger())
	body, _ := json.Marshal(map[string]interface{}{
		"trainer_id":       uuid.New(),
		"subscription_id":  uuid.New(),
		"scheduled_start":  time.Now().Add(2 * time.Hour).Format(time.RFC3339),
		"scheduled_end":    time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		"session_platform": "zoom",
		// timezone omitted
	})
	w := httptest.NewRecorder()
	c := newCreateBookingCtx(w, uuid.New(), body)
	h.HandleCreateBookingSession(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing timezone, got %d", w.Code)
	}
}

func TestCreateBooking_InactiveSubscription_400(t *testing.T) {
	svc := &mockBookingService{
		checkSubscriptionFn: func(_ context.Context, _ uuid.UUID) (bool, error) { return false, nil },
	}
	h := bookings.NewBookingHandler(svc, testLogger())
	trainerID := uuid.New()
	subID := uuid.New()
	w := httptest.NewRecorder()
	c := newCreateBookingCtx(w, uuid.New(), validCreateBookingBody(trainerID, subID))
	h.HandleCreateBookingSession(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for inactive subscription, got %d", w.Code)
	}
}

func TestCreateBooking_InvalidPlatform_400(t *testing.T) {
	svc := &mockBookingService{
		checkSubscriptionFn: func(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil },
	}
	h := bookings.NewBookingHandler(svc, testLogger())
	body, _ := json.Marshal(map[string]interface{}{
		"trainer_id":       uuid.New(),
		"subscription_id":  uuid.New(),
		"scheduled_start":  time.Now().Add(2 * time.Hour).Format(time.RFC3339),
		"scheduled_end":    time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		"session_platform": "carrier_pigeon", // invalid
		"timezone":         "Africa/Lagos",
	})
	w := httptest.NewRecorder()
	c := newCreateBookingCtx(w, uuid.New(), body)
	h.HandleCreateBookingSession(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid platform, got %d", w.Code)
	}
}

func TestCreateBooking_EndBeforeStart_400(t *testing.T) {
	svc := &mockBookingService{
		checkSubscriptionFn: func(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil },
	}
	h := bookings.NewBookingHandler(svc, testLogger())
	start := time.Now().Add(2 * time.Hour)
	body, _ := json.Marshal(map[string]interface{}{
		"trainer_id":       uuid.New(),
		"subscription_id":  uuid.New(),
		"scheduled_start":  start.Format(time.RFC3339),
		"scheduled_end":    start.Add(-30 * time.Minute).Format(time.RFC3339), // end before start
		"session_platform": "zoom",
		"timezone":         "Africa/Lagos",
	})
	w := httptest.NewRecorder()
	c := newCreateBookingCtx(w, uuid.New(), body)
	h.HandleCreateBookingSession(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for end-before-start, got %d", w.Code)
	}
}

func TestCreateBooking_NoUserContext_401(t *testing.T) {
	svc := &mockBookingService{
		checkSubscriptionFn: func(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil },
	}
	h := bookings.NewBookingHandler(svc, testLogger())
	body := validCreateBookingBody(uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodPost, "/bookings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	// no user ID set in context
	h.HandleCreateBookingSession(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing user context, got %d", w.Code)
	}
}

func TestCreateBooking_Success_200(t *testing.T) {
	trainerID := uuid.New()
	subID := uuid.New()
	clientID := uuid.New()
	created := &db.Booking{ID: uuid.New(), TrainerID: trainerID, ClientID: clientID}

	svc := &mockBookingService{
		checkSubscriptionFn: func(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil },
		getUserByIDFn: func(_ context.Context, _ uuid.UUID) (*db.User, error) {
			u := db.User{Name: "Client", Email: "client@example.com"}
			return &u, nil
		},
		getTrainerDetailsFn: func(_ context.Context, _ uuid.UUID) (*db.GetTrainerUserDetailsRow, error) {
			row := db.GetTrainerUserDetailsRow{Name: "Trainer", Email: "trainer@example.com"}
			return &row, nil
		},
		createBookingFn: func(_ context.Context, _ db.CreateBookingParams, _ db.User, _ db.GetTrainerUserDetailsRow) (*db.Booking, error) {
			return created, nil
		},
	}
	h := bookings.NewBookingHandler(svc, testLogger())
	w := httptest.NewRecorder()
	c := newCreateBookingCtx(w, clientID, validCreateBookingBody(trainerID, subID))
	h.HandleCreateBookingSession(c)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d — body: %s", w.Code, w.Body.String())
	}
}
