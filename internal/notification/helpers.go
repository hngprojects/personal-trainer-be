package notification

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

func (s *NotificationService) sendNotifViaFCM(ctx context.Context, userID uuid.UUID, notification db.Notification, resp NotificationResponse, title, message string) (*NotificationResponse, error) {
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

func (s *NotificationService) sendNotifViaWS(ctx context.Context, userID uuid.UUID, notification db.Notification, resp NotificationResponse, title, message string) (*NotificationResponse, error) {
	msg, err := json.Marshal(map[string]interface{}{
		"id":         notification.ID,
		"title":      title,
		"message":    message,
		"type":       "notification",
		"created_at": notification.CreatedAt,
	})
	if err != nil {
		s.log.Error("sendNotifViaWS: failed to marshal json body", "error", err)
		return nil, err
	}
	sent := s.wsHub.SendToUser(userID, msg)
	if sent {
		if err := s.repo.UpdateNotificationStatus(ctx, db.UpdateNotificationStatusParams{
			ID:     notification.ID,
			Status: SentNotifStatus,
		}); err != nil {
			s.log.Error("sendNotifViaWS: Failed to update notification status", "error", err)
		}
		resp.Status = SentNotifStatus
	} else {
		resp.Status = notification.Status
	}
	return &resp, nil
}
