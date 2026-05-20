package subscription

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	infra_stripe "github.com/hngprojects/personal-trainer-be/internal/infra/stripe"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

const defaultPageSize = 20

// maxTipAmount is the maximum tip in the smallest currency unit (999999 = $9,999.99).
const maxTipAmount = 999999

type Handler struct {
	repo   Repository
	stripe *infra_stripe.Client
	log    *slog.Logger
}

func NewHandler(log *slog.Logger, repo Repository, stripe *infra_stripe.Client) *Handler {
	return &Handler{log: log, repo: repo, stripe: stripe}
}

func (h *Handler) extractUserID(c *gin.Context) (uuid.UUID, bool) {
	val, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return uuid.Nil, false
	}
	id, ok := val.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return uuid.Nil, false
	}
	return id, true
}

// ListSubscriptions GET /subscriptions
func (h *Handler) ListSubscriptions(c *gin.Context) {
	clientID, ok := h.extractUserID(c)
	if !ok {
		return
	}

	subs, err := h.repo.ListSubscriptions(c.Request.Context(), clientID)
	if err != nil {
		h.log.Error("failed to list subscriptions", "client_id", clientID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to retrieve subscriptions", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Subscriptions retrieved successfully", api.CodeOK, subs))
}

// CreateSubscription POST /subscriptions — charge Stripe, then create subscription + payment record.
func (h *Handler) CreateSubscription(c *gin.Context) {
	clientID, ok := h.extractUserID(c)
	if !ok {
		return
	}

	var body struct {
		PlanType        string    `json:"plan_type" binding:"required"`
		TrainerID       uuid.UUID `json:"trainer_id" binding:"required"`
		PaymentMethodID string    `json:"payment_method_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid or missing parameters", api.CodeBadRequest))
		return
	}

	if body.PlanType != common.PlanTypeSingle &&
		body.PlanType != common.PlanTypeMonthly12 &&
		body.PlanType != common.PlanTypeMonthly18 {
		c.JSON(http.StatusBadRequest, api.NewError("invalid plan_type", api.CodeBadRequest))
		return
	}

	ctx := c.Request.Context()

	if _, err := h.repo.GetTrainerByID(ctx, body.TrainerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusBadRequest, api.NewError("trainer not found", api.CodeBadRequest))
			return
		}
		h.log.Error("failed to fetch trainer", "trainer_id", body.TrainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to process request", api.CodeServerError))
		return
	}

	existing, err := h.repo.GetActiveSubscriptionForClient(ctx, clientID, body.TrainerID)
	if err != nil {
		h.log.Error("failed to check existing subscription", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to process request", api.CodeServerError))
		return
	}
	if existing != nil {
		c.JSON(http.StatusConflict, api.NewError("active subscription already exists for this trainer", api.CodeConflict))
		return
	}

	plan, err := h.repo.GetPlanByType(ctx, body.PlanType)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusBadRequest, api.NewError("plan not found", api.CodeBadRequest))
			return
		}
		h.log.Error("failed to fetch plan", "plan_type", body.PlanType, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to process request", api.CodeServerError))
		return
	}

	if h.stripe == nil {
		c.JSON(http.StatusInternalServerError, api.NewError("payment provider not configured", api.CodeServerError))
		return
	}

	idempotencyKey := fmt.Sprintf("sub:%s:%s:%s:%s",
		clientID, body.TrainerID, body.PlanType, time.Now().UTC().Format("2006-01"))

	existingPayment, err := h.repo.GetPaymentByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		h.log.Error("failed to check idempotency key", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to process request", api.CodeServerError))
		return
	}
	if existingPayment != nil {
		c.JSON(http.StatusConflict, api.NewError("subscription payment already processed", api.CodeConflict))
		return
	}

	trainerEarning := plan.Amount * int64(100-common.PlatformFeePercent) / 100
	fee := plan.Amount - trainerEarning

	chargeResult, stripeErr := h.stripe.Charge(ctx, infra_stripe.ChargeParams{
		Amount:          plan.Amount,
		Currency:        plan.Currency,
		PaymentMethodID: body.PaymentMethodID,
		IdempotencyKey:  idempotencyKey,
		Description:     fmt.Sprintf("Subscription: %s", plan.DisplayName),
	})
	if stripeErr != nil {
		h.log.Error("stripe charge failed", "err", stripeErr)
		c.JSON(http.StatusPaymentRequired, api.NewError("payment failed", api.CodePaymentFailed))
		return
	}

	now := time.Now().UTC()
	sub, err := h.repo.CreateSubscription(ctx, db.CreateSubscriptionParams{
		ClientID:           clientID,
		TrainerID:          body.TrainerID,
		PlanType:           body.PlanType,
		SessionsPerMonth:   int32(plan.SessionsTotal),
		Amount:             plan.Amount,
		Currency:           plan.Currency,
		Status:             common.SubscriptionStatusActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			if refundErr := h.stripe.Refund(ctx, chargeResult.TransactionID, "refund:"+idempotencyKey); refundErr != nil {
				h.log.Error("CRITICAL: subscription conflict AND refund failed — manual reconciliation required",
					"stripe_tx", chargeResult.TransactionID, "refund_err", refundErr)
			} else {
				h.log.Warn("concurrent subscription creation — charge refunded", "stripe_tx", chargeResult.TransactionID)
			}
			c.JSON(http.StatusConflict, api.NewError("active subscription already exists for this trainer", api.CodeConflict))
			return
		}
		if refundErr := h.stripe.Refund(ctx, chargeResult.TransactionID, "refund:"+idempotencyKey); refundErr != nil {
			h.log.Error("CRITICAL: charge succeeded but subscription failed AND refund failed — manual reconciliation required",
				"stripe_tx", chargeResult.TransactionID, "err", err, "refund_err", refundErr)
		} else {
			h.log.Error("charge succeeded but subscription creation failed — refunded",
				"stripe_tx", chargeResult.TransactionID, "err", err)
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create subscription", api.CodeServerError))
		return
	}

	payment, payErr := h.repo.CreatePayment(ctx, db.CreatePaymentParams{
		SubscriptionID:        uuid.NullUUID{UUID: sub.ID, Valid: true},
		BookingID:             uuid.NullUUID{},
		PayerID:               clientID,
		Provider:              common.ProviderStripe,
		ProviderTransactionID: sql.NullString{String: chargeResult.TransactionID, Valid: true},
		IdempotencyKey:        idempotencyKey,
		Currency:              plan.Currency,
		TotalAmount:           plan.Amount,
		TrainerEarning:        trainerEarning,
		PlatformFee:           fee,
		PaymentType:           "subscription",
		PaymentStatus:         common.PaymentStatusSuccessful,
	})
	if payErr != nil {
		if refundErr := h.stripe.Refund(ctx, chargeResult.TransactionID, "refund:"+idempotencyKey); refundErr != nil {
			h.log.Error("CRITICAL: charge succeeded but payment record failed AND refund failed — manual reconciliation required",
				"stripe_tx", chargeResult.TransactionID, "err", payErr, "refund_err", refundErr)
		} else {
			h.log.Error("charge succeeded but payment record failed — refunded",
				"stripe_tx", chargeResult.TransactionID, "err", payErr)
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to record payment", api.CodeServerError))
		return
	}

	wallet, walletErr := h.repo.UpsertTrainerWallet(ctx, body.TrainerID, trainerEarning)
	if walletErr != nil {
		paymentID := ""
		if payment != nil {
			paymentID = payment.ID.String()
			if flagErr := h.repo.FlagPaymentForReconciliation(ctx, payment.ID); flagErr != nil {
				h.log.Error("failed to flag payment for reconciliation", "payment_id", paymentID, "err", flagErr)
			}
		}
		h.log.Error("CRITICAL: wallet update failed after successful charge — payment flagged for reconciliation",
			"trainer_id", body.TrainerID,
			"subscription_id", sub.ID,
			"payment_id", paymentID,
			"stripe_tx", chargeResult.TransactionID,
			"amount", trainerEarning,
			"err", walletErr)
	}
	if wallet != nil {
		if _, err := h.repo.CreateLedgerEntry(ctx, db.CreateLedgerEntryParams{
			TrainerID:       body.TrainerID,
			TransactionType: common.LedgerTypeCredit,
			ReferenceType:   common.LedgerRefSubscription,
			ReferenceID:     sub.ID,
			Amount:          trainerEarning,
			BalanceBefore:   wallet.CurrentBalance - trainerEarning,
			BalanceAfter:    wallet.CurrentBalance,
		}); err != nil {
			h.log.Error("failed to create ledger entry", "err", err)
		}
	}

	c.JSON(http.StatusCreated, api.NewSuccess("Subscription created successfully", api.CodeCreated, sub))
}

// GetSubscription GET /subscriptions/:id
func (h *Handler) GetSubscription(c *gin.Context, id openapi_types.UUID) {
	clientID, ok := h.extractUserID(c)
	if !ok {
		return
	}

	sub, err := h.repo.GetSubscriptionByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewError("subscription not found", api.CodeNotFound))
			return
		}
		h.log.Error("failed to get subscription", "id", id, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to retrieve subscription", api.CodeServerError))
		return
	}

	if sub.ClientID != clientID {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Subscription retrieved successfully", api.CodeOK, sub))
}

// CancelSubscription PUT /subscriptions/:id/cancel — immediately cancels the subscription.
func (h *Handler) CancelSubscription(c *gin.Context, id openapi_types.UUID) {
	clientID, ok := h.extractUserID(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	sub, err := h.repo.GetSubscriptionByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewError("subscription not found", api.CodeNotFound))
			return
		}
		h.log.Error("failed to get subscription", "id", id, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to retrieve subscription", api.CodeServerError))
		return
	}

	if sub.ClientID != clientID {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	if sub.Status != common.SubscriptionStatusActive {
		c.JSON(http.StatusBadRequest, api.NewError("only active subscriptions can be cancelled", api.CodeBadRequest))
		return
	}

	updated, err := h.repo.CancelSubscription(ctx, id)
	if err != nil {
		h.log.Error("failed to cancel subscription", "id", id, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to cancel subscription", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Subscription cancelled successfully", api.CodeOK, updated))
}

// ListPayments GET /payments?page=1
func (h *Handler) ListPayments(c *gin.Context) {
	payerID, ok := h.extractUserID(c)
	if !ok {
		return
	}

	page := int32(1)
	if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
		page = int32(p)
	}
	offset := (page - 1) * defaultPageSize

	payments, err := h.repo.ListPayments(c.Request.Context(), payerID, defaultPageSize, offset)
	if err != nil {
		h.log.Error("failed to list payments", "payer_id", payerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to retrieve payments", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Payments retrieved successfully", api.CodeOK, payments))
}

// GetPayment GET /payments/:id
func (h *Handler) GetPayment(c *gin.Context, id openapi_types.UUID) {
	callerID, ok := h.extractUserID(c)
	if !ok {
		return
	}

	payment, err := h.repo.GetPaymentByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewError("payment not found", api.CodeNotFound))
			return
		}
		h.log.Error("failed to get payment", "id", id, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to retrieve payment", api.CodeServerError))
		return
	}

	if payment.PayerID != callerID {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Payment retrieved successfully", api.CodeOK, payment))
}

// TipTrainer POST /bookings/:id/tip — charge Stripe and credit trainer wallet 100%.
func (h *Handler) TipTrainer(c *gin.Context, id openapi_types.UUID) {
	clientID, ok := h.extractUserID(c)
	if !ok {
		return
	}

	var body struct {
		Amount          int64  `json:"amount" binding:"required,min=1,max=999999"`
		Currency        string `json:"currency" binding:"required"`
		PaymentMethodID string `json:"payment_method_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("invalid or missing parameters (amount must be 1-%d)", maxTipAmount), api.CodeBadRequest))
		return
	}

	currency := strings.ToLower(body.Currency)
	if currency != "usd" && currency != "gbp" {
		c.JSON(http.StatusBadRequest, api.NewError("currency must be USD or GBP", api.CodeBadRequest))
		return
	}

	ctx := c.Request.Context()

	booking, err := h.repo.GetBookingByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewError("booking not found", api.CodeNotFound))
			return
		}
		h.log.Error("failed to get booking", "id", id, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to process request", api.CodeServerError))
		return
	}

	if booking.ClientID != clientID {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	if !booking.BookingStatus.Valid || booking.BookingStatus.String != "completed" {
		c.JSON(http.StatusBadRequest, api.NewError("tips can only be sent for completed sessions", api.CodeBadRequest))
		return
	}

	if h.stripe == nil {
		c.JSON(http.StatusInternalServerError, api.NewError("payment provider not configured", api.CodeServerError))
		return
	}

	idempotencyKey := fmt.Sprintf("tip:%s:%s", clientID, id)

	existingPayment, err := h.repo.GetPaymentByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		h.log.Error("failed to check idempotency key", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to process request", api.CodeServerError))
		return
	}
	if existingPayment != nil {
		c.JSON(http.StatusConflict, api.NewError("tip already sent for this session", api.CodeConflict))
		return
	}

	chargeResult, stripeErr := h.stripe.Charge(ctx, infra_stripe.ChargeParams{
		Amount:          body.Amount,
		Currency:        currency,
		PaymentMethodID: body.PaymentMethodID,
		IdempotencyKey:  idempotencyKey,
		Description:     fmt.Sprintf("Tip for booking %s", id),
	})
	if stripeErr != nil {
		h.log.Error("stripe tip charge failed", "err", stripeErr)
		c.JSON(http.StatusPaymentRequired, api.NewError("payment failed", api.CodePaymentFailed))
		return
	}

	payment, err := h.repo.CreatePayment(ctx, db.CreatePaymentParams{
		SubscriptionID:        uuid.NullUUID{},
		BookingID:             uuid.NullUUID{UUID: uuid.UUID(id), Valid: true},
		PayerID:               clientID,
		Provider:              common.ProviderStripe,
		ProviderTransactionID: sql.NullString{String: chargeResult.TransactionID, Valid: true},
		IdempotencyKey:        idempotencyKey,
		Currency:              currency,
		TotalAmount:           body.Amount,
		TrainerEarning:        body.Amount,
		PlatformFee:           0,
		PaymentType:           "tip",
		PaymentStatus:         common.PaymentStatusSuccessful,
	})
	if err != nil {
		if refundErr := h.stripe.Refund(ctx, chargeResult.TransactionID, "refund:"+idempotencyKey); refundErr != nil {
			h.log.Error("CRITICAL: tip charge succeeded but payment record failed AND refund failed — manual reconciliation required",
				"stripe_tx", chargeResult.TransactionID, "err", err, "refund_err", refundErr)
		} else {
			h.log.Error("tip charge succeeded but payment record failed — refunded",
				"stripe_tx", chargeResult.TransactionID, "err", err)
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to record payment", api.CodeServerError))
		return
	}

	wallet, walletErr := h.repo.UpsertTrainerWallet(ctx, booking.TrainerID, body.Amount)
	if walletErr != nil {
		if flagErr := h.repo.FlagPaymentForReconciliation(ctx, payment.ID); flagErr != nil {
			h.log.Error("failed to flag tip payment for reconciliation", "payment_id", payment.ID, "err", flagErr)
		}
		h.log.Error("CRITICAL: wallet update failed after successful tip charge — payment flagged for reconciliation",
			"trainer_id", booking.TrainerID,
			"booking_id", id,
			"payment_id", payment.ID,
			"stripe_tx", chargeResult.TransactionID,
			"amount", body.Amount,
			"err", walletErr)
	}
	if wallet != nil {
		if _, err := h.repo.CreateLedgerEntry(ctx, db.CreateLedgerEntryParams{
			TrainerID:       booking.TrainerID,
			TransactionType: common.LedgerTypeCredit,
			ReferenceType:   common.LedgerRefTip,
			ReferenceID:     uuid.UUID(id),
			Amount:          body.Amount,
			BalanceBefore:   wallet.CurrentBalance - body.Amount,
			BalanceAfter:    wallet.CurrentBalance,
		}); err != nil {
			h.log.Error("failed to create ledger entry for tip", "err", err)
		}
	}

	c.JSON(http.StatusCreated, api.NewSuccess("Tip sent successfully", api.CodeCreated, gin.H{
		"payment_id": payment.ID,
		"amount":     body.Amount,
		"currency":   currency,
	}))
}
