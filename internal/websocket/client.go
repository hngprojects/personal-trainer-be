package websocket

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
	sendBufferSize = 64
)

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	userID uuid.UUID
	send   chan []byte
	wg     sync.WaitGroup
	log    *slog.Logger
}

func NewClient(hub *Hub, conn *websocket.Conn, userID uuid.UUID, log *slog.Logger) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		userID: userID,
		send:   make(chan []byte, sendBufferSize),
		log:    log,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		if err := c.conn.Close(); err != nil {
			c.log.Warn("failed to close ws connection", "userID", c.userID, "error", err)
		}
		c.wg.Done()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		c.log.Warn("failed to set read deadline", "userID", c.userID, "error", err)
	}
	c.conn.SetPongHandler(func(string) error {
		if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			c.log.Warn("failed to set ws read deadline to pong wait", "userID", c.userID, "error", err)
			return err
		}
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.log.Warn("WebSocket read error", "userID", c.userID, "error", err)
			}
			break
		}
	}
}
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if err := c.conn.Close(); err != nil {
			c.log.Warn("failed to close ws connection", "userID", c.userID, "error", err)
		}
		c.wg.Done()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.log.Warn("failed to set ws read deadline to pong wait", "userID", c.userID, "error", err)
			}
			if !ok {
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					c.log.Warn("WebSocket write error", "userID", c.userID, "error", err)
					return
				}
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				c.log.Warn("WebSocket write error", "userID", c.userID, "error", err)
				return
			}
		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.log.Warn("failed to set write ws deadline", "error", err)
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.log.Warn("failed to set write ws message", "error", err)
				return
			}
		}
	}
}
func (c *Client) Start() {
	c.wg.Add(2)
	go c.ReadPump()
	go c.WritePump()
}
func (c *Client) Wait() {
	c.wg.Wait()
}
