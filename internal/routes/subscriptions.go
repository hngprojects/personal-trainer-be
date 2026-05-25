package routes

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/iap"
	"github.com/lib/pq"
)

// planMeta holds the static details for each subscription tier.
// planType must match plan_type FK in the subscription_plans table.
var planMeta = map[string]struct {
	sessions int
	amount   int64
	planType string
}{
	"casual":     {sessions: 1, amount: 2000, planType: "single"},
	"committed":  {sessions: 12, amount: 8000, planType: "monthly_12"},
	"consistent": {sessions: 18, amount: 12000, planType: "monthly_18"},
}

func ptr[T any](v T) *T { return &v }

func (s *routerImpl) GetSubscriptionPlans(c *gin.Context) {
	plans := []api.SubscriptionPlan{
		{
			Id:               "casual",
			Name:             "The Casual",
			SessionsPerMonth: 1,
			Amount:           2000,
			Currency:         "USD",
			AmountDisplay:    "$20/month",
			TrialDays:        7,
			AppleProductId:   "com.fitcal.plan.casual.monthly",
			GoogleProductId:  "fitcal_plan_casual_monthly",
			Features: []string{
				"1 guided session",
				"Expert guidance during sessions",
				"Workout reminders",
			},
		},
		{
			Id:               "committed",
			Name:             "The Committed",
			Tag:              ptr("Most Popular"),
			SessionsPerMonth: 12,
			Amount:           8000,
			Currency:         "USD",
			AmountDisplay:    "$80/month",
			TrialDays:        7,
			AppleProductId:   "com.fitcal.plan.committed.monthly",
			GoogleProductId:  "fitcal_plan_committed_monthly",
			Features: []string{
				"12 guided sessions per month",
				"Session duration: 60 minutes",
				"Trainer calls you at scheduled time",
				"Expert guidance during sessions",
				"Workout reminders",
			},
		},
		{
			Id:               "consistent",
			Name:             "The Consistent",
			SessionsPerMonth: 18,
			Amount:           12000,
			Currency:         "USD",
			AmountDisplay:    "$120/month",
			TrialDays:        7,
			AppleProductId:   "com.fitcal.plan.consistent.monthly",
			GoogleProductId:  "fitcal_plan_consistent_monthly",
			Features: []string{
				"18 guided sessions per month",
				"Expert guidance during sessions",
				"Workout reminders",
				"Meal recommendations",
			},
		},
	}

	c.JSON(http.StatusOK, api.SubscriptionPlansResponse{
		Code:    api.CodeOK,
		Message: "PLANS_FETCHED",
		Data:    plans,
	})
}

