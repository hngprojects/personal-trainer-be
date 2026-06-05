package websocket

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hngprojects/personal-trainer-be/internal/common"
)

// upgrader's Error field is overridden inside UpgradeHandler so failures
// inside gorilla's handshake validation become a structured response on
// our side instead of an opaque "Bad Request" with no body.
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

		// Pre-check the handshake headers BEFORE handing to gorilla's
		// Upgrade(). The default gorilla error writes a terse "Bad
		// handshake" 400 with no diagnostic — clients (and ops!) then
		// can't tell whether the token was wrong, the proxy stripped
		// the Upgrade header, or the call wasn't a WebSocket call at
		// all (e.g. a fetch() / curl). Spelling out the specific
		// header that's missing turns "WS is broken" into an
		// actionable error message.
		if reason := missingHandshakeHeader(c.Request); reason != "" {
			log.Warn("WebSocket handshake rejected — bad request",
				"userID", userID,
				"reason", reason,
				"connection_header", c.GetHeader("Connection"),
				"upgrade_header", c.GetHeader("Upgrade"),
				"sec_ws_version_header", c.GetHeader("Sec-WebSocket-Version"),
				"sec_ws_key_present", c.GetHeader("Sec-WebSocket-Key") != "",
				"user_agent", c.GetHeader("User-Agent"),
			)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":  "websocket handshake failed",
				"reason": reason,
				"hint":   "this endpoint requires a WebSocket upgrade — use new WebSocket(...) not fetch(); if behind a proxy ensure it forwards Connection/Upgrade headers",
			})
			return
		}

		// Localise the upgrader so we can hook its Error callback —
		// the package-level one is shared with anything else that
		// imports this package, so we don't mutate it.
		u := upgrader
		u.Error = func(w http.ResponseWriter, _ *http.Request, status int, reason error) {
			// Mirror gorilla's default body but with structured logging
			// on our side so the failing reason is visible in server
			// logs instead of buried.
			log.Warn("WebSocket upgrade failed inside gorilla validation",
				"userID", userID,
				"status", status,
				"reason", reason.Error(),
			)
			http.Error(w, fmt.Sprintf("websocket: %s", reason), status)
		}

		conn, err := u.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			// Already logged by our Error hook above; just return.
			return
		}
		client := NewClient(hub, conn, userID, log)
		hub.Register(client)
		log.Info("WebSocket connected", "userID", userID, "remote", c.ClientIP())

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

// missingHandshakeHeader returns a human-readable reason if the request
// is missing any of the RFC 6455 handshake headers, or "" when the
// request looks like a real WebSocket upgrade. It's a fast-path check
// — gorilla does the same work later, but its error doesn't tell us
// which header was wrong.
func missingHandshakeHeader(r *http.Request) string {
	// Connection header must be a (case-insensitive) list containing
	// "upgrade" — proxies sometimes append other values like "keep-alive,
	// upgrade", so we tokenise instead of equality-checking.
	connHdr := strings.ToLower(r.Header.Get("Connection"))
	hasUpgradeInConn := false
	for _, tok := range strings.Split(connHdr, ",") {
		if strings.TrimSpace(tok) == "upgrade" {
			hasUpgradeInConn = true
			break
		}
	}
	if !hasUpgradeInConn {
		return "missing or non-'upgrade' Connection header (got: " + r.Header.Get("Connection") + ")"
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return "missing or non-'websocket' Upgrade header (got: " + r.Header.Get("Upgrade") + ")"
	}
	if r.Header.Get("Sec-WebSocket-Key") == "" {
		return "missing Sec-WebSocket-Key header — likely not a real WebSocket client (curl/fetch?)"
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return "missing or unsupported Sec-WebSocket-Version (got: " + r.Header.Get("Sec-WebSocket-Version") + ", want: 13)"
	}
	return ""
}
