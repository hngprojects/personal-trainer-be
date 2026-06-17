package notification

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/apple"
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

	// Partition devices by platform. iOS goes through APNs (when wired);
	// android/web stays on FCM. Both buckets share the same dead-token
	// cleanup pipeline downstream so the routing decision is invisible
	// to the rest of the codebase.
	var iosDevices []db.UserDevice
	var fcmTokens []string
	fcmTokenToDeviceID := make(map[string]uuid.UUID, len(*userDevice))

	for _, device := range *userDevice {
		if !device.IsPushNotificationEnabled {
			s.log.Info("User has disabled push notifications", "userID", userID, "deviceID", device.ID)
			continue
		}
		if device.Platform == "ios" && s.apnsClient != nil {
			iosDevices = append(iosDevices, device)
			continue
		}
		fcmTokens = append(fcmTokens, device.DeviceToken)
		fcmTokenToDeviceID[device.DeviceToken] = device.ID
	}

	if len(iosDevices) == 0 && len(fcmTokens) == 0 {
		s.log.Info("No device tokens with push enabled for user", "userID", userID)
		return &resp, nil
	}

	totalSent, totalFailed := 0, 0

	// ── APNs path (iOS, direct) ──────────────────────────────────────
	for _, device := range iosDevices {
		res, sendErr := s.apnsClient.Send(ctx, device.DeviceToken, title, message)
		if sendErr != nil {
			totalFailed++
			s.log.Warn("APNs send failed", "userID", userID, "deviceID", device.ID, "reason", apnsReason(res), "err", sendErr)
			if res != nil && res.InvalidToken {
				if dErr := s.repo.DeactivateDevice(ctx, device.ID); dErr != nil {
					s.log.Warn("failed to deactivate dead APNs device row", "deviceID", device.ID, "error", dErr)
				} else {
					s.log.Info("deactivated dead APNs device row", "deviceID", device.ID, "userID", userID)
				}
			}
			continue
		}
		totalSent++
	}

	// ── FCM path (Android/web, plus iOS fallback when APNs unwired) ──
	if len(fcmTokens) > 0 {
		result, sendErr := s.fcmClient.SendToTokens(ctx, fcmTokens, title, message)

		for _, dead := range result.InvalidTokens {
			id, ok := fcmTokenToDeviceID[dead]
			if !ok {
				continue
			}
			if err := s.repo.DeactivateDevice(ctx, id); err != nil {
				s.log.Warn("failed to deactivate dead device row", "deviceID", id, "error", err)
			} else {
				s.log.Info("deactivated dead device row", "deviceID", id, "userID", userID)
			}
		}

		totalSent += result.Sent
		totalFailed += result.Failed

		if sendErr != nil && totalSent == 0 {
			s.log.Error("Failed to send notification to user via FCM", "userID", userID, "error", sendErr)
			if uerr := s.repo.UpdateNotificationStatus(ctx, db.UpdateNotificationStatusParams{
				Status: "failed",
				ID:     notification.ID,
			}); uerr != nil {
				s.log.Error("Failed to update notification status", "error", uerr)
			}
			return nil, sendErr
		}
	}

	// Partial success counts as success — at least one device got the
	// message. All-failed is only "failed" if NO channel delivered.
	if totalSent == 0 && totalFailed > 0 {
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

// apnsReason extracts the APNs error reason for log lines without
// blowing up when the result is nil (network errors before we got a
// response from Apple).
func apnsReason(r *apple.SendResult) string {
	if r == nil {
		return ""
	}
	return r.Reason
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
