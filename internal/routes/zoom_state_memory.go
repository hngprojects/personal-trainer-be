package routes

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// stateMemory is a tiny in-memory TTL store for OAuth state nonces.
// Used ONLY when Redis isn't configured (local dev with REDIS_URL
// pointing nowhere). Behaviour is intentionally minimal — a real
// deployment must have Redis so state survives across instances.
//
// Keys are purged opportunistically on each put/consume — there's no
// background sweeper. With a 10-minute TTL and dev-only traffic the
// map stays tiny enough that this is fine.
type stateMemory struct {
	mu      sync.Mutex
	entries map[string]stateEntry
}

type stateEntry struct {
	userID    uuid.UUID
	expiresAt time.Time
}

func newStateMemory() *stateMemory {
	return &stateMemory{entries: make(map[string]stateEntry)}
}

func (s *stateMemory) put(state string, userID uuid.UUID, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	s.entries[state] = stateEntry{userID: userID, expiresAt: time.Now().Add(ttl)}
}

func (s *stateMemory) consume(state string) (uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	e, ok := s.entries[state]
	if !ok {
		return uuid.Nil, errors.New("state not found")
	}
	delete(s.entries, state)
	if time.Now().After(e.expiresAt) {
		return uuid.Nil, errors.New("state expired")
	}
	return e.userID, nil
}

func (s *stateMemory) purgeLocked() {
	now := time.Now()
	for k, v := range s.entries {
		if now.After(v.expiresAt) {
			delete(s.entries, k)
		}
	}
}
