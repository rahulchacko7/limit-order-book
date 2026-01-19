package engine

import (
	"container/heap"
	"limit-order-book/internal/models"
	"log"
	"sync"
)

type OrderBook struct {
	buyHeap    *OrderHeap
	sellHeap   *OrderHeap
	mu         sync.Mutex
	ordersByID map[int64]*OrderWrapper
	onTrade    func(pair string, trade *TradeResult)
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		buyHeap:    NewBuyHeap(),
		sellHeap:   NewSellHeap(),
		ordersByID: make(map[int64]*OrderWrapper),
	}
}

func (ob *OrderBook) AddOrder(order *OrderWrapper) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	ob.ordersByID[order.ID] = order

	if order.Side == "buy" {
		ob.matchBuy(order)
		if order.Remaining() > 0 {
			heap.Push(ob.buyHeap, &OrderItem{order: order})
		} else {
			delete(ob.ordersByID, order.ID)
		}
	} else {
		ob.matchSell(order)
		if order.Remaining() > 0 {
			heap.Push(ob.sellHeap, &OrderItem{order: order})
		} else {
			delete(ob.ordersByID, order.ID)
		}
	}
}

// CancelReason represents the reason for order cancellation.
type CancelReason string

const (
	CancelReasonUserRequest       CancelReason = "user_request"
	CancelReasonInsufficientFunds CancelReason = "insufficient_funds"
	CancelReasonMarketOrder       CancelReason = "market_order"
	CancelReasonAdminCancel       CancelReason = "admin_cancel"
	CancelReasonExpired           CancelReason = "expired"
)

// CancelOrderResult contains the result of a cancel operation.
type CancelOrderResult struct {
	Cancelled    bool         `json:"cancelled"`
	OrderID      int64        `json:"order_id"`
	Reason       CancelReason `json:"reason,omitempty"`
	RemainingQty float64      `json:"remaining_qty"`
}

// CancelOrder cancels an order and returns the result.
func (ob *OrderBook) CancelOrder(orderID int64) *CancelOrderResult {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	order, exists := ob.ordersByID[orderID]
	if !exists {
		return &CancelOrderResult{
			Cancelled: false,
			OrderID:   orderID,
		}
	}

	// Prevent double-cancellation
	if order.Status == models.Cancelled || order.Status == models.Filled {
		return &CancelOrderResult{
			Cancelled: false,
			OrderID:   orderID,
			Reason:    CancelReasonUserRequest,
		}
	}

	return ob.cancelOrderInternal(order, CancelReasonUserRequest)
}

// CancelOrderWithReason cancels an order with a specific reason.
func (ob *OrderBook) CancelOrderWithReason(orderID int64, reason CancelReason) *CancelOrderResult {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	order, exists := ob.ordersByID[orderID]
	if !exists {
		return &CancelOrderResult{
			Cancelled: false,
			OrderID:   orderID,
		}
	}

	// Prevent double-cancellation
	if order.Status == models.Cancelled || order.Status == models.Filled {
		return &CancelOrderResult{
			Cancelled: false,
			OrderID:   orderID,
			Reason:    reason,
		}
	}

	return ob.cancelOrderInternal(order, reason)
}

// cancelOrderInternal performs the actual cancellation (caller must hold lock).
func (ob *OrderBook) cancelOrderInternal(order *OrderWrapper, reason CancelReason) *CancelOrderResult {
	remainingQty := order.Remaining()
	order.Status = models.Cancelled

	if order.Side == "buy" {
		ob.removeFromHeap(ob.buyHeap, order.ID)
	} else {
		ob.removeFromHeap(ob.sellHeap, order.ID)
	}

	delete(ob.ordersByID, order.ID)

	log.Printf("✅ Order cancelled: ID=%d, Pair=%s, Reason=%s, RemainingQty=%.4f",
		order.ID, order.Pair, reason, remainingQty)

	return &CancelOrderResult{
		Cancelled:    true,
		OrderID:      order.ID,
		Reason:       reason,
		RemainingQty: remainingQty,
	}
}

// CancelUserOrders cancels all orders for a specific user.
func (ob *OrderBook) CancelUserOrders(userID int64, reason CancelReason) []*CancelOrderResult {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	var results []*CancelOrderResult
	for _, order := range ob.ordersByID {
		if order.UserID == userID && order.Status == models.Open {
			results = append(results, ob.cancelOrderInternal(order, reason))
		}
	}
	return results
}

func (ob *OrderBook) removeFromHeap(h *OrderHeap, orderID int64) {
	for i, item := range h.items {
		if item.order.ID == orderID {
			h.items = append(h.items[:i], h.items[i+1:]...)
			heap.Init(h)
			return
		}
	}
}

func (ob *OrderBook) GetOrder(orderID int64) *OrderWrapper {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	return ob.ordersByID[orderID]
}

func (ob *OrderBook) GetBestBid() (float64, float64, bool) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.buyHeap.Len() == 0 {
		return 0, 0, false
	}

	order := ob.buyHeap.items[0].order
	return order.Price, order.Remaining(), true
}

func (ob *OrderBook) GetBestAsk() (float64, float64, bool) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.sellHeap.Len() == 0 {
		return 0, 0, false
	}

	order := ob.sellHeap.items[0].order
	return order.Price, order.Remaining(), true
}

func (ob *OrderBook) GetDepth(levels int) (bids, asks []OrderBookLevel) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	bids = make([]OrderBookLevel, 0, levels)
	for i := 0; i < intMin(levels, ob.buyHeap.Len()); i++ {
		order := ob.buyHeap.items[i].order
		bids = append(bids, OrderBookLevel{
			Price:  order.Price,
			Volume: order.Remaining(),
		})
	}

	asks = make([]OrderBookLevel, 0, levels)
	for i := 0; i < intMin(levels, ob.sellHeap.Len()); i++ {
		order := ob.sellHeap.items[i].order
		asks = append(asks, OrderBookLevel{
			Price:  order.Price,
			Volume: order.Remaining(),
		})
	}

	return bids, asks
}

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (ob *OrderBook) GetOrderCount() int {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	return len(ob.ordersByID)
}

func (ob *OrderBook) GetUserOrders(userID int64) []*models.Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	var orders []*models.Order
	for _, order := range ob.ordersByID {
		if order.UserID == userID {
			orders = append(orders, order.Order)
		}
	}
	return orders
}

func (ob *OrderBook) SetTradeCallback(cb func(pair string, trade *TradeResult)) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.onTrade = cb
}
