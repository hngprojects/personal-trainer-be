package meeting

import (
	"context"
	"time"
)

// Provider is the interface every video-call backend must satisfy.
type Provider interface {
	IsConfigured() bool
	CreateMeeting(ctx context.Context, topic string, startTime time.Time, durationMinutes int) (joinURL, meetingID string, err error)
	DeleteMeeting(ctx context.Context, meetingID string) error
}

// NoOp is used when no meeting provider is configured.
type NoOp struct{}

func (NoOp) IsConfigured() bool { return false }
func (NoOp) CreateMeeting(_ context.Context, _ string, _ time.Time, _ int) (string, string, error) {
	return "", "", nil
}
func (NoOp) DeleteMeeting(_ context.Context, _ string) error { return nil }
