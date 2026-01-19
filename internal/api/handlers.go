package api

import (
	"net/http"
	"strconv"
	"time"

	"limit-order-book/internal/cache"
	"limit-order-book/internal/engine"
	"limit-order-book/internal/models"
	"limit-order-book/internal/ws"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	ob    *engine.OrderBookManager
	cache *cache.RedisCache
	wsHub *ws.Hub
}

func NewHandler(ob *engine.OrderBookManager, cache *cache.RedisCache, wsHub *ws.Hub) *Handler {
	return &Handler{
		ob:    ob,
		cache: cache,
		wsHub: wsHub,
	}
}

func (h *Handler) PlaceOrder(c *gin.Context) {
	var req PlaceOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.validateOrderRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	order := &models.Order{
		UserID:    req.UserID,
		Pair:      req.Pair,
		Side:      models.Side(req.Side),
		Price:     req.Price,
		Quantity:  req.Quantity,
		CreatedAt: time.Now(),
		Status:    models.Open,
	}

	wrapper := engine.NewOrderWrapper(order)
	h.ob.AddOrder(req.Pair, wrapper)

	if h.cache != nil {
		h.updateOrderBookCache(req.Pair)
	}

	if h.wsHub != nil {
		h.wsHub.BroadcastOrderUpdate(req.Pair, order.ID, string(order.Status))
	}

	c.JSON(http.StatusOK, order)
}

func (h *Handler) CancelOrder(c *gin.Context) {
	orderIDStr := c.Param("id")
	pair := c.Query("pair")

	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair query parameter is required"})
		return
	}

	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}

	order := h.ob.GetOrder(pair, orderID)
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	result := h.ob.CancelOrder(pair, orderID)
	if result == nil || !result.Cancelled {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel order"})
		return
	}

	// Update cache with cancelled status
	if h.cache != nil {
		h.cache.SetOrderStatus(orderID, string(models.Cancelled))
	}

	// Broadcast order update via WebSocket
	if h.wsHub != nil {
		h.wsHub.BroadcastOrderUpdate(pair, orderID, string(models.Cancelled))
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "order cancelled",
		"order_id":      orderID,
		"pair":          pair,
		"reason":        result.Reason,
		"remaining_qty": result.RemainingQty,
	})
}

func (h *Handler) GetOrder(c *gin.Context) {
	orderIDStr := c.Param("id")
	pair := c.Query("pair")

	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair query parameter is required"})
		return
	}

	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}

	order := h.ob.GetOrder(pair, orderID)
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	c.JSON(http.StatusOK, order.Order)
}

func (h *Handler) GetOrderBook(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair is required"})
		return
	}

	levels := 10
	if levelsStr := c.Query("levels"); levelsStr != "" {
		if l, err := strconv.Atoi(levelsStr); err == nil && l > 0 && l <= 100 {
			levels = l
		}
	}

	bids, asks := h.ob.GetOrderBookDepth(pair, levels)

	c.JSON(http.StatusOK, gin.H{
		"pair": pair,
		"bids": bids,
		"asks": asks,
	})
}

func (h *Handler) GetUserOrders(c *gin.Context) {
	userIDStr := c.Query("user_id")
	if userIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	pairs := h.ob.ListPairs()
	var orders []*models.Order
	for _, pair := range pairs {
		ob := h.ob.GetOrderBook(pair)
		orders = append(orders, ob.GetUserOrders(userID)...)
	}

	c.JSON(http.StatusOK, gin.H{
		"orders": orders,
		"count":  len(orders),
	})
}

func (h *Handler) GetTicker(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair is required"})
		return
	}

	bid, bidOk := h.ob.GetBestBid(pair)
	ask, askOk := h.ob.GetBestAsk(pair)

	c.JSON(http.StatusOK, gin.H{
		"pair":   pair,
		"bid":    bid,
		"bid_ok": bidOk,
		"ask":    ask,
		"ask_ok": askOk,
		"spread": func() float64 {
			if bidOk && askOk {
				return ask - bid
			}
			return 0
		}(),
	})
}

func (h *Handler) ListPairs(c *gin.Context) {
	pairs := h.ob.ListPairs()
	c.JSON(http.StatusOK, gin.H{
		"pairs": pairs,
		"count": len(pairs),
	})
}

type PlaceOrderRequest struct {
	UserID   int64   `json:"user_id" binding:"required"`
	Pair     string  `json:"pair" binding:"required"`
	Side     string  `json:"side" binding:"required,oneof=buy sell"`
	Price    float64 `json:"price" binding:"required,gt=0"`
	Quantity float64 `json:"quantity" binding:"required,gt=0"`
}

func (h *Handler) validateOrderRequest(req *PlaceOrderRequest) error {
	if req.Price <= 0 {
		return ErrInvalidPrice
	}
	if req.Quantity <= 0 {
		return ErrInvalidQuantity
	}
	if req.Side != "buy" && req.Side != "sell" {
		return ErrInvalidSide
	}
	return nil
}

func (h *Handler) updateOrderBookCache(pair string) {
	if h.cache == nil {
		return
	}

	bidPrice, bidVol, bidOk := h.ob.GetOrderBook(pair).GetBestBid()
	askPrice, askVol, askOk := h.ob.GetOrderBook(pair).GetBestAsk()

	if bidOk && askOk {
		h.cache.SetBestPrice(pair,
			&cache.OrderBookLevel{Price: bidPrice, Volume: bidVol},
			&cache.OrderBookLevel{Price: askPrice, Volume: askVol},
		)
	}
}

var (
	ErrInvalidPrice    = &ValidationError{Message: "price must be greater than 0"}
	ErrInvalidQuantity = &ValidationError{Message: "quantity must be greater than 0"}
	ErrInvalidSide     = &ValidationError{Message: "side must be 'buy' or 'sell'"}
)

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}
