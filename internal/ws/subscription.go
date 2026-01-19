package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"limit-order-book/internal/models"
)

// SubscriptionManager handles WebSocket client subscriptions to trading pairs.
// SUBSCRIPTION FEATURES:
//   - Clients can subscribe/unsubscribe to specific trading pairs
//   - Each client can subscribe to multiple pairs
//   - Messages are only sent to clients subscribed to the relevant pair
//   - Subscription acknowledgments are sent to clients
type SubscriptionManager struct {
	// Map of client ID -> set of subscribed pairs
	clientSubscriptions map[string]map[string]bool

	// Map of pair -> set of subscribed client IDs
	pairSubscriptions map[string]map[string]bool

	// Mutex for thread-safe operations
	mu sync.RWMutex
}

// NewSubscriptionManager creates a new subscription manager.
func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		clientSubscriptions: make(map[string]map[string]bool),
		pairSubscriptions:   make(map[string]map[string]bool),
	}
}

// Subscribe adds a subscription for a client to a trading pair.
func (s *SubscriptionManager) Subscribe(clientID, pair string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Add to client subscriptions
	if s.clientSubscriptions[clientID] == nil {
		s.clientSubscriptions[clientID] = make(map[string]bool)
	}
	s.clientSubscriptions[clientID][pair] = true

	// Add to pair subscriptions
	if s.pairSubscriptions[pair] == nil {
		s.pairSubscriptions[pair] = make(map[string]bool)
	}
	s.pairSubscriptions[pair][clientID] = true

	log.Printf("📱 Client %s subscribed to %s", clientID, pair)
}

// Unsubscribe removes a subscription for a client from a trading pair.
func (s *SubscriptionManager) Unsubscribe(clientID, pair string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove from client subscriptions
	if pairs, ok := s.clientSubscriptions[clientID]; ok {
		delete(pairs, pair)
		if len(pairs) == 0 {
			delete(s.clientSubscriptions, clientID)
		}
	}

	// Remove from pair subscriptions
	if clients, ok := s.pairSubscriptions[pair]; ok {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(s.pairSubscriptions, pair)
		}
	}

	log.Printf("📱 Client %s unsubscribed from %s", clientID, pair)
}

// UnsubscribeAll removes all subscriptions for a client.
func (s *SubscriptionManager) UnsubscribeAll(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pairs, ok := s.clientSubscriptions[clientID]
	if !ok {
		return
	}

	// Remove from pair subscriptions
	for pair := range pairs {
		if clients, ok := s.pairSubscriptions[pair]; ok {
			delete(clients, clientID)
			if len(clients) == 0 {
				delete(s.pairSubscriptions, pair)
			}
		}
	}

	// Remove client subscriptions
	delete(s.clientSubscriptions, clientID)

	log.Printf("📱 Client %s unsubscribed from all pairs", clientID)
}

// GetSubscribedPairs returns all pairs a client is subscribed to.
func (s *SubscriptionManager) GetSubscribedPairs(clientID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pairs, ok := s.clientSubscriptions[clientID]
	if !ok {
		return nil
	}

	result := make([]string, 0, len(pairs))
	for pair := range pairs {
		result = append(result, pair)
	}
	return result
}

// GetSubscribedClients returns all clients subscribed to a pair.
func (s *SubscriptionManager) GetSubscribedClients(pair string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clients, ok := s.pairSubscriptions[pair]
	if !ok {
		return nil
	}

	result := make([]string, 0, len(clients))
	for clientID := range clients {
		result = append(result, clientID)
	}
	return result
}

// IsSubscribed checks if a client is subscribed to a pair.
func (s *SubscriptionManager) IsSubscribed(clientID, pair string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pairs, ok := s.clientSubscriptions[clientID]
	if !ok {
		return false
	}
	return pairs[pair]
}

// GetClientCount returns the number of clients subscribed to a pair.
func (s *SubscriptionManager) GetClientCount(pair string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clients, ok := s.pairSubscriptions[pair]
	if !ok {
		return 0
	}
	return len(clients)
}

// GetTotalClientCount returns the total number of subscribed clients.
func (s *SubscriptionManager) GetTotalClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clientSubscriptions)
}

// SubscriptionMessage represents a subscription request from a client.
type SubscriptionMessage struct {
	Action string   `json:"action"` // "subscribe" or "unsubscribe"
	Pairs  []string `json:"pairs"`  // List of pairs to subscribe/unsubscribe
}

