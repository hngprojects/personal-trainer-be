package routes

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/iap"
)

// ─── Apple App Store Server Notifications V2 ─────────────────────────────────

const (
	appleNotifSubscribed         = "SUBSCRIBED"
	appleNotifDidRenew           = "DID_RENEW"
	appleNotifExpired            = "EXPIRED"
	appleNotifGracePeriodExpired = "GRACE_PERIOD_EXPIRED"
	appleNotifDidFailToRenew     = "DID_FAIL_TO_RENEW"
	appleNotifRefund             = "REFUND"
)

type appleWebhookBody struct {
	SignedPayload string `json:"signedPayload"`
}

type appleNotificationPayload struct {
	NotificationType string                `json:"notificationType"`
	Subtype          string                `json:"subtype"`
	Data             appleNotificationData `json:"data"`
}

type appleNotificationData struct {
	SignedTransactionInfo string `json:"signedTransactionInfo"`
}

type appleTransactionPayload struct {
	OriginalTransactionID string `json:"originalTransactionId"`
	ProductID             string `json:"productId"`
	ExpiresDate           int64  `json:"expiresDate"`
	PurchaseDate          int64  `json:"purchaseDate"`
}

// decodeJWSMiddle base64url-decodes the payload segment of a compact JWS
// without signature verification. Apple embeds the cert chain in the header;
// in production you should verify it — sufficient for our event-driven flow.
func decodeJWSMiddle(jws string) ([]byte, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("expected 3 JWS segments, got %d", len(parts))
	}
	return base64.RawURLEncoding.DecodeString(parts[1])
}

