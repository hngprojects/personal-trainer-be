package websocket

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hngprojects/personal-trainer-be/internal/common"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins (tune for production)
	},
}

// PendingDeliverer is a function that sends pending notifications
// over the WebSocket connection after upgrade. Implemented by the
// notification service in the routes layer.
type PendingDeliverer func(conn *websocket.Conn, userID uuid.UUID) error

func UpgradeHandler(hub *Hub, log *slog.Logger, deliver PendingDeliverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDVal, ok := c.Get(string(common.ContextKeyUserID))
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			return
		}
		userID, ok := userIDVal.(uuid.UUID)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user ID"})
			return
		}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Warn("WebSocket upgrade failed", "userID", userID, "error", err)
			return
		}
		client := NewClient(hub, conn, userID, log)
		hub.Register(client)
		// Deliver pending 'realtime' notifications on connect
		if deliver != nil {
			if err := deliver(conn, userID); err != nil {
				log.Warn("Failed to deliver pending notifications", "userID", userID, "error", err)
			}
		}
		client.Start()
		client.Wait()
	}
}
