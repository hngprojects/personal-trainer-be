package meeting

import (
	"context"

	"github.com/google/uuid"
)

// Selector picks the right Provider for a given trainer at call time.
// Per-trainer Zoom (when enabled) needs to load and refresh that
// trainer's OAuth grant on every meeting create/delete; the Selector
// is the seam where that choice happens. Callers that have a trainer
// ID in hand ask the Selector for a Provider and then use it like any
// other Provider — they don't need to know whether they got a per-user
// or an org-credentials backend.
//
// The Provider returned is intended to be used immediately and then
// discarded. Don't cache it across requests — a trainer who connects
// Zoom between two booking calls should get the per-user provider on
// the second call, and a Selector held across that boundary won't.
type Selector interface {
	// For returns the Provider that should handle meeting operations
	// for the trainer identified by trainerUserID. Pass uuid.Nil for
	// flows that have no trainer in scope yet (e.g. discovery calls
	// before matching) — implementations should fall back to the org
	// provider in that case.
	For(ctx context.Context, trainerUserID uuid.UUID) Provider
}

// StaticSelector returns the same Provider for every call. Used when
// per-trainer mode is disabled (ZOOM_MEETING_HOST=org) or when no
// per-user credential store has been wired (boot ran without the
// encryption key etc.). Keeps the call sites uniform — they always
// ask the Selector — without forcing a real lookup.
type StaticSelector struct {
	Provider Provider
}

func (s StaticSelector) For(_ context.Context, _ uuid.UUID) Provider {
	if s.Provider == nil {
		return NoOp{}
	}
	return s.Provider
}