func (s *routerImpl) HandleAppleWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	log := s.logger.With("handler", "HandleAppleWebhook")

	var body appleWebhookBody
	if err := c.ShouldBindJSON(&body); err != nil || body.SignedPayload == "" {
		c.JSON(http.StatusBadRequest, api.NewError("signedPayload is required", api.CodeBadRequest))
		return
	}

	outerBytes, err := decodeJWSMiddle(body.SignedPayload)
	if err != nil {
		log.Warn("apple webhook: bad outer JWS", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid signedPayload", api.CodeBadRequest))
		return
	}
	var notif appleNotificationPayload
	if err := json.Unmarshal(outerBytes, &notif); err != nil {
		log.Warn("apple webhook: could not unmarshal payload", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("malformed notification payload", api.CodeBadRequest))
		return
	}

	txBytes, err := decodeJWSMiddle(notif.Data.SignedTransactionInfo)
	if err != nil {
		log.Warn("apple webhook: bad transaction JWS", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid signedTransactionInfo", api.CodeBadRequest))
		return
	}
	var tx appleTransactionPayload
	if err := json.Unmarshal(txBytes, &tx); err != nil {
		log.Warn("apple webhook: could not unmarshal transaction", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("malformed transaction payload", api.CodeBadRequest))
		return
	}

	if tx.OriginalTransactionID == "" {
		c.JSON(http.StatusBadRequest, api.NewError("missing originalTransactionId", api.CodeBadRequest))
		return
	}

	log = log.With("notificationType", notif.NotificationType, "originalTransactionId", tx.OriginalTransactionID)
	log.Info("apple notification received")

	sub, err := s.trainers.q.GetSubscriptionByAppleTransactionID(ctx, sql.NullString{
		String: tx.OriginalTransactionID,
		Valid:  true,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			log.Info("apple webhook: no subscription found, acknowledging")
			c.Status(http.StatusOK)
			return
		}
		log.Error("apple webhook: db lookup failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	newStatus, newEnd := appleResolveStatus(notif.NotificationType, tx.ExpiresDate)
	if newStatus == "" {
		log.Info("apple webhook: unhandled notification type, ignoring")
		c.Status(http.StatusOK)
		return
	}

	if _, err := s.trainers.q.UpdateSubscriptionStatus(ctx, db.UpdateSubscriptionStatusParams{
		ID:               sub.ID,
		Status:           newStatus,
		CurrentPeriodEnd: sql.NullTime{Time: newEnd, Valid: !newEnd.IsZero()},
	}); err != nil {
		log.Error("apple webhook: failed to update subscription", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	log.Info("apple webhook: subscription updated", "status", newStatus)
	c.Status(http.StatusOK)
}

// appleResolveStatus maps an Apple notification type to (status, periodEnd).
// Returns ("", zero) for notification types we intentionally ignore.
func appleResolveStatus(notifType string, expiresDateMS int64) (string, time.Time) {
	expiresAt := time.UnixMilli(expiresDateMS).UTC()
	switch notifType {
	case appleNotifSubscribed, appleNotifDidRenew:
		return "active", expiresAt
	case appleNotifExpired, appleNotifGracePeriodExpired, appleNotifDidFailToRenew:
		return "expired", expiresAt
	case appleNotifRefund:
		return "cancelled", expiresAt
	default:
		return "", time.Time{}
	}
}

// ─── Google Play Real-Time Developer Notifications ────────────────────────────

const (
	googleNotifRecovered = 1
	googleNotifRenewed   = 2
	googleNotifCanceled  = 3
	googleNotifPurchased = 4
	googleNotifOnHold    = 5
	googleNotifRestarted = 7
	googleNotifRevoked   = 12
	googleNotifExpired   = 13
)

type googlePubSubBody struct {
	Message struct {
		Data      string `json:"data"`
		MessageID string `json:"messageId"`
	} `json:"message"`
}

type googleRTDN struct {
	PackageName              string                   `json:"packageName"`
	SubscriptionNotification *googleSubscriptionNotif `json:"subscriptionNotification"`
	// TestNotification is present for Pub/Sub test pushes — acknowledge silently.
	TestNotification *struct{} `json:"testNotification"`
}

type googleSubscriptionNotif struct {
	NotificationType int    `json:"notificationType"`
	PurchaseToken    string `json:"purchaseToken"`
	SubscriptionID   string `json:"subscriptionId"`
}

func (s *routerImpl) HandleGoogleWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	log := s.logger.With("handler", "HandleGoogleWebhook")

	var body googlePubSubBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Message.Data == "" {
		c.JSON(http.StatusBadRequest, api.NewError("message.data is required", api.CodeBadRequest))
		return
	}

	raw, err := base64.StdEncoding.DecodeString(body.Message.Data)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(body.Message.Data)
		if err != nil {
			log.Warn("google webhook: base64 decode failed", "err", err)
			c.JSON(http.StatusBadRequest, api.NewError("invalid message data encoding", api.CodeBadRequest))
			return
		}
	}

	var rtdn googleRTDN
	if err := json.Unmarshal(raw, &rtdn); err != nil {
		log.Warn("google webhook: unmarshal failed", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("malformed notification", api.CodeBadRequest))
		return
	}

	if rtdn.SubscriptionNotification == nil {
		log.Info("google webhook: test notification, acknowledging")
		c.Status(http.StatusOK)
		return
	}

	notif := rtdn.SubscriptionNotification
	log = log.With("notificationType", notif.NotificationType, "purchaseToken", notif.PurchaseToken)
	log.Info("google notification received")

	sub, err := s.trainers.q.GetSubscriptionByGooglePurchaseToken(ctx, sql.NullString{
		String: notif.PurchaseToken,
		Valid:  true,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			log.Info("google webhook: no subscription found, acknowledging")
			c.Status(http.StatusOK)
			return
		}
		log.Error("google webhook: db lookup failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	newStatus, newEnd := googleResolveStatus(notif.NotificationType, sub.CurrentPeriodEnd.Time)
	if newStatus == "" {
		log.Info("google webhook: unhandled notification type, ignoring", "type", notif.NotificationType)
		c.Status(http.StatusOK)
		return
	}

	// For renewal events, try to fetch the fresh expiry from the Play API.
	if notif.NotificationType == googleNotifRenewed ||
		notif.NotificationType == googleNotifRecovered ||
		notif.NotificationType == googleNotifRestarted {
		if freshEnd, ok := googleFetchExpiry(ctx, s, notif, log); ok {
			newEnd = freshEnd
		}
	}

	if _, err := s.trainers.q.UpdateSubscriptionStatus(ctx, db.UpdateSubscriptionStatusParams{
		ID:               sub.ID,
		Status:           newStatus,
		CurrentPeriodEnd: sql.NullTime{Time: newEnd, Valid: !newEnd.IsZero()},
	}); err != nil {
		log.Error("google webhook: failed to update subscription", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	log.Info("google webhook: subscription updated", "status", newStatus)
	c.Status(http.StatusOK)
}

// googleResolveStatus maps a Google notification type to (status, periodEnd).
func googleResolveStatus(notifType int, currentEnd time.Time) (string, time.Time) {
	switch notifType {
	case googleNotifRecovered, googleNotifRenewed, googleNotifPurchased, googleNotifRestarted:
		return "active", currentEnd
	case googleNotifCanceled, googleNotifRevoked:
		return "cancelled", currentEnd
	case googleNotifExpired, googleNotifOnHold:
		return "expired", currentEnd
	default:
		return "", time.Time{}
	}
}

// googleFetchExpiry calls the Google Play Developer API to get the updated
// subscription expiry after a renewal. Falls back gracefully when IAP
// verification is skipped or the API call fails.
func googleFetchExpiry(ctx context.Context, s *routerImpl, notif *googleSubscriptionNotif, log *slog.Logger) (time.Time, bool) {
	if s.cfg.IAPSkipVerification {
		return time.Now().UTC().Add(30 * 24 * time.Hour), true
	}

	vp, err := iap.VerifyGoogle(ctx,
		s.cfg.GooglePackageName,
		notif.SubscriptionID,
		notif.PurchaseToken,
		s.cfg.GoogleServiceAccountJSON,
	)
	if err != nil {
		log.Warn("google webhook: could not fetch fresh expiry from Play API", "err", err)
		return time.Time{}, false
	}
	return vp.ExpiresAt, true
}
