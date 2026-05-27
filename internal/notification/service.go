package notification

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	ws "github.com/gorilla/websocket"
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
		return nil, err
	}
	resp := parseNotificationResponse(notification)
	if role == ClientUsers {
		return s.sendNotifViaFCM(ctx, userID, notification, resp, title, message)
	}
	return s.sendNotifViaWS(ctx, userID, notification, resp, title, message)
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
