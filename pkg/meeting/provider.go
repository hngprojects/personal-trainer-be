package meeting

import (
	"context"
	"time"
)

// Provider is the interface every video-call backend must satisfy.
// Swap Zoom for Google Meet (or any other provider) by implementing this.
type Provider interface {
	IsConfigured() bool
	CreateMeeting(ctx context.Context, topic string, startTime time.Time, durationMinutes int) (joinURL, meetingID string, err error)
}

// NoOp is used when no meeting provider is configured — bookings proceed
// without a meeting link.
type NoOp struct{}

func (NoOp) IsConfigured() bool { return false }
func (NoOp) CreateMeeting(_ context.Context, _ string, _ time.Time, _ int) (string, string, error) {
	return "", "", nil
}
