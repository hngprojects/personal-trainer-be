package notification

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	fcmnotif "github.com/hngprojects/personal-trainer-be/pkg/notification"
)

type NotificationService struct {
	repo      RepositoryInterface
	log       *slog.Logger
	fcmClient *fcmnotif.PushNotification
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

func NewNotificationService(repo RepositoryInterface, fcmClient *fcmnotif.PushNotification, log *slog.Logger) *NotificationService {
	return &NotificationService{
		repo:      repo,
		log:       log,
		fcmClient: fcmClient,
		// redis: redis
	}
}

type NotificationServiceInterface interface {
	SendNotificationToUser(ctx context.Context, userID uuid.UUID, title, message, idempotency_key string) (*NotificationResponse, error)
	GetUserNotification(ctx context.Context, userID uuid.UUID) (*[]NotificationResponse, error)
	GetUserDevicesToken(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error)
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
	data := &db.CreateNotificationParams{
		UserID:         userID,
		Title:          title,
		Message:        message,
		IdempotencyKey: idempotency_key,
	}
	notification, err := s.repo.CreateNotification(ctx, *data)
	if err != nil {
		return nil, err
	}
	resp := parseNotificationResponse(notification)
	userDevice, err := s.repo.GetUserDeviceToken(ctx, userID)
	if err != nil {
		s.log.Error("Failed to get user device tokens", "error", err)
		return &resp, err
	}

	var tokens []string
	if len(*userDevice) == 0 {
		s.log.Info("No devices found for user", "userID", userID)
		return &resp, nil
	}
	for _, device := range *userDevice {
		if !device.IsPushNotificationEnabled {
			s.log.Info("User has disabled push notifications", "userID", userID, "deviceID", device.ID)
			continue
		}
		tokens = append(tokens, device.DeviceToken)
	}
	if len(tokens) == 0 {
		s.log.Info("No device tokens found for user", "userID", userID)
		return &resp, nil
	}
	if err := s.fcmClient.SendToUser(ctx, tokens, title, message); err != nil {
		s.log.Error("Failed to send notification to user", "userID", userID, "error", err)
		if err := s.repo.UpdateNotificationStatus(ctx, db.UpdateNotificationStatusParams{
			Status: "failed",
			ID:     notification.ID,
		}); err != nil {
			s.log.Error("Failed to update notification status", "error", err)
		}
		return nil, err
	}
	if err := s.repo.UpdateNotificationStatus(ctx, db.UpdateNotificationStatusParams{
		Status: "sent",
		ID:     notification.ID,
	}); err != nil {
		s.log.Error("Failed to update notification status", "error", err)
		// Notification was sent successfully; status update is best-effort
	}
	resp.Status = "sent"
	return &resp, nil
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
