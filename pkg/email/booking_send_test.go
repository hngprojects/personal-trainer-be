//go:build integration

package email

import (
	"os"
	"testing"
	"time"
)

// TestSendBookingEmailPreviews sends all 4 platform confirmation emails
// to a test address via Resend.
//
// Run with:
//   go test -run TestSendBookingEmailPreviews -v ./pkg/email/
// (reads RESEND_API_KEY, RESEND_FROM, APP_URL from environment / .env)
func TestSendBookingEmailPreviews(t *testing.T) {
	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("RESEND_FROM")
	appURL := os.Getenv("APP_URL")

	if apiKey == "" || from == "" {
		t.Skip("RESEND_API_KEY or RESEND_FROM not set — skipping live send")
	}

	to := "quoussusiprede-4467@yopmail.com"
	mailer := NewResendMailer(apiKey, from, appURL)

	loc, _ := time.LoadLocation("America/New_York")
	start := time.Date(2026, 7, 21, 15, 0, 0, 0, loc)
	end := start.Add(time.Hour)

	cases := []struct {
		label           string
		platform        string
		meetingLink     string
		messengerHandle string
		phoneNumber     string
	}{
		{
			label:       "WhatsApp",
			platform:    "whatsapp",
			phoneNumber: "+1 (555) 234-5678",
		},
		{
			label:       "Google Meet",
			platform:    "google_meet",
			meetingLink: "https://meet.google.com/abc-defg-hij",
		},
		{
			label:       "FaceTime",
			platform:    "imessage",
			phoneNumber: "coach.alex@fitcall.me",
		},
		{
			label:           "Messenger",
			platform:        "messenger",
			messengerHandle: "coach.alex.fitcall",
		},
	}

	for _, c := range cases {
		err := mailer.SendBookingConfirmation(
			to,
			"Gospel",
			"Coach Alex",
			start,
			end,
			"America/New_York",
			c.platform,
			c.meetingLink,
			c.messengerHandle,
			c.phoneNumber,
			false,
		)
		if err != nil {
			t.Errorf("%s: send failed: %v", c.label, err)
		} else {
			t.Logf("✓ sent %s → %s", c.label, to)
		}
	}
}
