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

func NewPushNotification(credentialFile, projectID string, client *fcm.Client, log *slog.Logger) *PushNotification {
	if client != nil {
		return &PushNotification{
			client: client,
			log:    log,
		}
	}
	if credentialFile == "" {
		log.Warn("PushNotification: credential file not set, push notifications will be disabled")
		return &PushNotification{log: log, disabled: true}
	}
	if projectID == "" {
		log.Warn("PushNotification: project ID not set, push notifications will be disabled")
		return &PushNotification{log: log, disabled: true}
	}
	options := []fcm.Option{fcm.WithCredentialsFile(credentialFile), fcm.WithProjectID(projectID)}

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

func (p *PushNotification) SendToUser(ctx context.Context, deviceToken []string, title, message string) error {
	if p.disabled {
		p.log.Warn("Notifier Credentaial file not set, push notifications will be disabled")
		return fmt.Errorf("push notifications are disabled")
	}
	if len(deviceToken) == 0 {
		p.log.Warn("Notifier: no device tokens available to push notification to")
		return fmt.Errorf("no device tokens available to push notification to")
	}
	failed := 0
	for _, token := range deviceToken {
		response, err := p.client.Send(ctx, &messaging.Message{
			Token: token,
			Notification: &messaging.Notification{
				Title: title,
				Body:  message,
			},
		})
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
