package routes

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type subscriptionItem struct {
	ID                 string     `json:"id"`
	ClientID           string     `json:"client_id"`
	ClientName         string     `json:"client_name"`
	ClientEmail        string     `json:"client_email"`
	TrainerID          string     `json:"trainer_id"`
	TrainerName        string     `json:"trainer_name"`
	TrainerEmail       string     `json:"trainer_email"`
	PlanType           string     `json:"plan_type"`
	Amount             *int64     `json:"amount"`
	Currency           string     `json:"currency"`
	Status             string     `json:"status"`
	Platform           *string    `json:"platform"`
	CurrentPeriodStart *time.Time `json:"current_period_start"`
	CurrentPeriodEnd   *time.Time `json:"current_period_end"`
	CreatedAt          time.Time  `json:"created_at"`
	CancelledAt        *time.Time `json:"cancelled_at"`
}

func (s *routerImpl) GetAdminSubscriptions(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	page, limit, ok := parseAdminPagination(c)
	if !ok {
		return
	}

	status := c.Query("status")
	if status != "" && status != "active" && status != "cancelled" && status != "expired" {
		c.JSON(http.StatusBadRequest, api.NewError("status must be one of: active, cancelled, expired", api.CodeBadRequest))
		return
	}

	ctx := c.Request.Context()

	total, err := s.trainers.q.CountAdminSubscriptions(ctx, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get subscriptions", api.CodeServerError))
		return
	}

	rows, err := s.trainers.q.ListAdminSubscriptions(ctx, db.ListAdminSubscriptionsParams{
		Status:     status,
		PageLimit:  int32(limit),
		PageOffset: int32(int64(page-1) * int64(limit)),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get subscriptions", api.CodeServerError))
		return
	}

	items := make([]subscriptionItem, 0, len(rows))
	for _, r := range rows {
		item := subscriptionItem{
			ID:           r.ID.String(),
			ClientID:     r.ClientID.String(),
			ClientName:   r.ClientName,
			ClientEmail:  r.ClientEmail,
			TrainerID:    r.TrainerID.String(),
			TrainerName:  r.TrainerName,
			TrainerEmail: r.TrainerEmail,
			PlanType:     r.PlanType,
			Currency:     r.Currency,
			Status:       r.Status,
			CreatedAt:    r.CreatedAt,
		}
		if r.Amount.Valid {
			v := r.Amount.Int64
			item.Amount = &v
		}
		if r.Platform.Valid {
			v := r.Platform.String
			item.Platform = &v
		}
		if r.CurrentPeriodStart.Valid {
			t := r.CurrentPeriodStart.Time
			item.CurrentPeriodStart = &t
		}
		if r.CurrentPeriodEnd.Valid {
			t := r.CurrentPeriodEnd.Time
			item.CurrentPeriodEnd = &t
		}
		if r.CancelledAt.Valid {
			t := r.CancelledAt.Time
			item.CancelledAt = &t
		}
		items = append(items, item)
	}

	meta := api.NewPaginationMeta(page, limit, int(total))
	c.JSON(http.StatusOK, api.NewSuccessWithMeta("subscriptions fetched", api.CodeOK, items, meta))
}
