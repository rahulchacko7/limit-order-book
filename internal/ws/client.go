package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Client represents a WebSocket client connection.
type Client struct {
	id   string // Unique client ID
	hub  *Hub
	conn *websocket.Conn
	pair string // Trading pair this client is subscribed to

	// Buffered channel of outbound messages
	send chan []byte

	// Close signal
	done chan struct{}
}

// WebSocket configuration for the upgrader.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins in development (configure appropriately for production)
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// NewClient creates a new WebSocket client.
func NewClient(hub *Hub, conn *websocket.Conn, pair string) *Client {
	return &Client{
		id:   uuid.New().String(),
		hub:  hub,
		conn: conn,
		pair: pair,
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}
}

// ID returns the unique client ID.
func (c *Client) ID() string {
	return c.id
}

// Pair returns the trading pair the client is subscribed to.
func (c *Client) Pair() string {
	return c.pair
}

// ReadPump pumps messages from the WebSocket connection to the hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
		close(c.done)
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("⚠️ WS unexpected close error: %v", err)
			}
			break
		}

		// Handle client messages (e.g., ping, subscribe, unsubscribe)
		c.handleMessage(message)
	}
}

// WritePump pumps messages from the hub to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Write queued messages to current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}

// handleMessage processes incoming messages from the client.
func (c *Client) handleMessage(message []byte) {
	// Parse subscription/unsubscription messages
	var subMsg SubscriptionMessage
	if err := json.Unmarshal(message, &subMsg); err != nil {
		// Not a subscription message, ignore
		log.Printf("📱 Unknown message from client %s: %s", c.id, string(message))
		return
	}

	// Process subscription
	if subMsg.Action == "subscribe" {
		for _, pair := range subMsg.Pairs {
			c.hub.GetSubscriptionManager().Subscribe(c.id, pair)
		}
		// Send acknowledgment
		ack := NewSubscriptionAck("subscribe", true, subMsg.Pairs, "")
		c.send <- ToJSON(ack)
	} else if subMsg.Action == "unsubscribe" {
		for _, pair := range subMsg.Pairs {
			c.hub.GetSubscriptionManager().Unsubscribe(c.id, pair)
		}
		// Send acknowledgment
		ack := NewSubscriptionAck("unsubscribe", true, subMsg.Pairs, "")
		c.send <- ToJSON(ack)
	}
}

// IsConnected checks if the client is still connected.
func (c *Client) IsConnected() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// Close closes the client connection.
func (c *Client) Close() {
	close(c.done)
	c.conn.Close()
}

// WebSocket configuration constants.
const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512 * 1024
)
