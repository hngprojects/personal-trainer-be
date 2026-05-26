package routes

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Happy path: put then consume returns the same user ID.
func TestStateMemory_PutConsume(t *testing.T) {
	s := newStateMemory()
	uid := uuid.New()
	s.put("alpha", uid, time.Minute)
	got, err := s.consume("alpha")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got != uid {
		t.Fatalf("user_id mismatch: want %s, got %s", uid, got)
	}
}

// State must be single-use — a second consume of the same key fails,
// otherwise a leaked state could be replayed against the callback.
func TestStateMemory_SingleUse(t *testing.T) {
	s := newStateMemory()
	uid := uuid.New()
	s.put("beta", uid, time.Minute)
	_, _ = s.consume("beta")
	if _, err := s.consume("beta"); err == nil {
		t.Fatal("expected second consume to fail (state should be single-use)")
	}
}

// Expired entries must be rejected even if the key still exists in
// the map.
func TestStateMemory_Expired(t *testing.T) {
	s := newStateMemory()
	uid := uuid.New()
	s.put("gamma", uid, 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, err := s.consume("gamma"); err == nil {
		t.Fatal("expected expired state to fail")
	}
}

// Consuming a state we never wrote should fail clean (not panic).
func TestStateMemory_Unknown(t *testing.T) {
	s := newStateMemory()
	if _, err := s.consume("never-set"); err == nil {
		t.Fatal("expected unknown state to fail")
	}
}
