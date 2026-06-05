package notification

import (
	"context"
	"fmt"
	"log/slog"

	"firebase.google.com/go/v4/messaging"
	fcm "github.com/appleboy/go-fcm"
)

type PushNotification struct {
	client   *fcm.Client
	log      *slog.Logger
	disabled bool
}

func NewPushNotification(credentialJSON []byte, projectID string, client *fcm.Client, log *slog.Logger) *PushNotification {
	if client != nil {
		return &PushNotification{
			client: client,
			log:    log,
		}
	}
	if len(credentialJSON) == 0 {
		log.Warn("PushNotification: credential json not set or encoded, push notifications will be disabled")
		return &PushNotification{log: log, disabled: true}
	}
	if projectID == "" {
		log.Warn("PushNotification: project ID not set, push notifications will be disabled")
		return &PushNotification{log: log, disabled: true}
	}
	options := []fcm.Option{fcm.WithCredentialsJSON(credentialJSON), fcm.WithProjectID(projectID)}

	fcmSvc, err := fcm.NewClient(context.Background(), options...)
	if err != nil {
		log.Error("Notification service: failed to create FCM client", "err", err)
		return &PushNotification{log: log, disabled: true}
	}
	return &PushNotification{
		client: fcmSvc,
		log:    log,
	}
}

// buildMessage assembles an FCM Message with platform-specific
// hints. The same body is sent regardless of the recipient platform
// because the FCM token doesn't tell us what OS the device runs —
// but iOS and Android both ignore the config block that isn't theirs,
// so attaching both is safe and saves a per-device branch.
//
// For iOS (APNs):
//   - sound="default" so the device actually rings the notification
//     instead of delivering it silently to the notification centre,
//     which is what happens when no `aps.sound` field is set.
//   - mutableContent=1 lets a Notification Service Extension on the
//     client modify the body before display (rich notifications,
//     attachments) without a backend change.
//
// For Android, we attach a channel id so high-priority notifications
// on Android 8+ aren't silently down-ranked. The client app must
// register a matching channel at install time — the channel id
// "fitcall_general" is documentation, not magic; if the mobile team
// uses a different one we just need to keep them in sync.
func buildMessage(token, title, body string) *messaging.Message {
	return &messaging.Message{
		Token: token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound:          "default",
					MutableContent: true,
				},
			},
		},
		Android: &messaging.AndroidConfig{
			Priority: "high",
			Notification: &messaging.AndroidNotification{
				ChannelID: "fitcall_general",
				Sound:     "default",
			},
		},
	}
}

// IsDisabled reports whether the notifier was constructed without
// valid credentials. Callers use this to skip the FCM channel cleanly
// instead of relying on the error returned from SendToUser (which
// otherwise spams logs in dev environments that intentionally don't
// configure FCM).
func (p *PushNotification) IsDisabled() bool {
	return p == nil || p.disabled
}

func (p *PushNotification) SendToUser(ctx context.Context, deviceToken []string, title, message string) error {
	if p.disabled {
		p.log.Warn("Notifier Credential file not set, push notifications will be disabled")
		return fmt.Errorf("push notifications are disabled")
	}
	if len(deviceToken) == 0 {
		p.log.Warn("Notifier: no device tokens available to push notification to")
		return fmt.Errorf("no device tokens available to push notification to")
	}
	failed := 0
	for _, token := range deviceToken {
		response, err := p.client.Send(ctx, buildMessage(token, title, message))
		if err != nil {
			failed++
			p.log.Error("Notification service: failed to send push notification", "err", err)
			continue
		}
		p.log.Info("Notification service: push notification sent successfully", "response", response)
	}
	if failed > 0 {
		return fmt.Errorf("failed to send push notification to %d device(s)", failed)
	}
	return nil
}
