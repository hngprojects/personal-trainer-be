package notification

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	ws "github.com/gorilla/websocket"
	"github.com/lib/pq"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/websocket"
	fcmnotif "github.com/hngprojects/personal-trainer-be/pkg/notification"
)

const (
	ClientUsers           = "client"
	TrainrUsers           = "trainer"
	AdminUsers            = "admin"
	PushNotifications     = "push"
	RealTimeNotifications = "realtime"
	SentNotifStatus       = "sent"
)

type NotificationService struct {
	repo      RepositoryInterface
	log       *slog.Logger
	fcmClient *fcmnotif.PushNotification
	wsHub     websocket.HubInterface
	// redis *redis.Client
}

type NotificationResponse struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	Title     string     `json:"title"`
	Message   string     `json:"message"`
	Status    string     `json:"status"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func NewNotificationService(repo RepositoryInterface, fcmClient *fcmnotif.PushNotification, ws websocket.HubInterface, log *slog.Logger) *NotificationService {
	return &NotificationService{
		repo:      repo,
		log:       log,
		fcmClient: fcmClient,
		wsHub:     ws,
		// redis: redis
	}
}

type NotificationServiceInterface interface {
	SendNotificationToUser(ctx context.Context, userID uuid.UUID, title, message, idempotency_key string) (*NotificationResponse, error)
	SendNotificationToAdmins(ctx context.Context, title, message, idempotencyKeyBase string) (int, error)
	GetUserNotification(ctx context.Context, userID uuid.UUID) (*[]NotificationResponse, error)
	GetUserDevicesToken(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error)
	DeliverPendingNotifOnConn(conn *ws.Conn, userID uuid.UUID) error
}

func (s *NotificationService) GetUserDevicesToken(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error) {
	devices, err := s.repo.GetUserDeviceToken(ctx, userID)
	if err != nil {
		s.log.Error("Failed to get user device tokens", "userID", userID, "error", err)
		return nil, err
	}
	if len(*devices) == 0 {
		return []db.UserDevice{}, nil
	}
	return *devices, nil
}

func (s *NotificationService) SendNotificationToUser(ctx context.Context, userID uuid.UUID, title, message, idempotency_key string) (*NotificationResponse, error) {
	role, err := s.repo.GetUserRoleByUserID(ctx, userID)
	if err != nil {
		s.log.Warn("failed to get role of user")
		return nil, err
	}
	notificationType := PushNotifications
	if role != ClientUsers {
		notificationType = RealTimeNotifications
	}
	data := &db.CreateNotificationWithTypeParams{
		UserID:         userID,
		Title:          title,
		Message:        message,
		IdempotencyKey: idempotency_key,
		Type:           notificationType,
	}
	notification, err := s.repo.CreateNotificationWithType(ctx, *data)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrDuplicateIdempotencyKey
		}
		return nil, err
	}
	resp := parseNotificationResponse(notification)
	if role == ClientUsers {
		return s.sendNotifViaFCM(ctx, userID, notification, resp, title, message)
	}
	return s.sendNotifViaWS(ctx, userID, notification, resp, title, message)
}

// SendNotificationToAdmins delivers the same notification to every
// user with role admin OR super_admin. Used by event sites that don't
// know a specific admin to address — new bookings, subscription
// lifecycle events, Zoom connect, etc.
//
// Each admin gets a distinct DB row keyed by
// `<idempotencyKeyBase>-admin-<their_user_id>`. The table-wide UNIQUE
// constraint on idempotency_key would otherwise reject all but the
// first INSERT when broadcasting the same event.
//
// Failures for individual admins are logged and the loop continues —
// one admin's missing device tokens or expired WS connection won't
// block notifying the others. Returns the count of admins for whom
// the DB insert succeeded (excludes duplicate-key collisions, which
// just mean "already delivered on a prior call with the same base").
func (s *NotificationService) SendNotificationToAdmins(ctx context.Context, title, message, idempotencyKeyBase string) (int, error) {
	admins, err := s.repo.ListAdminUserIDs(ctx)
	if err != nil {
		s.log.Error("admin broadcast: failed to list admin user ids", "error", err)
		return 0, err
	}
	if len(admins) == 0 {
		s.log.Warn("admin broadcast: no admin users in the system", "title", title)
		return 0, nil
	}
	sent := 0
	for _, adminID := range admins {
		key := idempotencyKeyBase + "-admin-" + adminID.String()
		if _, err := s.SendNotificationToUser(ctx, adminID, title, message, key); err != nil {
			if errors.Is(err, ErrDuplicateIdempotencyKey) {
				// This admin already got the row on a prior call —
				// not a failure, just a re-fire of the same event.
				continue
			}
			s.log.Warn("admin broadcast: send failed for one admin",
				"adminID", adminID, "title", title, "error", err)
			continue
		}
		sent++
	}
	return sent, nil
}

func (s *NotificationService) GetUserNotification(ctx context.Context, userID uuid.UUID) (*[]NotificationResponse, error) {
	notifications, err := s.repo.GetUserNotification(ctx, userID)
	if err != nil {
		s.log.Error("failed to fetch user notifications", "userID", userID, "error", err)
		return nil, err
	}
	if len(*notifications) == 0 {
		empty := []NotificationResponse{}
		return &empty, nil
	}

	resp := make([]NotificationResponse, 0, len(*notifications))
	for _, notification := range *notifications {
		r := parseNotificationResponse(notification)
		resp = append(resp, r)
	}
	return &resp, nil
}

func (s *NotificationService) DeliverPendingNotifOnConn(conn *ws.Conn, userID uuid.UUID) error {
	ctx := context.Background()
	notifications, err := s.repo.GetUserPendingNotification(ctx, userID)
	if err != nil {
		return err
	}
	for _, n := range notifications {
		msg, err := json.Marshal(map[string]interface{}{
			"id": n.ID, "title": n.Title, "message": n.Message,
			"type": "notification", "created_at": n.CreatedAt,
		})
		if err != nil {
			s.log.Error("marshal pending notification", "error", err)
			continue
		}
		if err := conn.WriteMessage(ws.TextMessage, msg); err != nil {
			return err
		}
		if err := s.repo.UpdateNotificationStatus(ctx, db.UpdateNotificationStatusParams{
			Status: "sent", ID: n.ID,
		}); err != nil {
			s.log.Warn("could not update notification status to sent", "notificationID", n.ID, "error", err)
		}
	}
	return nil
}

func parseNotificationResponse(data db.Notification) NotificationResponse {
	response := NotificationResponse{
		ID:        data.ID,
		UserID:    data.UserID,
		Title:     data.Title,
		Message:   data.Message,
		Status:    data.Status,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
	}
	if data.SentAt.Valid {
		response.SentAt = &data.SentAt.Time
	}
	return response
}
