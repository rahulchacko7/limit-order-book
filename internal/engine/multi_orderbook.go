package engine

import (
	"sync"
)

// OrderBookManager manages multiple order books for different trading pairs.
// THREAD SAFETY:
//   - Each order book has its own mutex, allowing concurrent operations on different pairs
//   - Global operations (like listing pairs) are protected by a global mutex
//
// SCALABILITY:
//   - For very high throughput, consider sharding by pair prefix
//   - Each pair's order book is independent
type OrderBookManager struct {
	// Map of trading pair -> order book
	books map[string]*OrderBook

	// Mutex for protecting the books map
	mu sync.RWMutex

	// Callbacks for publishing events (optional)
	onTrade func(pair string, trade *TradeResult)
	onOrder func(pair string, order *OrderWrapper)
}

// TradeResult contains trade execution details.
type TradeResult struct {
	BuyOrderID  int64
	SellOrderID int64
	Price       float64
	Quantity    float64
	BuyFilled   float64
	SellFilled  float64
}

// NewOrderBookManager creates a new order book manager.
func NewOrderBookManager() *OrderBookManager {
	return &OrderBookManager{
		books: make(map[string]*OrderBook),
	}
}

// GetOrderBook returns the order book for a trading pair, creating one if needed.
func (m *OrderBookManager) GetOrderBook(pair string) *OrderBook {
	m.mu.RLock()
	ob, exists := m.books[pair]
	m.mu.RUnlock()

	if exists {
		return ob
	}

	// Create new order book (double-check locking)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check again after acquiring write lock
	if ob, exists = m.books[pair]; exists {
		return ob
	}

	ob = NewOrderBook()
	m.books[pair] = ob
	return ob
}

// AddOrder adds an order to the specified trading pair's order book.
func (m *OrderBookManager) AddOrder(pair string, order *OrderWrapper) *OrderBookManager {
	ob := m.GetOrderBook(pair)
	ob.AddOrder(order)

	if m.onOrder != nil {
		m.onOrder(pair, order)
	}

	return m
}

// CancelOrder attempts to cancel an order from the specified trading pair.
// Returns the cancel result with details.
func (m *OrderBookManager) CancelOrder(pair string, orderID int64) *CancelOrderResult {
	m.mu.RLock()
	ob, exists := m.books[pair]
	m.mu.RUnlock()

	if !exists {
		return &CancelOrderResult{
			Cancelled: false,
			OrderID:   orderID,
		}
	}

	return ob.CancelOrder(orderID)
}

// CancelOrderWithReason attempts to cancel an order with a specific reason.
// Returns the cancel result with details.
func (m *OrderBookManager) CancelOrderWithReason(pair string, orderID int64, reason CancelReason) *CancelOrderResult {
	m.mu.RLock()
	ob, exists := m.books[pair]
	m.mu.RUnlock()

	if !exists {
		return &CancelOrderResult{
			Cancelled: false,
			OrderID:   orderID,
		}
	}

	return ob.CancelOrderWithReason(orderID, reason)
}

// GetOrder retrieves an order from the specified trading pair.
func (m *OrderBookManager) GetOrder(pair string, orderID int64) *OrderWrapper {
	m.mu.RLock()
	ob, exists := m.books[pair]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	return ob.GetOrder(orderID)
}

// GetBestBid returns the best bid price for a trading pair.
func (m *OrderBookManager) GetBestBid(pair string) (float64, bool) {
	m.mu.RLock()
	ob, exists := m.books[pair]
	m.mu.RUnlock()

	if !exists {
		return 0, false
	}

	price, _, ok := ob.GetBestBid()
	return price, ok
}

// GetBestAsk returns the best ask price for a trading pair.
func (m *OrderBookManager) GetBestAsk(pair string) (float64, bool) {
	m.mu.RLock()
	ob, exists := m.books[pair]
	m.mu.RUnlock()

	if !exists {
		return 0, false
	}

	price, _, ok := ob.GetBestAsk()
	return price, ok
}

// GetOrderBookDepth returns the order book depth for a trading pair.
func (m *OrderBookManager) GetOrderBookDepth(pair string, levels int) (bids, asks []OrderBookLevel) {
	m.mu.RLock()
	ob, exists := m.books[pair]
	m.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	return ob.GetDepth(levels)
}

// ListPairs returns all trading pairs with active order books.
func (m *OrderBookManager) ListPairs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pairs := make([]string, 0, len(m.books))
	for pair := range m.books {
		pairs = append(pairs, pair)
	}
	return pairs
}

// GetOrderBookCount returns the number of order books.
func (m *OrderBookManager) GetOrderBookCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.books)
}

// SetTradeCallback sets the callback for trade events.
func (m *OrderBookManager) SetTradeCallback(cb func(pair string, trade *TradeResult)) *OrderBookManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onTrade = cb
	return m
}

// SetOrderCallback sets the callback for order events.
func (m *OrderBookManager) SetOrderCallback(cb func(pair string, order *OrderWrapper)) *OrderBookManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onOrder = cb
	return m
}

// OrderBookLevel represents a price level in the order book.
type OrderBookLevel struct {
	Price  float64
	Volume float64
	Count  int
}
