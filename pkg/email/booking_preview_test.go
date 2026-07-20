package email

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBookingEmailPreviews renders all 4 platform templates with sample data
// and writes them to /tmp so you can open them in a browser.
//
// Run with: go test -run TestBookingEmailPreviews -v ./pkg/email/
func TestBookingEmailPreviews(t *testing.T) {
	t.Skip("visual preview only — run manually: go test -run TestBookingEmailPreviews -v ./pkg/email/")
	// Sample booking: tomorrow at 3 PM Eastern
	loc, _ := time.LoadLocation("America/New_York")
	start := time.Date(2026, 7, 21, 15, 0, 0, 0, loc)
	end := start.Add(time.Hour)

	cases := []struct {
		name            string
		platform        string
		meetingLink     string
		messengerHandle string
		phoneNumber     string
	}{
		{
			name:        "WhatsApp",
			platform:    "whatsapp",
			phoneNumber: "+1 (555) 234-5678",
		},
		{
			name:        "GoogleMeet",
			platform:    "google_meet",
			meetingLink: "https://meet.google.com/abc-defg-hij",
		},
		{
			name:        "FaceTime",
			platform:    "imessage",
			phoneNumber: "coach.alex@fitcall.me",
		},
		{
			name:            "Messenger",
			platform:        "messenger",
			messengerHandle: "coach.alex.fitcall",
		},
	}

	outDir := filepath.Join(os.TempDir(), "fitcall-email-previews")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatalf("create preview dir: %v", err)
	}

	for _, c := range cases {
		html, err := bookingConfirmation(
			"Gospel",
			"Coach Alex",
			start,
			end,
			"America/New_York",
			c.platform,
			c.meetingLink,
			c.messengerHandle,
			c.phoneNumber,
			"", // appURL empty → falls back to fitcall.me/logo.svg
			false,
		)
		if err != nil {
			t.Errorf("%s: render error: %v", c.name, err)
			continue
		}

		out := filepath.Join(outDir, c.name+".html")
		if err := os.WriteFile(out, []byte(html), 0644); err != nil {
			t.Errorf("%s: write file: %v", c.name, err)
			continue
		}
		t.Logf("✓ %s → %s", c.name, out)
	}
}
