package engine

import (
	"limit-order-book/internal/models"
	"testing"
	"time"
)

// Helper to create a test order
func createTestOrder(id int64, side string, price, quantity float64) *OrderWrapper {
	return &OrderWrapper{
		Order: &models.Order{
			ID:        id,
			UserID:    id,
			Pair:      "BTC-USD",
			Side:      models.Side(side),
			Price:     price,
			Quantity:  quantity,
			Filled:    0,
			Status:    models.Open,
			CreatedAt: time.Now(),
		},
		timestamp: time.Now().UnixNano(),
	}
}

// TestOrderBook_AddOrder_SingleBuy tests adding a single buy order.
func TestOrderBook_AddOrder_SingleBuy(t *testing.T) {
	ob := NewOrderBook()
	order := createTestOrder(1, "buy", 50000, 1.0)

	ob.AddOrder(order)

	if ob.GetOrderCount() != 1 {
		t.Errorf("Expected 1 order in book, got %d", ob.GetOrderCount())
	}
}

// TestOrderBook_AddOrder_SingleSell tests adding a single sell order.
func TestOrderBook_AddOrder_SingleSell(t *testing.T) {
	ob := NewOrderBook()
	order := createTestOrder(2, "sell", 50000, 1.0)

	ob.AddOrder(order)

	if ob.GetOrderCount() != 1 {
		t.Errorf("Expected 1 order in book, got %d", ob.GetOrderCount())
	}
}

// TestOrderBook_MatchOrders tests matching a buy and sell order.
func TestOrderBook_MatchOrders(t *testing.T) {
	ob := NewOrderBook()

	// Add a sell order first (maker)
	sellOrder := createTestOrder(1, "sell", 50000, 1.0)
	ob.AddOrder(sellOrder)

	// Add a matching buy order (taker)
	buyOrder := createTestOrder(2, "buy", 50000, 1.0)
	ob.AddOrder(buyOrder)

	// Both orders should be filled
	if sellOrder.Status != models.Filled {
		t.Errorf("Expected sell order to be filled, got %s", sellOrder.Status)
	}
	if buyOrder.Status != models.Filled {
		t.Errorf("Expected buy order to be filled, got %s", buyOrder.Status)
	}

	// Order book should be empty
	if ob.GetOrderCount() != 0 {
		t.Errorf("Expected 0 orders in book, got %d", ob.GetOrderCount())
	}
}

// TestOrderBook_PartialFill tests partial order fill.
func TestOrderBook_PartialFill(t *testing.T) {
	ob := NewOrderBook()

	// Add 0.5 a sell order for BTC
	sellOrder := createTestOrder(1, "sell", 50000, 0.5)
	ob.AddOrder(sellOrder)

	// Add a buy order for 1.0 BTC (larger than available sell)
	buyOrder := createTestOrder(2, "buy", 50000, 1.0)
	ob.AddOrder(buyOrder)

	// Sell order should be fully filled
	if sellOrder.Status != models.Filled {
		t.Errorf("Expected sell order to be filled, got %s", sellOrder.Status)
	}
	if sellOrder.Filled != 0.5 {
		t.Errorf("Expected sell order filled to be 0.5, got %f", sellOrder.Filled)
	}

	// Buy order should be partially filled
	if buyOrder.Status != models.Partial {
		t.Errorf("Expected buy order to be partial, got %s", buyOrder.Status)
	}
	if buyOrder.Filled != 0.5 {
		t.Errorf("Expected buy order filled to be 0.5, got %f", buyOrder.Filled)
	}

	// One order (buy) should remain in book
	if ob.GetOrderCount() != 1 {
		t.Errorf("Expected 1 order in book, got %d", ob.GetOrderCount())
	}
}

// TestOrderBook_NoMatch tests when prices don't cross.
func TestOrderBook_NoMatch(t *testing.T) {
	ob := NewOrderBook()

	// Add a sell order at 51000
	sellOrder := createTestOrder(1, "sell", 51000, 1.0)
	ob.AddOrder(sellOrder)

	// Add a buy order at 50000 (below sell price - no match)
	buyOrder := createTestOrder(2, "buy", 50000, 1.0)
	ob.AddOrder(buyOrder)

	// Both orders should remain open
	if sellOrder.Status != models.Open {
		t.Errorf("Expected sell order to be open, got %s", sellOrder.Status)
	}
	if buyOrder.Status != models.Open {
		t.Errorf("Expected buy order to be open, got %s", buyOrder.Status)
	}

	// Both orders should be in book
	if ob.GetOrderCount() != 2 {
		t.Errorf("Expected 2 orders in book, got %d", ob.GetOrderCount())
	}
}

