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

	if len(*userDevice) == 0 {
		s.log.Info("No active devices found for user", "userID", userID)
		return &resp, nil
	}

	// Build the token list and a parallel index from token → device id
	// so we can deactivate by id when FCM tells us a specific token
	// is dead. Repository.GetUserDeviceToken now returns only active
	// rows so the IsPushNotificationEnabled per-device opt-out is the
	// last remaining filter here.
	var tokens []string
	tokenToDeviceID := make(map[string]uuid.UUID, len(*userDevice))
	for _, device := range *userDevice {
		if !device.IsPushNotificationEnabled {
			s.log.Info("User has disabled push notifications", "userID", userID, "deviceID", device.ID)
			continue
		}
		tokens = append(tokens, device.DeviceToken)
		tokenToDeviceID[device.DeviceToken] = device.ID
	}
	if len(tokens) == 0 {
		s.log.Info("No device tokens with push enabled for user", "userID", userID)
		return &resp, nil
	}

	result, sendErr := s.fcmClient.SendToTokens(ctx, tokens, title, message)

	// Deactivate device rows whose tokens FCM said no longer exist
	// (uninstalled, rotated, sender-id mismatch). We do this BEFORE
	// returning so a future push to this same user skips the dead
	// rows even if the current call ultimately returns an error.
	for _, dead := range result.InvalidTokens {
		id, ok := tokenToDeviceID[dead]
		if !ok {
			continue
		}
		if err := s.repo.DeactivateDevice(ctx, id); err != nil {
			s.log.Warn("failed to deactivate dead device row", "deviceID", id, "error", err)
		} else {
			s.log.Info("deactivated dead device row", "deviceID", id, "userID", userID)
		}
	}

	if sendErr != nil {
		s.log.Error("Failed to send notification to user", "userID", userID, "error", sendErr)
		if uerr := s.repo.UpdateNotificationStatus(ctx, db.UpdateNotificationStatusParams{
			Status: "failed",
			ID:     notification.ID,
		}); uerr != nil {
			s.log.Error("Failed to update notification status", "error", uerr)
		}
		return nil, sendErr
	}

	// Partial success counts as success — at least one device got
	// the message, which is what the user cares about. Per-device
	// failures are already logged inside SendToTokens.
	if result.Sent == 0 && result.Failed > 0 {
		if uerr := s.repo.UpdateNotificationStatus(ctx, db.UpdateNotificationStatusParams{
			Status: "failed",
			ID:     notification.ID,
		}); uerr != nil {
			s.log.Error("Failed to update notification status", "error", uerr)
		}
		return &resp, nil
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
