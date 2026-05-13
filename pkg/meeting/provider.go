package meeting

import "time"

// Provider is the interface every video-call backend must satisfy.
// Swap Zoom for Google Meet (or any other provider) by implementing this.
type Provider interface {
	IsConfigured() bool
	CreateMeeting(topic string, startTime time.Time, durationMinutes int) (joinURL, meetingID string, err error)
}

// NoOp is returned when no credentials are configured — callers get empty
// strings and no error, so bookings proceed without a meeting link.
type NoOp struct{}

func (NoOp) IsConfigured() bool { return false }
func (NoOp) CreateMeeting(_ string, _ time.Time, _ int) (string, string, error) {
	return "", "", nil
}
