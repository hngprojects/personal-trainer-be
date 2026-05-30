package bookings

import (
	"fmt"

	"github.com/google/uuid"
)

// JoinLinkBuilder turns a (raw zoom URL, session UUID) pair into
// whatever the booking-confirmation / reschedule emails should show as
// the "Join" target. Centralised so the initial confirmation, paid
// reschedule, and discovery reschedule all produce the same shape —
// without this, a user who reschedules an SDK-mode booking would get
// the universal link on the first email and a raw Zoom URL on the
// second, breaking the in-app-only experience.
//
// JoinMode mirrors cfg.ZoomJoinMode: "sdk" → return a universal link
// pointing at the FitCall app; anything else → return the raw Zoom
// URL untouched. When the recipe is incomplete (missing domain or
// session id) we always fall back to the raw URL so a booking email
// is never blocked on this.
type JoinLinkBuilder struct {
	JoinMode            string
	UniversalLinkDomain string
}

// Build returns the URL to put in the email. Always non-erroring —
// degrades to the raw URL when sdk mode is off or any input is empty.
func (b JoinLinkBuilder) Build(zoomURL string, sessionID uuid.UUID) string {
	if zoomURL == "" {
		return ""
	}
	if b.JoinMode != "sdk" || b.UniversalLinkDomain == "" || sessionID == uuid.Nil {
		return zoomURL
	}
	return fmt.Sprintf("https://%s/sessions/%s/join", b.UniversalLinkDomain, sessionID.String())
}
