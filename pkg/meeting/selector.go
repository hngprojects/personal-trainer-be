package meeting

import (
	"context"

	"github.com/google/uuid"
)

// Platform names — keep these in sync with the bookings.session_platform
// and discovery_bookings.contact_mode CHECK constraints.
const (
	PlatformZoom      = "zoom"
	PlatformGoogleMeet = "google_meet"
	// Messenger is intentionally NOT here. It's a contact channel
	// (client-supplied handle), not a meeting provider; handlers
	// short-circuit before consulting the Selector when they see it.
)

// Selector picks the right Provider for a given (trainer, platform)
// at call time.
//
// The platform dimension was added when Google Meet joined the mix.
// Zoom kept its per-trainer-OAuth fallback path; Meet uses a single
// org refresh token. The Selector hides that asymmetry from the
// booking handlers — they ask for "the provider for trainer X on
// platform Y" and use whatever they get back.
//
// The Provider returned is intended to be used immediately and then
// discarded. Don't cache it across requests — a trainer who connects
// Zoom between two booking calls should get the per-user provider on
// the second call, and a Selector held across that boundary won't.
type Selector interface {
	// For returns the Provider that should handle meeting operations
	// for the trainer identified by trainerUserID on the named
	// platform. Pass uuid.Nil for trainerUserID in flows that have no
	// trainer in scope yet (discovery before matching) — Zoom
	// implementations fall back to the org account in that case.
	//
	// Unknown platforms return NoOp; the caller checks IsConfigured()
	// to surface a 503.
	For(ctx context.Context, trainerUserID uuid.UUID, platform string) Provider
}

// StaticSelector returns the same Provider for every call regardless
// of trainer or platform. Used in tests and in degraded boot states
// where the real selector dependency isn't available.
type StaticSelector struct {
	Provider Provider
}

func (s StaticSelector) For(_ context.Context, _ uuid.UUID, _ string) Provider {
	if s.Provider == nil {
		return NoOp{}
	}
	return s.Provider
}

// MultiPlatformSelector dispatches to one Provider per platform.
// Built at boot time with whatever providers happen to be configured;
// missing entries return NoOp.
//
// Zoom-on-this-selector still benefits from the per-trainer fallback
// when constructed via zoomflow.MeetingSelector + adapter — the
// adapter implements Provider directly so the platform map can hold
// it like any other backend.
type MultiPlatformSelector struct {
	// Zoom is the seam for the per-trainer-or-org Zoom flow. Holds a
	// nested Selector because zoomflow.MeetingSelector needs to do its
	// own per-trainer lookup. Wrapped with a small adapter inside For().
	Zoom Selector
	// Meet is the single org Provider — no per-trainer dimension since
	// MEET_HOST is always 'org' in our deployment.
	Meet Provider
}

func (m MultiPlatformSelector) For(ctx context.Context, trainerUserID uuid.UUID, platform string) Provider {
	switch platform {
	case PlatformZoom:
		if m.Zoom == nil {
			return NoOp{}
		}
		return m.Zoom.For(ctx, trainerUserID, PlatformZoom)
	case PlatformGoogleMeet:
		if m.Meet == nil {
			return NoOp{}
		}
		return m.Meet
	default:
		return NoOp{}
	}
}
