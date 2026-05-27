package websocket

import (
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

type HubInterface interface {
	SendToUser(userID uuid.UUID, message []byte) bool
	UserHasConnections(userID uuid.UUID) bool
}
type Hub struct {
	mu      sync.RWMutex
	clients map[uuid.UUID]map[*Client]struct{} // userID → set of connections
	log     *slog.Logger
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{
		clients: make(map[uuid.UUID]map[*Client]struct{}),
		log:     log,
	}
}
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.userID] == nil {
		h.clients[c.userID] = make(map[*Client]struct{})
	}
	h.clients[c.userID][c] = struct{}{}
	h.log.Info("WebSocket client registered", "userID", c.userID)
}
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if clients, ok := h.clients[c.userID]; ok {
		delete(clients, c)
		if len(clients) == 0 {
			delete(h.clients, c.userID)
		}
	}
	h.log.Info("WebSocket client unregistered", "userID", c.userID)
}

// SendToUser sends a message to all connections for a user.
// Returns true if at least one connection received the message.
func (h *Hub) SendToUser(userID uuid.UUID, message []byte) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.clients[userID]
	if !ok || len(clients) == 0 {
		return false
	}
	delivered := false
	for c := range clients {
		select {
		case c.send <- message:
			delivered = true
		default:
			// client's send buffer is full — skip
			h.log.Warn("WebSocket client buffer full, dropping message", "userID", userID)
		}
	}
	return delivered
}
func (h *Hub) UserHasConnections(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.clients[userID]
	return ok && len(clients) > 0
}
