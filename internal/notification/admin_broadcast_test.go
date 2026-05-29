package notification_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/hngprojects/personal-trainer-be/internal/notification"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// SendNotificationToAdmins must fan out one DB row per admin and
// suffix each row's idempotency key with that admin's user_id so the
// table-wide UNIQUE(idempotency_key) constraint doesn't collide.
func TestAdminBroadcast_OnePerAdmin_KeySuffixed(t *testing.T) {
	admin1, admin2, admin3 := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()

	var seenKeys []string
	var mu sync.Mutex

	repo := &mockRepository{
		listAdminUserIDsFn: func(_ context.Context) ([]uuid.UUID, error) {
			return []uuid.UUID{admin1, admin2, admin3}, nil
		},
		// SendNotificationToUser calls GetUserRoleByUserID first; both
		// "admin" and "super_admin" roles route via WebSocket, so any
		// non-client role suffices.
		getUserRoleByUserIDFn: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "admin", nil
		},
		createNotificationWithTypeFn: func(_ context.Context, args db.CreateNotificationWithTypeParams) (db.Notification, error) {
			mu.Lock()
			seenKeys = append(seenKeys, args.IdempotencyKey)
			mu.Unlock()
			return db.Notification{ID: uuid.New(), CreatedAt: now, UpdatedAt: now}, nil
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), &mockWSHub{}, testLogger())

	sent, err := svc.SendNotificationToAdmins(context.Background(),
		"Test Title", "Test Message", "discovery-booked-XYZ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent != 3 {
		t.Fatalf("want 3 sends, got %d", sent)
	}
	if len(seenKeys) != 3 {
		t.Fatalf("want 3 DB inserts, got %d (keys=%v)", len(seenKeys), seenKeys)
	}
	for _, k := range seenKeys {
		if !strings.HasPrefix(k, "discovery-booked-XYZ-admin-") {
			t.Errorf("key %q missing required `<base>-admin-<uuid>` shape", k)
		}
	}
	// All three keys must be distinct — otherwise broadcasting the
	// same base event to multiple admins collides on UNIQUE.
	uniq := map[string]bool{}
	for _, k := range seenKeys {
		uniq[k] = true
	}
	if len(uniq) != 3 {
		t.Fatalf("duplicate idempotency keys generated: %v", seenKeys)
	}
}

// Empty admin list is not a failure — system might be early-stage. We
// log and return 0 sent. No panic.
func TestAdminBroadcast_NoAdminsIsZero(t *testing.T) {
	repo := &mockRepository{
		listAdminUserIDsFn: func(_ context.Context) ([]uuid.UUID, error) {
			return []uuid.UUID{}, nil
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), &mockWSHub{}, testLogger())

	sent, err := svc.SendNotificationToAdmins(context.Background(), "x", "y", "z")
	if err != nil {
		t.Fatalf("unexpected error on empty admin list: %v", err)
	}
	if sent != 0 {
		t.Fatalf("want 0 sends on empty admin list, got %d", sent)
	}
}

// ListAdminUserIDs error short-circuits with err — there's no useful
// degraded path when we can't even look up who to notify.
func TestAdminBroadcast_ListErrorPropagates(t *testing.T) {
	repo := &mockRepository{
		listAdminUserIDsFn: func(_ context.Context) ([]uuid.UUID, error) {
			return nil, errors.New("db down")
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), &mockWSHub{}, testLogger())

	sent, err := svc.SendNotificationToAdmins(context.Background(), "x", "y", "z")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if sent != 0 {
		t.Fatalf("want 0 sends on lookup failure, got %d", sent)
	}
}

// One admin failing must not stop the loop — the others still get
// their row. Verified by counting successful inserts via the mock.
func TestAdminBroadcast_PartialFailureLoopContinues(t *testing.T) {
	admins := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	failOn := admins[1]

	var sentCount int
	var mu sync.Mutex

	repo := &mockRepository{
		listAdminUserIDsFn: func(_ context.Context) ([]uuid.UUID, error) {
			return admins, nil
		},
		getUserRoleByUserIDFn: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "admin", nil
		},
		createNotificationWithTypeFn: func(_ context.Context, args db.CreateNotificationWithTypeParams) (db.Notification, error) {
			// Decide based on whose key it is — the suffix carries the user ID.
			if strings.Contains(args.IdempotencyKey, failOn.String()) {
				return db.Notification{}, errors.New("write failed for admin 2")
			}
			mu.Lock()
			sentCount++
			mu.Unlock()
			return db.Notification{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), &mockWSHub{}, testLogger())

	sent, err := svc.SendNotificationToAdmins(context.Background(), "x", "y", "evt-1")
	if err != nil {
		t.Fatalf("unexpected outer error (per-admin failures must not bubble): %v", err)
	}
	if sent != 2 {
		t.Fatalf("want 2 successful sends out of 3 (one was forced to fail), got %d", sent)
	}
	if sentCount != 2 {
		t.Fatalf("mock counted %d successful inserts, want 2", sentCount)
	}
}

// Duplicate-idempotency-key on one admin doesn't count as a failure —
// it just means "already delivered" from a prior call.
func TestAdminBroadcast_DuplicateKeyTreatedAsAlreadyDelivered(t *testing.T) {
	admins := []uuid.UUID{uuid.New(), uuid.New()}
	dupOn := admins[0]

	var sentCount int
	repo := &mockRepository{
		listAdminUserIDsFn: func(_ context.Context) ([]uuid.UUID, error) {
			return admins, nil
		},
		getUserRoleByUserIDFn: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "super_admin", nil
		},
		createNotificationWithTypeFn: func(_ context.Context, args db.CreateNotificationWithTypeParams) (db.Notification, error) {
			if strings.Contains(args.IdempotencyKey, dupOn.String()) {
				return db.Notification{}, &pq.Error{Code: "23505"}
			}
			sentCount++
			return db.Notification{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), &mockWSHub{}, testLogger())

	sent, err := svc.SendNotificationToAdmins(context.Background(), "x", "y", "evt-dup")
	if err != nil {
		t.Fatalf("duplicate key should NOT bubble: %v", err)
	}
	// dup is excluded from the count (it's not a true failure, but
	// it's not a new send either).
	if sent != 1 {
		t.Fatalf("want 1 fresh send (1 dup, 1 new), got %d", sent)
	}
	if sentCount != 1 {
		t.Fatalf("mock counted %d real inserts, want 1", sentCount)
	}
}
