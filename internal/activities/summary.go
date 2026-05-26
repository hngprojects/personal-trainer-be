package activities

import (
	"fmt"
	"strings"
	"time"
)

// BuildSummary renders a one-line human description for an activity.
// Kept server-side so the wording is identical across mobile and web
// and can move to i18n templates later without touching every client.
//
// Trainer-scope feed gets second-person summaries ("Jane Doe booked a
// session with you"); admin-scope (Trainer set) gets third-person
// ("Jane Doe booked a session with Trainer Mike"). The handler decides
// which to ask for by leaving Trainer nil vs set on the input.
func BuildSummary(a Activity) string {
	actorName := "Someone"
	if a.Actor != nil && strings.TrimSpace(a.Actor.Name) != "" {
		actorName = a.Actor.Name
	}
	trainerSuffix := "you"
	if a.Trainer != nil && strings.TrimSpace(a.Trainer.Name) != "" {
		trainerSuffix = a.Trainer.Name
	}

	switch a.Type {
	case BookingCreated:
		return fmt.Sprintf("%s booked a session with %s%s", actorName, trainerSuffix, eventTimeSuffix(a.EventTime))
	case BookingRescheduled:
		return fmt.Sprintf("%s rescheduled their session with %s%s", actorName, trainerSuffix, eventTimeSuffix(a.EventTime))
	case BookingCancelled:
		base := fmt.Sprintf("%s cancelled their session with %s", actorName, trainerSuffix)
		if a.Extra != "" {
			return base + " — " + a.Extra
		}
		return base
	case DiscoveryBooked:
		return fmt.Sprintf("%s booked a discovery call with %s%s", actorName, trainerSuffix, eventTimeSuffix(a.EventTime))
	case DiscoveryRescheduled:
		return fmt.Sprintf("%s rescheduled their discovery call with %s%s", actorName, trainerSuffix, eventTimeSuffix(a.EventTime))
	case ReviewReceived:
		return fmt.Sprintf("%s left a %s-star review for %s", actorName, a.Extra, trainerSuffix)
	default:
		return string(a.Type)
	}
}

// eventTimeSuffix renders " on Mon, Jan 2 3:04 PM" when the time is
// non-nil and non-zero; empty otherwise. Lives here so every summary
// formats the date the same way and reviewers don't have to scan for
// inconsistencies.
func eventTimeSuffix(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return " on " + t.UTC().Format("Mon, Jan 2 3:04 PM UTC")
}
