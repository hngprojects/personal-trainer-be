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

// SendResult is the per-token outcome of a fan-out send. Callers use
// InvalidTokens to drive cleanup (mark the corresponding user_device
// row inactive so future pushes skip it). FailedTokens covers
// transient or unclassified failures — don't deactivate on these or
// you'll churn legitimate tokens on a flaky network.
type SendResult struct {
	Sent           int
	Failed         int
	InvalidTokens  []string // tokens FCM said no longer exist — deactivate these
	FailedTokens   []string // other failures (network, quota) — keep for retry
}

func (p *PushNotification) SendToUser(ctx context.Context, deviceToken []string, title, message string) error {
	res, err := p.SendToTokens(ctx, deviceToken, title, message)
	if err != nil {
		return err
	}
	if res.Failed > 0 {
		return fmt.Errorf("failed to send push notification to %d device(s)", res.Failed)
	}
	return nil
}

// SendToTokens fans out the same notification to every supplied
// device token and returns a per-token outcome. Use this in
// preference to SendToUser when the caller needs to act on which
// specific tokens were rejected — typically to mark them inactive
// in the user_device table.
func (p *PushNotification) SendToTokens(ctx context.Context, deviceToken []string, title, message string) (SendResult, error) {
	var res SendResult
	if p.disabled {
		p.log.Warn("Notifier Credential file not set, push notifications will be disabled")
		return res, fmt.Errorf("push notifications are disabled")
	}
	if len(deviceToken) == 0 {
		p.log.Warn("Notifier: no device tokens available to push notification to")
		return res, fmt.Errorf("no device tokens available to push notification to")
	}
	for _, token := range deviceToken {
		response, err := p.client.Send(ctx, buildMessage(token, title, message))
		if err != nil {
			res.Failed++
			if IsTokenPermanentlyInvalid(err) {
				res.InvalidTokens = append(res.InvalidTokens, token)
				p.log.Warn("Notification service: device token is permanently invalid — will be deactivated",
					"err", err, "token_prefix", tokenPrefix(token))
			} else {
				res.FailedTokens = append(res.FailedTokens, token)
				p.log.Error("Notification service: failed to send push notification",
					"err", err, "token_prefix", tokenPrefix(token))
			}
			continue
		}
		res.Sent++
		p.log.Info("Notification service: push notification sent successfully", "response", response)
	}
	return res, nil
}

// IsTokenPermanentlyInvalid reports whether an FCM error indicates the
// device token is no longer routable — the app was uninstalled, the
// token rotated, or the sender id no longer matches. These are NOT
// retryable; the row should be marked inactive so we stop pushing to
// it. Transient errors (network, quota) deliberately don't match
// here — we want to retry those on the next event.
func IsTokenPermanentlyInvalid(err error) bool {
	if err == nil {
		return false
	}
	// messaging.IsRegistrationTokenNotRegistered was deprecated in the
	// firebase-admin SDK in favour of IsUnregistered, which covers
	// the same condition (the FCM service replied "this token isn't
	// known anymore"). Keep both senderID + invalid-argument checks
	// — those catch the rarer "app's senderID doesn't match the
	// project anymore" and "the token isn't a token at all" cases.
	return messaging.IsUnregistered(err) ||
		messaging.IsSenderIDMismatch(err) ||
		messaging.IsInvalidArgument(err)
}

// tokenPrefix safely truncates a token for log output — FCM tokens
// are ~160 chars and confidential, but the first few chars are
// useful when correlating "which row got deactivated" with the log
// line, especially when multiple rows fail in the same call.
func tokenPrefix(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:12] + "…"
}
