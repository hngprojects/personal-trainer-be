package zoomflow

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
	"github.com/hngprojects/personal-trainer-be/pkg/zoom"
)

// MeetingSelector implements meeting.Selector with a trainer-first,
// org-fallback policy when PreferTrainer is true.
//
// Why fallback (not hard fail) when a trainer hasn't connected:
// rolling out per-trainer Zoom across an org that already had the
// org-credentials flow in production should NOT silently break
// bookings for trainers who haven't gone through the Connect Zoom
// flow yet. The selector quietly downgrades to org so the booking
// still gets a working meeting URL; ops can dashboard "% of bookings
// using trainer-owned Zoom" separately and chase the holdouts.
//
// Why fallback also when Status returns an error: same reason —
// transient DB blip during a booking shouldn't 5xx the user. The
// org provider will either succeed or fail loudly with its own error
// path. Logging happens at debug level since it's expected during the
// rollout window.
type MeetingSelector struct {
	// Store is the per-user credential store. May be nil when the
	// encryption key / OAuth client weren't configured at boot — in
	// that case the selector always returns OrgProvider regardless
	// of PreferTrainer (you can't host as a trainer with no token
	// pipeline).
	Store *CredentialStore

	// OrgProvider is the safety net. Must not be nil; if you really
	// want "no meetings", pass meeting.NoOp{}.
	OrgProvider meeting.Provider

	// PreferTrainer mirrors the ZOOM_MEETING_HOST=trainer config.
	// When false, the selector behaves like a meeting.StaticSelector
	// wrapping OrgProvider.
	PreferTrainer bool

	Log *slog.Logger
}

// For implements meeting.Selector. The platform argument is part of
// the multi-platform selector interface but ignored here — this
// selector only ever returns Zoom providers (the org Zoom or a per-
// trainer Zoom). MultiPlatformSelector is responsible for never
// invoking us with a non-Zoom platform.
func (s *MeetingSelector) For(ctx context.Context, trainerUserID uuid.UUID, _ string) meeting.Provider {
	if !s.PreferTrainer || s.Store == nil || trainerUserID == uuid.Nil {
		return s.OrgProvider
	}

	// Status is a single SELECT with no token refresh — safe to do on
	// the booking hot path. We only consult it to decide WHICH provider
	// to return; if we return the user provider, the actual token-fetch
	// (and refresh, if expired) happens lazily inside UserProvider on
	// its first API call, where errors are already plumbed back to the
	// caller.
	connected, _, _, err := s.Store.Status(ctx, trainerUserID)
	if err != nil {
		if s.Log != nil {
			s.Log.Debug("zoomflow: selector status lookup failed, falling back to org", "user_id", trainerUserID, "err", err)
		}
		return s.OrgProvider
	}
	if !connected {
		return s.OrgProvider
	}
	return zoom.NewUserProvider(s.Store.NewUserTokenSource(trainerUserID))
}
