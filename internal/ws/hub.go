package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"limit-order-book/internal/cache"
	"limit-order-book/internal/engine"
	"limit-order-book/internal/models"
)

// PriceLevel represents a price level in the order book.
type PriceLevel struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

// Hub maintains the set of active clients and broadcasts messages to clients.
// This implements the publisher side of the pub/sub pattern for WebSocket updates.
type Hub struct {
	// Registered clients by pair (e.g., "BTC-USD", "ETH-USD")
	clients map[string]map[*Client]bool

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Broadcast message to all clients (for trade events)
	broadcast chan []byte

	// Subscription manager for per-pair subscriptions
	subscriptionManager *SubscriptionManager

	// Order book manager for snapshot data
	orderBookManager *engine.OrderBookManager

	// Redis cache for recent trades
	redisCache *cache.RedisCache

	// Heartbeat sequence number
	heartbeatSeq int64

	// Heartbeat ticker
	heartbeatTicker *time.Ticker

	// Stop channel for graceful shutdown
	stop chan struct{}

	// Mutex for thread-safe operations
	mu sync.RWMutex
}

// HubConfig holds configuration for the hub.
type HubConfig struct {
	HeartbeatInterval time.Duration // Heartbeat interval (default: 30s)
	SnapshotLevels    int           // Number of price levels in snapshot (default: 20)
	RecentTradesLimit int64         // Number of recent trades in snapshot (default: 50)
}

// DefaultHubConfig returns default hub configuration.
func DefaultHubConfig() *HubConfig {
	return &HubConfig{
		HeartbeatInterval: 30 * time.Second,
		SnapshotLevels:    20,
		RecentTradesLimit: 50,
	}
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		clients:             make(map[string]map[*Client]bool),
		register:            make(chan *Client),
		unregister:          make(chan *Client),
		broadcast:           make(chan []byte, 256),
		subscriptionManager: NewSubscriptionManager(),
		heartbeatSeq:        0,
		heartbeatTicker:     time.NewTicker(30 * time.Second),
		stop:                make(chan struct{}),
	}
}

// NewHubWithConfig creates a new Hub with custom configuration.
func NewHubWithConfig(cfg *HubConfig, obManager *engine.OrderBookManager, redisCache *cache.RedisCache) *Hub {
	if cfg == nil {
		cfg = DefaultHubConfig()
	}

	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}

	return &Hub{
		clients:             make(map[string]map[*Client]bool),
		register:            make(chan *Client),
		unregister:          make(chan *Client),
		broadcast:           make(chan []byte, 256),
		subscriptionManager: NewSubscriptionManager(),
		orderBookManager:    obManager,
		redisCache:          redisCache,
		heartbeatSeq:        0,
		heartbeatTicker:     time.NewTicker(cfg.HeartbeatInterval),
		stop:                make(chan struct{}),
	}
}

// Run starts the hub's main event loop with heartbeat.
func (h *Hub) Run() {
	for {
		select {
		case <-h.stop:
			h.heartbeatTicker.Stop()
			log.Println("📡 WebSocket hub stopped")
			return

		case <-h.heartbeatTicker.C:
			h.heartbeatSeq++
			h.BroadcastHeartbeat(h.heartbeatSeq)

		case client := <-h.register:
			h.mu.Lock()
			if h.clients[client.pair] == nil {
				h.clients[client.pair] = make(map[*Client]bool)
			}
			h.clients[client.pair][client] = true
			log.Printf("📱 WS client registered for pair %s (total: %d)", client.pair, len(h.clients[client.pair]))
			h.mu.Unlock()

			// Send initial snapshot to new client
			go h.SendSnapshot(client)

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.clients[client.pair]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					close(client.send)
					log.Printf("📱 WS client unregistered for pair %s", client.pair)
				}
				// Clean up empty pair maps
				if len(clients) == 0 {
					delete(h.clients, client.pair)
				}
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.broadcastToAll(message)
		}
	}
}

// Stop gracefully stops the hub.
func (h *Hub) Stop() {
	close(h.stop)
}

// SendSnapshot sends an order book snapshot to a client.
func (h *Hub) SendSnapshot(client *Client) {
	if client == nil {
		return
	}

	pair := client.pair
	levels := 20 // Default levels
	if h.orderBookManager != nil {
		ob := h.orderBookManager.GetOrderBook(pair)
		if ob != nil {
			bids, asks := ob.GetDepth(levels)
			h.sendSnapshotToClient(client, pair, bids, asks)
		}
	}
}

// sendSnapshotToClient sends a snapshot event to a specific client.
func (h *Hub) sendSnapshotToClient(client *Client, pair string, bids, asks []engine.OrderBookLevel) {
	// Convert to price levels
	bidsLevels := make([]PriceLevel, 0, len(bids))
	for _, b := range bids {
		bidsLevels = append(bidsLevels, PriceLevel{Price: b.Price, Volume: b.Volume})
	}

	asksLevels := make([]PriceLevel, 0, len(asks))
	for _, a := range asks {
		asksLevels = append(asksLevels, PriceLevel{Price: a.Price, Volume: a.Volume})
	}

	// Get recent trades
	var recentTrades []TradeInfo
	if h.redisCache != nil {
		trades, _ := h.redisCache.GetRecentTrades(pair, 50)
		recentTrades = make([]TradeInfo, 0, len(trades))
		for i, t := range trades {
			recentTrades = append(recentTrades, TradeInfo{
				ID:        int64(i + 1), // Synthetic ID since Trade model doesn't have ID
				Price:     t.Price,
				Quantity:  t.Quantity,
				Side:      "unknown", // Trade model doesn't have side
				CreatedAt: t.CreatedAt,
			})
		}
	}

	// Create and send snapshot event
	snapshot := NewSnapshotEvent(pair, bidsLevels, asksLevels, recentTrades, h.heartbeatSeq)
	data, _ := json.Marshal(snapshot)

	select {
	case client.send <- data:
		log.Printf("📱 Sent snapshot to client %s for pair %s", client.id, pair)
	default:
		log.Printf("⚠️ Failed to send snapshot to client %s, buffer full", client.id)
	}
}

