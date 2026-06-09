package events

import (
	"sync"

	"github.com/google/uuid"
)

// broker.go
type AvailabilityBroker struct {
	mu      sync.RWMutex
	clients map[uuid.UUID]map[chan string]struct{}
	stopped bool
}

func NewAvailabilityBroker() *AvailabilityBroker {
	return &AvailabilityBroker{
		clients: make(map[uuid.UUID]map[chan string]struct{}),
	}
}

func (b *AvailabilityBroker) Subscribe(trainerID uuid.UUID) chan string {
	ch := make(chan string, 4)
	b.mu.Lock()
	if b.clients[trainerID] == nil {
		b.clients[trainerID] = make(map[chan string]struct{})
	}
	b.clients[trainerID][ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *AvailabilityBroker) Unsubscribe(trainerID uuid.UUID, ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients[trainerID], ch)
	if len(b.clients[trainerID]) == 0 {
		delete(b.clients, trainerID)
	}
	if !b.stopped {
		close(ch) // only close if Stop() hasn't already closed it
	}
}

func (b *AvailabilityBroker) Publish(trainerID uuid.UUID, payload string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients[trainerID] {
		select {
		case ch <- payload:
		default: // client too slow, skip
		}
	}
}

func (b *AvailabilityBroker) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true // mark first so concurrent Unsubscribe calls skip close
	for trainerID, clients := range b.clients {
		for ch := range clients {
			close(ch)
		}
		delete(b.clients, trainerID)
	}
}
