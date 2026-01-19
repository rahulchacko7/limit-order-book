package ws

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler provides HTTP handlers for WebSocket connections.
type Handler struct {
	hub *Hub
}

// NewHandler creates a new WebSocket handler.
func NewHandler(hub *Hub) *Handler {
	return &Handler{hub: hub}
}

// HandleUpgrade upgrades an HTTP connection to WebSocket.
// Path: /ws/:pair (e.g., /ws/BTC-USD)
//
// Clients can also subscribe to additional pairs by sending:
//   - {"action":"subscribe","pairs":["ETH-USD"]}
//   - {"action":"unsubscribe","pairs":["BTC-USD"]}
func (h *Handler) HandleUpgrade(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair is required"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("⚠️ WebSocket upgrade error: %v", err)
		return
	}

	client := NewClient(h.hub, conn, pair)
	h.hub.Register(client)

	// Start read and write pumps
	go client.WritePump()
	go client.ReadPump()
}

// HandleUpgradeMultiPair upgrades an HTTP connection to WebSocket with multi-pair support.
// Path: /ws (pairs specified in query parameter)
func (h *Handler) HandleUpgradeMultiPair(c *gin.Context) {
	pairsParam := c.Query("pairs")
	if pairsParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pairs query parameter is required"})
		return
	}

	var pairs []string
	if err := json.Unmarshal([]byte(pairsParam), &pairs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pairs format"})
		return
	}

	if len(pairs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one pair is required"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("⚠️ WebSocket upgrade error: %v", err)
		return
	}

	// Create client with first pair as primary
	client := NewClient(h.hub, conn, pairs[0])
	h.hub.Register(client)

	// Subscribe to additional pairs
	for _, pair := range pairs[1:] {
		h.hub.GetSubscriptionManager().Subscribe(client.ID(), pair)
	}

	// Send initial snapshot
	go h.hub.SendSnapshot(client)

	// Start read and write pumps
	go client.WritePump()
	go client.ReadPump()
}

// HandleStats returns WebSocket connection statistics.
func (h *Handler) HandleStats(c *gin.Context) {
	pair := c.Param("pair")

	if pair != "" {
		c.JSON(http.StatusOK, gin.H{
			"pair":        pair,
			"connections": h.hub.ClientCount(pair),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total_connections": h.hub.TotalClientCount(),
		"pairs":             h.hub.Pairs(),
	})
}

// HandleHealth returns WebSocket hub health status.
func (h *Handler) HandleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":        "healthy",
		"total_clients": h.hub.TotalClientCount(),
	})
}

// HandleSubscriptions returns the subscription manager status.
func (h *Handler) HandleSubscriptions(c *gin.Context) {
	sm := h.hub.GetSubscriptionManager()

	c.JSON(http.StatusOK, gin.H{
		"total_subscriptions": sm.GetTotalClientCount(),
		"pairs":               h.hub.Pairs(),
	})
}