// SubscriptionAckMessage represents an acknowledgment for a subscription request.
type SubscriptionAckMessage struct {
	Type      string    `json:"type"`
	Action    string    `json:"action"`
	Success   bool      `json:"success"`
	Pairs     []string  `json:"pairs"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// NewSubscriptionAck creates a new subscription acknowledgment.
func NewSubscriptionAck(action string, success bool, pairs []string, message string) *SubscriptionAckMessage {
	return &SubscriptionAckMessage{
		Type:      "subscription_ack",
		Action:    action,
		Success:   success,
		Pairs:     pairs,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// EventType represents the type of WebSocket event.
type EventType string

const (
	EventTypeTrade        EventType = "trade"
	EventTypeOrderBook    EventType = "orderbook"
	EventTypeOrderUpdate  EventType = "order_update"
	EventTypeSubscription EventType = "subscription"
	EventTypeError        EventType = "error"
	EventTypeHeartbeat    EventType = "heartbeat"
	EventTypeSnapshot     EventType = "snapshot"
)

// TypedEvent is a generic event wrapper with type information.
type TypedEvent struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Pair      string      `json:"pair,omitempty"`
	Data      interface{} `json:"data"`
}

// NewTypedEvent creates a new typed event.
func NewTypedEvent(eventType EventType, pair string, data interface{}) *TypedEvent {
	return &TypedEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Pair:      pair,
		Data:      data,
	}
}

// TradeEvent represents a trade execution event.
type TradeEvent struct {
	Type      string        `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	Pair      string        `json:"pair"`
	Trade     *models.Trade `json:"trade"`
}

// NewTradeEvent creates a new trade event.
func NewTradeEvent(pair string, trade *models.Trade) *TradeEvent {
	return &TradeEvent{
		Type:      "trade",
		Timestamp: time.Now(),
		Pair:      pair,
		Trade:     trade,
	}
}

// OrderBookEvent represents an order book update event.
type OrderBookEvent struct {
	Type      string       `json:"type"`
	Timestamp time.Time    `json:"timestamp"`
	Pair      string       `json:"pair"`
	Bids      []PriceLevel `json:"bids"`
	Asks      []PriceLevel `json:"asks"`
	Sequence  int64        `json:"sequence"`
}

// NewOrderBookEvent creates a new order book event.
func NewOrderBookEvent(pair string, bids, asks []PriceLevel, sequence int64) *OrderBookEvent {
	return &OrderBookEvent{
		Type:      "orderbook",
		Timestamp: time.Now(),
		Pair:      pair,
		Bids:      bids,
		Asks:      asks,
		Sequence:  sequence,
	}
}

// OrderUpdateEvent represents an order status update event.
type OrderUpdateEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Pair      string    `json:"pair"`
	OrderID   int64     `json:"order_id"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
}

// NewOrderUpdateEvent creates a new order update event.
func NewOrderUpdateEvent(pair string, orderID int64, status, reason string) *OrderUpdateEvent {
	return &OrderUpdateEvent{
		Type:      "order_update",
		Timestamp: time.Now(),
		Pair:      pair,
		OrderID:   orderID,
		Status:    status,
		Reason:    reason,
	}
}

// SnapshotEvent represents an initial order book snapshot.
type SnapshotEvent struct {
	Type      string       `json:"type"`
	Timestamp time.Time    `json:"timestamp"`
	Pair      string       `json:"pair"`
	Bids      []PriceLevel `json:"bids"`
	Asks      []PriceLevel `json:"asks"`
	Trades    []TradeInfo  `json:"trades"`
	Sequence  int64        `json:"sequence"`
}

// TradeInfo represents trade information for snapshots.
type TradeInfo struct {
	ID        int64     `json:"id"`
	Price     float64   `json:"price"`
	Quantity  float64   `json:"quantity"`
	Side      string    `json:"side"`
	CreatedAt time.Time `json:"created_at"`
}

// NewSnapshotEvent creates a new snapshot event.
func NewSnapshotEvent(pair string, bids, asks []PriceLevel, trades []TradeInfo, sequence int64) *SnapshotEvent {
	return &SnapshotEvent{
		Type:      "snapshot",
		Timestamp: time.Now(),
		Pair:      pair,
		Bids:      bids,
		Asks:      asks,
		Trades:    trades,
		Sequence:  sequence,
	}
}

// HeartbeatEvent is a periodic heartbeat message.
type HeartbeatEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Sequence  int64     `json:"sequence"`
}

// NewHeartbeatEvent creates a new heartbeat event.
func NewHeartbeatEvent(sequence int64) *HeartbeatEvent {
	return &HeartbeatEvent{
		Type:      "heartbeat",
		Timestamp: time.Now(),
		Sequence:  sequence,
	}
}

// ErrorEvent represents an error event.
type ErrorEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
}

// NewErrorEvent creates a new error event.
func NewErrorEvent(code, message string) *ErrorEvent {
	return &ErrorEvent{
		Type:      "error",
		Timestamp: time.Now(),
		Code:      code,
		Message:   message,
	}
}

// ToJSON converts an event to JSON bytes.
func ToJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("⚠️ Failed to marshal event: %v", err)
		return nil
	}
	return data
}
