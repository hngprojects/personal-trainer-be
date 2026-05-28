package routes

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

func (s *routerImpl) AdminAdd(c *gin.Context) {
	if s.admin == nil {
		s.logger.Warn("admin add: admin handler not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.admin.AdminAdd(c)
}

func (s *routerImpl) AdminApproveTrainer(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		s.logger.Warn("admin approve trainer: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	_, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("admin approve trainer: trainer not found", "trainerID", trainerID.String(), "err", err)
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		s.logger.Warn("admin approve trainer: failed to fetch trainer", "trainerID", trainerID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch trainer", api.CodeServerError))
		return
	}

	updated, err := s.trainers.q.ApproveTrainer(c.Request.Context(), trainerID)
	if err != nil {
		s.logger.Warn("admin approve trainer: failed to approve", "trainerID", trainerID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to approve trainer", api.CodeServerError))
		return
	}

	// Notify trainer about approval
	if _, notifErr := s.notificationService.SendNotificationToUser(c.Request.Context(), updated.UserID,
		"Account Approved",
		"Your trainer account has been approved.",
		"approve-trainer-"+trainerID.String(),
	); notifErr != nil {
		s.logger.Warn("approve trainer notification failed", "trainerID", trainerID, "err", notifErr)
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_APPROVED", api.CodeOK, trainerToMap(updated)))
}

// AdminListSessions handles GET /admin/sessions. Paginated list of every
// booking in the bookings table, with client and trainer names joined.
// Guarded by SuperAdminOnly via the /admin path prefix.
func (s *routerImpl) AdminListSessions(c *gin.Context, params api.AdminListSessionsParams) {
	if s.bookings == nil {
		s.logger.Warn("AdminListSessions: bookings store is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	page, limit, ok := parsePagination(c, params.Page, params.Limit, s.logger)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	total, err := s.bookings.q.CountBookingsForAdmin(ctx)
	if err != nil {
		s.logger.Error("count bookings failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list sessions", api.CodeServerError))
		return
	}

	rows, err := s.bookings.q.ListBookingsForAdmin(ctx, db.ListBookingsForAdminParams{
		PageLimit:  int32(limit),
		PageOffset: int32((page - 1) * limit),
	})
	if err != nil {
		s.logger.Error("list bookings failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list sessions", api.CodeServerError))
		return
	}

	list := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		list = append(list, adminBookingRowToMap(r))
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("SESSIONS_FETCHED", api.CodeOK, list, api.NewPaginationMeta(page, limit, int(total))))
}

// AdminListDiscoveryBookings handles GET /admin/discovery-bookings.
// Paginated list of every row in discovery_bookings, newest scheduled
// time first.
func (s *routerImpl) AdminListDiscoveryBookings(c *gin.Context, params api.AdminListDiscoveryBookingsParams) {
	if s.bookings == nil {
		s.logger.Warn("AdminListDiscoveryBookings: bookings store is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	page, limit, ok := parsePagination(c, params.Page, params.Limit, s.logger)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	total, err := s.bookings.q.CountDiscoveryBookings(ctx)
	if err != nil {
		s.logger.Error("count discovery bookings failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list discovery bookings", api.CodeServerError))
		return
	}

	rows, err := s.bookings.q.ListDiscoveryBookingsPaginated(ctx, db.ListDiscoveryBookingsPaginatedParams{
		PageLimit:  int32(limit),
		PageOffset: int32((page - 1) * limit),
	})
	if err != nil {
		s.logger.Error("list discovery bookings failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list discovery bookings", api.CodeServerError))
		return
	}

	list := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		list = append(list, discoveryBookingToAdminMap(r))
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("DISCOVERY_BOOKINGS_FETCHED", api.CodeOK, list, api.NewPaginationMeta(page, limit, int(total))))
}

func (s *routerImpl) GetAdminRevenue(c *gin.Context) {
	if s.trainers == nil {
		s.logger.Warn("GetAdminRevenue: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	ctx := c.Request.Context()

	rev, err := s.trainers.q.GetRevenueSnapshot(ctx)
	if err != nil {
		s.logger.Error("failed to get revenue snapshot", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get revenue snapshot", api.CodeServerError))
		return
	}

	revenueData := map[string]interface{}{
		"total":             rev.TotalRevenue,
		"subscriptions":     rev.SubscriptionRevenue,
		"one_time_sessions": rev.OneTimeRevenue,
		"trial_conversions": int64(0),
	}

	latest, err := s.trainers.q.GetLatestSubscription(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.logger.Error("failed to get latest subscription", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get latest payment", api.CodeServerError))
		return
	}

	var latestPayment interface{}
	if err == nil {
		lp := map[string]interface{}{
			"id":           latest.ID.String(),
			"client_name":  latest.ClientName,
			"client_email": latest.ClientEmail,
			"plan_type":    latest.PlanType,
			"amount":       latest.Amount.Int64,
			"currency":     latest.Currency,
			"status":       latest.Status,
			"created_at":   latest.CreatedAt,
		}
		if latest.PlanID.Valid {
			lp["plan_id"] = latest.PlanID.String
		}
		latestPayment = lp
	}

	c.JSON(http.StatusOK, api.NewSuccess("Revenue retrieved successfully", api.CodeOK, map[string]interface{}{
		"revenue":        revenueData,
		"latest_payment": latestPayment,
	}))
}

func adminBookingRowToMap(r db.ListBookingsForAdminRow) map[string]interface{} {
	m := map[string]interface{}{
		"id":            r.ID.String(),
		"trainer_id":    r.TrainerID.String(),
		"client_id":     r.ClientID.String(),
		"client_name":   r.ClientName,
		"client_email":  r.ClientEmail,
		"trainer_name":  r.TrainerName,
		"trainer_email": r.TrainerEmail,
	}
	if r.SessionID.Valid {
		m["session_id"] = r.SessionID.UUID.String()
	}
	if r.ScheduledStart.Valid {
		m["scheduled_start"] = r.ScheduledStart.Time
	}
	if r.ScheduledEnd.Valid {
		m["scheduled_end"] = r.ScheduledEnd.Time
	}
	if r.Timezone.Valid {
		m["timezone"] = r.Timezone.String
	}
	if r.BookingStatus.Valid {
		m["booking_status"] = r.BookingStatus.String
	}
	if r.SessionPlatform.Valid {
		m["session_platform"] = r.SessionPlatform.String
	}
	if r.CreatedAt.Valid {
		m["created_at"] = r.CreatedAt.Time
	}
	if r.CancelledAt.Valid {
		m["cancelled_at"] = r.CancelledAt.Time
	}
	if r.ZoomMeetingLink.Valid {
		m["zoom_meeting_link"] = r.ZoomMeetingLink.String
	}
	return m
}

func discoveryBookingToAdminMap(r db.DiscoveryBooking) map[string]interface{} {
	m := map[string]interface{}{
		"id":                r.ID.String(),
		"name":              r.Name,
		"email":             r.Email,
		"contact_mode":      r.ContactMode,
		"selected_datetime": r.SelectedDatetime,
		"client_timezone":   r.ClientTimezone,
		"status":            r.Status,
		"created_at":        r.CreatedAt,
		"reschedule_count":  r.RescheduleCount,
	}
	if r.UserID.Valid {
		m["user_id"] = r.UserID.UUID.String()
	}
	if r.PhoneNumber.Valid {
		m["phone_number"] = r.PhoneNumber.String
	}
	if r.ZoomMeetingLink.Valid {
		m["zoom_meeting_link"] = r.ZoomMeetingLink.String
	}
	if r.ZoomMeetingID.Valid {
		m["zoom_meeting_id"] = r.ZoomMeetingID.String
	}
	return m
}

func (s *routerImpl) GetUserTrainerCount(c *gin.Context) {
	if s.trainers == nil {
		s.logger.Warn("get user trainer count: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	totalClients, err := s.trainers.q.CountClients(c.Request.Context())
	if err != nil {
		s.logger.Error("failed to count clients", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to count clients", api.CodeServerError))
		return
	}

	totalTrainers, err := s.trainers.q.CountTrainers(c.Request.Context())
	if err != nil {
		s.logger.Error("failed to count trainers", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to count trainers", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Stats retrieved successfully", api.CodeOK, map[string]interface{}{
		"total_clients":           totalClients,
		"total_approved_trainers": totalTrainers,
	}))
}

func (s *routerImpl) GetAdminClients(c *gin.Context, params api.GetAdminClientsParams) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	page, limit, ok := parsePagination(c, params.Page, params.Limit, s.logger)
	if !ok {
		return
	}
	offset := int64((page - 1) * limit)

	var isActive sql.NullBool
	if params.Status != nil {
		switch *params.Status {
		case api.GetAdminClientsParamsStatusActive:
			isActive = sql.NullBool{Bool: true, Valid: true}
		case api.GetAdminClientsParamsStatusInactive:
			isActive = sql.NullBool{Bool: false, Valid: true}
		default:
			c.JSON(http.StatusBadRequest, api.NewError("invalid status: must be active or inactive", api.CodeBadRequest))
			return
		}
	}

	clients, err := s.trainers.q.ListClients(c.Request.Context(), db.ListClientsParams{
		IsActive: isActive,
		Lim:      int64(limit),
		Off:      offset,
	})
	if err != nil {
		s.logger.Error("failed to list clients", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list clients", api.CodeServerError))
		return
	}

	total, err := s.trainers.q.CountClients2(c.Request.Context(), isActive)
	if err != nil {
		s.logger.Error("failed to count clients", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to count clients", api.CodeServerError))
		return
	}

	type clientItem struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Email          string `json:"email"`
		IsActive       bool   `json:"is_active"`
		JoinedAt       string `json:"joined_at"`
		SessionsBooked int64  `json:"sessions_booked"`
		Revenue        int64  `json:"revenue"`
	}

	items := make([]clientItem, 0, len(clients))
	for _, cl := range clients {
		items = append(items, clientItem{
			ID:             cl.ID.String(),
			Name:           cl.Name,
			Email:          cl.Email,
			IsActive:       cl.IsActive,
			JoinedAt:       cl.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			SessionsBooked: cl.SessionsBooked,
			Revenue:        0,
		})
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("Clients retrieved successfully", api.CodeOK, items, api.NewPaginationMeta(page, limit, int(total))))
}

func (s *routerImpl) GetAdminClientByID(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	client, err := s.trainers.q.GetClientByID(c.Request.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, api.NewError("client not found", api.CodeNotFound))
			return
		}
		s.logger.Error("failed to get client", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get client", api.CodeServerError))
		return
	}

	type clientDetail struct {
		ID             string   `json:"id"`
		Name           string   `json:"name"`
		Email          string   `json:"email"`
		IsActive       bool     `json:"is_active"`
		JoinedAt       string   `json:"joined_at"`
		Gender         *string  `json:"gender"`
		FitnessGoals   []string `json:"fitness_goals"`
		FitnessLevel   *string  `json:"fitness_level"`
		AvatarURL      *string  `json:"avatar_url"`
		SessionsBooked int64    `json:"sessions_booked"`
		Revenue        int64    `json:"revenue"`
	}

	detail := clientDetail{
		ID:             client.ID.String(),
		Name:           client.Name,
		Email:          client.Email,
		IsActive:       client.IsActive,
		JoinedAt:       client.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		FitnessGoals:   client.FitnessGoals,
		SessionsBooked: client.SessionsBooked,
		Revenue:        0,
	}
	if client.Gender.Valid {
		detail.Gender = &client.Gender.String
	}
	if client.FitnessLevel.Valid {
		detail.FitnessLevel = &client.FitnessLevel.String
	}
	if client.AvatarUrl.Valid {
		detail.AvatarURL = &client.AvatarUrl.String
	}

	c.JSON(http.StatusOK, api.NewSuccess("Client retrieved successfully", api.CodeOK, detail))
}

func (s *routerImpl) GetAdminTopTrainers(c *gin.Context) {
	if s.trainers == nil {
		s.logger.Warn("get admin top trainers: required stores not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	topTrainers, err := s.trainers.q.GetTopTrainers(c, db.GetTopTrainersParams{
		SessionWeight: 0.6,
		StatusFilter:  []string{"approved"},
		RatingWeight:  0.4,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, api.NewError("no top trainers for this month", api.CodeNotFound))
			return
		}
		s.logger.Error("failed to count clients", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to count clients", api.CodeServerError))
		return
	}
	items := make([]map[string]interface{}, 0, len(topTrainers))
	for _, t := range topTrainers {
		m := map[string]interface{}{
			"id":                 t.ID.String(),
			"user_id":            t.UserID.String(),
			"name":               t.TrainerName,
			"total_reviews":      t.TotalReviews,
			"completed_sessions": t.CompletedSessions,
			"average_rating":     t.AverageRating.Float64,
			"ranking_score":      t.RankingScore,
			"created_at":         t.CreatedAt,
			"updated_at":         t.UpdatedAt,
		}
		if t.DisplayPicture.Valid {
			m["display_picture"] = t.DisplayPicture.String
		}
		items = append(items, m)
	}

	c.JSON(http.StatusOK, api.NewSuccess("Stats retrieved successfully", api.CodeOK, map[string]interface{}{
		"top_trainers": items,
	}))
}