// TestOrderBook_CancelOrder tests order cancellation.
func TestOrderBook_CancelOrder(t *testing.T) {
	ob := NewOrderBook()

	// Add a buy order
	order := createTestOrder(1, "buy", 50000, 1.0)
	ob.AddOrder(order)

	// Cancel the order
	result := ob.CancelOrder(1)
	if result == nil || !result.Cancelled {
		t.Error("Expected cancel to return true")
	}

	// Order should be marked as cancelled
	if order.Status != models.Cancelled {
		t.Errorf("Expected order status to be cancelled, got %s", order.Status)
	}

	// Order should no longer be in book
	if ob.GetOrderCount() != 0 {
		t.Errorf("Expected 0 orders in book, got %d", ob.GetOrderCount())
	}
}

// TestOrderBook_GetBestBid tests getting best bid price.
func TestOrderBook_GetBestBid(t *testing.T) {
	ob := NewOrderBook()

	// Add multiple buy orders at different prices
	ob.AddOrder(createTestOrder(1, "buy", 50000, 1.0))
	ob.AddOrder(createTestOrder(2, "buy", 50100, 1.0))
	ob.AddOrder(createTestOrder(3, "buy", 49900, 1.0))

	// Best bid should be 50100 (highest price)
	bid, _, ok := ob.GetBestBid()
	if !ok {
		t.Error("Expected best bid to exist")
	}
	if bid != 50100 {
		t.Errorf("Expected best bid to be 50100, got %f", bid)
	}
}

// TestOrderBook_GetBestAsk tests getting best ask price.
func TestOrderBook_GetBestAsk(t *testing.T) {
	ob := NewOrderBook()

	// Add multiple sell orders at different prices
	ob.AddOrder(createTestOrder(1, "sell", 51000, 1.0))
	ob.AddOrder(createTestOrder(2, "sell", 50500, 1.0))
	ob.AddOrder(createTestOrder(3, "sell", 50800, 1.0))

	// Best ask should be 50500 (lowest price)
	ask, _, ok := ob.GetBestAsk()
	if !ok {
		t.Error("Expected best ask to exist")
	}
	if ask != 50500 {
		t.Errorf("Expected best ask to be 50500, got %f", ask)
	}
}

// TestOrderBook_ConcurrentAccess tests thread safety.
func TestOrderBook_ConcurrentAccess(t *testing.T) {
	ob := NewOrderBook()
	done := make(chan bool)

	// Add orders concurrently
	for i := 0; i < 100; i++ {
		go func(id int) {
			ob.AddOrder(createTestOrder(int64(id), "buy", 50000+float64(id), 1.0))
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// All orders should be in book
	count := ob.GetOrderCount()
	if count != 100 {
		t.Errorf("Expected 100 orders in book, got %d", count)
	}
}

// TestOrderBook_GetDepth tests order book depth.
func TestOrderBook_GetDepth(t *testing.T) {
	ob := NewOrderBook()

	// Add multiple orders
	for i := 0; i < 5; i++ {
		ob.AddOrder(createTestOrder(int64(i+1), "buy", 50000+float64(i), 1.0))
		ob.AddOrder(createTestOrder(int64(i+6), "sell", 51000+float64(i), 1.0))
	}

	bids, asks := ob.GetDepth(3)

	if len(bids) != 3 {
		t.Errorf("Expected 3 bid levels, got %d", len(bids))
	}
	if len(asks) != 3 {
		t.Errorf("Expected 3 ask levels, got %d", len(asks))
	}

	// Bids should be in descending order (highest first)
	if len(bids) > 1 && bids[0].Price < bids[1].Price {
		t.Error("Bids should be in descending order")
	}

	// Asks should be in ascending order (lowest first)
	if len(asks) > 1 && asks[0].Price > asks[1].Price {
		t.Error("Asks should be in ascending order")
	}
}