func (s *routerImpl) CreateSubscription(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}

	var body api.CreateSubscriptionRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	// Validate platform-specific field presence
	if body.Platform == "apple" && (body.ReceiptData == nil || *body.ReceiptData == "") {
		c.JSON(http.StatusBadRequest, api.NewError("receipt_data is required for Apple platform", api.CodeBadRequest))
		return
	}
	if body.Platform == "google" && (body.PurchaseToken == nil || *body.PurchaseToken == "") {
		c.JSON(http.StatusBadRequest, api.NewError("purchase_token is required for Google platform", api.CodeBadRequest))
		return
	}

	meta, ok := planMeta[string(body.PlanId)]
	if !ok {
		c.JSON(http.StatusBadRequest, api.NewError("invalid plan_id", api.CodeBadRequest))
		return
	}

	trainerID := uuid.UUID(body.TrainerId)

	ctx := c.Request.Context()

	// Verify trainer exists
	if _, err := s.trainers.q.GetTrainerByID(ctx, trainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("trainer not found", api.CodeNotFound))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to verify trainer", api.CodeServerError))
		return
	}

	// ── IAP Verification ────────────────────────────────────────────────────
	var (
		verified             *iap.VerifiedPurchase
		appleOriginalTxID    sql.NullString
		googlePurchaseToken  sql.NullString
		verifyErr            error
	)

	if s.cfg.IAPSkipVerification {
		// Dev mode: build a synthetic purchase so we can test the full flow
		verified = &iap.VerifiedPurchase{
			OriginalTransactionID: "dev-" + uuid.New().String(),
			ProductID:             body.ProductId,
			PurchasedAt:           time.Now().UTC(),
			ExpiresAt:             time.Now().UTC().AddDate(0, 1, 0),
			IsTrialPeriod:         true,
		}
	} else if body.Platform == "apple" {
		verified, verifyErr = iap.VerifyApple(ctx, *body.ReceiptData, s.cfg.AppleSharedSecret, body.ProductId)
		if verifyErr != nil {
			c.JSON(http.StatusBadRequest, api.NewError("apple receipt verification failed: "+verifyErr.Error(), api.CodeBadRequest))
			return
		}
	} else {
		verified, verifyErr = iap.VerifyGoogle(ctx, s.cfg.GooglePackageName, body.ProductId, *body.PurchaseToken, s.cfg.GoogleServiceAccountJSON)
		if verifyErr != nil {
			c.JSON(http.StatusBadRequest, api.NewError("google purchase verification failed: "+verifyErr.Error(), api.CodeBadRequest))
			return
		}
	}

	// ── Duplicate check ──────────────────────────────────────────────────────
	if body.Platform == "apple" {
		appleOriginalTxID = sql.NullString{String: verified.OriginalTransactionID, Valid: true}
		existing, err := s.trainers.q.GetSubscriptionByAppleTransactionID(ctx, appleOriginalTxID)
		if err == nil && existing.ID != uuid.Nil {
			c.JSON(http.StatusConflict, api.NewError("subscription already exists for this receipt", api.CodeConflict))
			return
		}
	} else {
		googlePurchaseToken = sql.NullString{String: *body.PurchaseToken, Valid: true}
		existing, err := s.trainers.q.GetSubscriptionByGooglePurchaseToken(ctx, googlePurchaseToken)
		if err == nil && existing.ID != uuid.Nil {
			c.JSON(http.StatusConflict, api.NewError("subscription already exists for this purchase token", api.CodeConflict))
			return
		}
	}

	// ── Trial window ─────────────────────────────────────────────────────────
	trialEndsAt := sql.NullTime{}
	if verified.IsTrialPeriod {
		trialEndsAt = sql.NullTime{Time: time.Now().UTC().Add(7 * 24 * time.Hour), Valid: true}
	}

	// ── Persist ──────────────────────────────────────────────────────────────
	sub, err := s.trainers.q.CreateSubscription(ctx, db.CreateSubscriptionParams{
		ClientID:                   userID,
		TrainerID:                  trainerID,
		PlanID:                     sql.NullString{String: string(body.PlanId), Valid: true},
		PlanType:                   meta.planType,
		Platform:                   sql.NullString{String: string(body.Platform), Valid: true},
		SessionsPerMonth:           sql.NullInt32{Int32: int32(meta.sessions), Valid: true},
		Amount:                     sql.NullInt64{Int64: meta.amount, Valid: true},
		TrialEndsAt:                trialEndsAt,
		CurrentPeriodStart:         sql.NullTime{Time: verified.PurchasedAt, Valid: true},
		CurrentPeriodEnd:           sql.NullTime{Time: verified.ExpiresAt, Valid: true},
		AppleOriginalTransactionID: appleOriginalTxID,
		GooglePurchaseToken:        googlePurchaseToken,
	})
	if err != nil {
		s.logger.Error("create subscription: db error", "err", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			c.JSON(http.StatusConflict, api.NewError("client already has an active subscription with this trainer", api.CodeConflict))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create subscription", api.CodeServerError))
		return
	}

	c.JSON(http.StatusCreated, api.NewSuccess("SUBSCRIPTION_CREATED", api.CodeCreated, subscriptionToMap(sub)))
}

func subscriptionToMap(s db.Subscription) map[string]interface{} {
	m := map[string]interface{}{
		"id":                      s.ID.String(),
		"client_id":               s.ClientID.String(),
		"trainer_id":              s.TrainerID.String(),
		"status":                  s.Status,
		"sessions_used_this_month": s.SessionsUsedThisMonth,
		"currency":                s.Currency,
		"created_at":              s.CreatedAt,
	}
	if s.PlanID.Valid {
		m["plan_id"] = s.PlanID.String
	}
	if s.Platform.Valid {
		m["platform"] = s.Platform.String
	}
	if s.SessionsPerMonth.Valid {
		m["sessions_per_month"] = s.SessionsPerMonth.Int32
	}
	if s.Amount.Valid {
		m["amount"] = s.Amount.Int64
	}
	if s.TrialEndsAt.Valid {
		m["trial_ends_at"] = s.TrialEndsAt.Time
	}
	if s.CurrentPeriodStart.Valid {
		m["current_period_start"] = s.CurrentPeriodStart.Time
	}
	if s.CurrentPeriodEnd.Valid {
		m["current_period_end"] = s.CurrentPeriodEnd.Time
	}
	return m
}
