package routes

import (
	"errors"
	"net/http"
	"testing"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	bookingsvc "github.com/hngprojects/personal-trainer-be/internal/bookings"
)

func TestMapDiscoveryBookingErrorSlotUnavailable(t *testing.T) {
	t.Parallel()

	status, code, message, meta := mapDiscoveryBookingError(bookingsvc.ErrSlotUnavailable)
	if status != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, status)
	}
	if code != api.CodeConflict {
		t.Fatalf("expected code %s, got %s", api.CodeConflict, code)
	}
	if message == "" {
		t.Fatal("expected non-empty message")
	}
	if meta != nil {
		t.Fatalf("expected nil meta, got %#v", meta)
	}
}

func TestMapDiscoveryBookingErrorZoomUnavailable(t *testing.T) {
	t.Parallel()

	status, code, _, _ := mapDiscoveryBookingError(bookingsvc.ErrZoomUnavailable)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, status)
	}
	if code != api.CodeServerError {
		t.Fatalf("expected code %s, got %s", api.CodeServerError, code)
	}
}

func TestMapDiscoveryBookingErrorDiscoveryAlreadyUsedIncludesUpgradeMeta(t *testing.T) {
	t.Parallel()

	status, code, _, meta := mapDiscoveryBookingError(&bookingsvc.DiscoveryAlreadyUsedError{
		UpgradeURL: "https://example.com/pricing",
	})
	if status != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, status)
	}
	if code != api.CodeForbidden {
		t.Fatalf("expected code %s, got %s", api.CodeForbidden, code)
	}
	if meta == nil {
		t.Fatal("expected non-nil meta for upgrade prompt")
	}
}

func TestMapDiscoveryBookingErrorFallback(t *testing.T) {
	t.Parallel()

	status, code, _, _ := mapDiscoveryBookingError(errors.New("unknown"))
	if status != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, status)
	}
	if code != api.CodeInternalError {
		t.Fatalf("expected code %s, got %s", api.CodeInternalError, code)
	}
}