// GetSnapshot returns a snapshot for a trading pair.
func (h *Hub) GetSnapshot(pair string) *SnapshotEvent {
	if h.orderBookManager == nil {
		return nil
	}

	ob := h.orderBookManager.GetOrderBook(pair)
	if ob == nil {
		return nil
	}

	bids, asks := ob.GetDepth(20)

	// Convert to price levels
	bidsLevels := make([]PriceLevel, 0, len(bids))
	for _, b := range bids {
		bidsLevels = append(bidsLevels, PriceLevel{Price: b.Price, Volume: b.Volume})
	}

	asksLevels := make([]PriceLevel, 0, len(asks))
	for _, a := range asks {
		asksLevels = append(asksLevels, PriceLevel{Price: a.Price, Volume: a.Volume})
	}

	// Get recent trades
	var recentTrades []TradeInfo
	if h.redisCache != nil {
		trades, _ := h.redisCache.GetRecentTrades(pair, 50)
		recentTrades = make([]TradeInfo, 0, len(trades))
		for i, t := range trades {
			recentTrades = append(recentTrades, TradeInfo{
				ID:        int64(i + 1), // Synthetic ID since Trade model doesn't have ID
				Price:     t.Price,
				Quantity:  t.Quantity,
				Side:      "unknown",
				CreatedAt: t.CreatedAt,
			})
		}
	}

	return NewSnapshotEvent(pair, bidsLevels, asksLevels, recentTrades, h.heartbeatSeq)
}

// broadcastToAll sends a message to all connected clients.
func (h *Hub) broadcastToAll(message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for pair, clients := range h.clients {
		for client := range clients {
			select {
			case client.send <- message:
			default:
				// Client's send buffer is full, skip this message
				log.Printf("⚠️ WS client %s send buffer full, skipping", pair)
			}
		}
	}
}

// BroadcastToPair sends a message to all clients subscribed to a specific pair.
func (h *Hub) BroadcastToPair(pair string, message []byte) {
	h.mu.RLock()
	clients, ok := h.clients[pair]
	h.mu.RUnlock()

	if !ok {
		return
	}

	for client := range clients {
		select {
		case client.send <- message:
		default:
			log.Printf("⚠️ WS client send buffer full for pair %s, skipping", pair)
		}
	}
}

// BroadcastTrade sends a trade event to all subscribed clients.
func (h *Hub) BroadcastTrade(pair string, trade *models.Trade) {
	event := NewTradeEvent(pair, trade)
	data, _ := json.Marshal(event)
	h.broadcast <- data
}

// BroadcastOrderUpdate sends an order update to all subscribed clients.
func (h *Hub) BroadcastOrderUpdate(pair string, orderID int64, status string) {
	event := NewOrderUpdateEvent(pair, orderID, status, "")
	data, _ := json.Marshal(event)
	h.broadcast <- data
}

// BroadcastOrderBookUpdate sends an order book update to all subscribed clients.
func (h *Hub) BroadcastOrderBookUpdate(pair string, bids, asks []PriceLevel) {
	event := NewOrderBookEvent(pair, bids, asks, 0)
	data, _ := json.Marshal(event)
	h.broadcast <- data
}

// BroadcastTradeToPair sends a trade event only to clients subscribed to a specific pair.
func (h *Hub) BroadcastTradeToPair(pair string, trade *models.Trade) {
	event := NewTradeEvent(pair, trade)
	data, _ := json.Marshal(event)
	h.BroadcastToPair(pair, data)
}

// BroadcastHeartbeat sends a heartbeat to all connected clients.
func (h *Hub) BroadcastHeartbeat(sequence int64) {
	event := NewHeartbeatEvent(sequence)
	data, _ := json.Marshal(event)
	h.broadcast <- data
}

// BroadcastError sends an error event to a specific client.
func (h *Hub) BroadcastError(clientID string, code, message string) {
	event := NewErrorEvent(code, message)
	data, _ := json.Marshal(event)
	h.mu.RLock()
	for _, clients := range h.clients {
		for client := range clients {
			if client.ID() == clientID {
				select {
				case client.send <- data:
				default:
				}
				break
			}
		}
	}
	h.mu.RUnlock()
}

// Register registers a client for a specific trading pair.
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister unregisters a client.
func (h *Hub) Unregister(client *Client) {
	h.subscriptionManager.UnsubscribeAll(client.ID())
	h.unregister <- client
}

// Broadcast sends a message to all clients.
func (h *Hub) Broadcast(data []byte) {
	h.broadcast <- data
}

// ClientCount returns the number of connected clients for a pair.
func (h *Hub) ClientCount(pair string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if clients, ok := h.clients[pair]; ok {
		return len(clients)
	}
	return 0
}

// TotalClientCount returns the total number of connected clients.
func (h *Hub) TotalClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, clients := range h.clients {
		total += len(clients)
	}
	return total
}

// Pairs returns a list of trading pairs with connected clients.
func (h *Hub) Pairs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	pairs := make([]string, 0, len(h.clients))
	for pair := range h.clients {
		pairs = append(pairs, pair)
	}
	return pairs
}

// GetSubscriptionManager returns the subscription manager.
func (h *Hub) GetSubscriptionManager() *SubscriptionManager {
	return h.subscriptionManager
}

// GetHeartbeatSequence returns the current heartbeat sequence number.
func (h *Hub) GetHeartbeatSequence() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.heartbeatSeq
}
