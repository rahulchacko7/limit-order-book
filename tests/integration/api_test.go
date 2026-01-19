package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"limit-order-book/internal/api"
	"limit-order-book/internal/engine"
	"limit-order-book/internal/models"
	"limit-order-book/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRouter creates a test router with all components.
func setupTestRouter(t *testing.T) (*gin.Engine, *engine.OrderBookManager, *store.DedupStore) {
	gin.SetMode(gin.TestMode)

	obManager := engine.NewOrderBookManager()
	dedupStore := store.NewDedupStore(nil, nil) // nil DB for testing

	router := gin.New()

	// Register routes (without actual DB/Redis/MQ connections)
	api.RegisterRoutes(router, obManager, nil, nil, nil, nil, nil)

	return router, obManager, dedupStore
}

func TestPlaceOrder(t *testing.T) {
	router, obManager, _ := setupTestRouter(t)

	// Create test order
	orderReq := api.PlaceOrderRequest{
		UserID:   1,
		Pair:     "BTC-USD",
		Side:     "buy",
		Price:    50000.0,
		Quantity: 1.5,
	}

	body, _ := json.Marshal(orderReq)
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var order models.Order
	err := json.Unmarshal(w.Body.Bytes(), &order)
	require.NoError(t, err)

	assert.Equal(t, "BTC-USD", order.Pair)
	assert.Equal(t, "buy", string(order.Side))
	assert.Equal(t, 50000.0, order.Price)
	assert.Equal(t, 1.5, order.Quantity)
	assert.Equal(t, models.Open, order.Status)

	// Verify order is in the order book
	ob := obManager.GetOrderBook("BTC-USD")
	assert.NotNil(t, ob)
	assert.Equal(t, 1, ob.GetOrderCount())
}

func TestPlaceOrderValidation(t *testing.T) {
	router, _, _ := setupTestRouter(t)

	tests := []struct {
		name       string
		orderReq   api.PlaceOrderRequest
		wantStatus int
	}{
		{
			name: "missing_user_id",
			orderReq: api.PlaceOrderRequest{
				Pair:     "BTC-USD",
				Side:     "buy",
				Price:    50000.0,
				Quantity: 1.5,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid_price",
			orderReq: api.PlaceOrderRequest{
				UserID:   1,
				Pair:     "BTC-USD",
				Side:     "buy",
				Price:    0,
				Quantity: 1.5,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid_quantity",
			orderReq: api.PlaceOrderRequest{
				UserID:   1,
				Pair:     "BTC-USD",
				Side:     "buy",
				Price:    50000.0,
				Quantity: -1,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid_side",
			orderReq: api.PlaceOrderRequest{
				UserID:   1,
				Pair:     "BTC-USD",
				Side:     "hold",
				Price:    50000.0,
				Quantity: 1.5,
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.orderReq)
			req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestCancelOrder(t *testing.T) {
	router, obManager, _ := setupTestRouter(t)

	// First place an order
	orderReq := api.PlaceOrderRequest{
		UserID:   1,
		Pair:     "BTC-USD",
		Side:     "buy",
		Price:    50000.0,
		Quantity: 1.5,
	}

	body, _ := json.Marshal(orderReq)
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var order models.Order
	json.Unmarshal(w.Body.Bytes(), &order)

	// Now cancel the order
	req = httptest.NewRequest(http.MethodDelete, "/api/orders/"+string(rune(order.ID))+"?pair=BTC-USD", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify order is cancelled in order book
	ob := obManager.GetOrderBook("BTC-USD")
	assert.Equal(t, 0, ob.GetOrderCount())
}

func TestGetOrder(t *testing.T) {
	router, _, _ := setupTestRouter(t)

	// Place an order first
	orderReq := api.PlaceOrderRequest{
		UserID:   1,
		Pair:     "BTC-USD",
		Side:     "buy",
		Price:    50000.0,
		Quantity: 1.5,
	}

	body, _ := json.Marshal(orderReq)
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var order models.Order
	json.Unmarshal(w.Body.Bytes(), &order)

	// Get the order
	req = httptest.NewRequest(http.MethodGet, "/api/orders/"+string(rune(order.ID))+"?pair=BTC-USD", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetOrderBook(t *testing.T) {
	router, _, _ := setupTestRouter(t)

	// Create some orders
	for i := 0; i < 5; i++ {
		orderReq := api.PlaceOrderRequest{
			UserID:   int64(i + 1),
			Pair:     "BTC-USD",
			Side:     "buy",
			Price:    50000.0 + float64(i*100),
			Quantity: 1.0,
		}
		body, _ := json.Marshal(orderReq)
		req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	// Get order book
	req := httptest.NewRequest(http.MethodGet, "/api/pairs/BTC-USD/book", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	assert.Equal(t, "BTC-USD", response["pair"])
	assert.NotNil(t, response["bids"])
	assert.NotNil(t, response["asks"])
}

func TestGetTicker(t *testing.T) {
	router, _, _ := setupTestRouter(t)

	// Create a sell order to establish a market
	orderReq := api.PlaceOrderRequest{
		UserID:   1,
		Pair:     "BTC-USD",
		Side:     "sell",
		Price:    50000.0,
		Quantity: 1.0,
	}
	body, _ := json.Marshal(orderReq)
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Get ticker
	req = httptest.NewRequest(http.MethodGet, "/api/pairs/BTC-USD/ticker", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListPairs(t *testing.T) {
	router, _, _ := setupTestRouter(t)

	// Create orders for different pairs
	pairs := []string{"BTC-USD", "ETH-USD", "LTC-USD"}
	for i, pair := range pairs {
		orderReq := api.PlaceOrderRequest{
			UserID:   int64(i + 1),
			Pair:     pair,
			Side:     "buy",
			Price:    100.0,
			Quantity: 1.0,
		}
		body, _ := json.Marshal(orderReq)
		req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	// List pairs
	req := httptest.NewRequest(http.MethodGet, "/api/pairs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	pairsList := response["pairs"].([]interface{})
	assert.Equal(t, 3, len(pairsList))
}

// Benchmark for order placement
func BenchmarkPlaceOrder(b *testing.B) {
	router, _, _ := setupTestRouter(b)

	orderReq := api.PlaceOrderRequest{
		UserID:   1,
		Pair:     "BTC-USD",
		Side:     "buy",
		Price:    50000.0,
		Quantity: 1.5,
	}
	body, _ := json.Marshal(orderReq)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// Benchmark for order cancellation
func BenchmarkCancelOrder(b *testing.B) {
	router, obManager, _ := setupTestRouter(b)

	// Pre-create orders
	var orderIDs []int64
	for i := 0; i < 100; i++ {
		orderReq := api.PlaceOrderRequest{
			UserID:   int64(i),
			Pair:     "BTC-USD",
			Side:     "buy",
			Price:    50000.0,
			Quantity: 1.0,
		}
		body, _ := json.Marshal(orderReq)
		req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var order models.Order
		json.Unmarshal(w.Body.Bytes(), &order)
		orderIDs = append(orderIDs, order.ID)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		orderID := orderIDs[i%len(orderIDs)]
		req := httptest.NewRequest(http.MethodDelete, "/api/orders/"+string(rune(orderID))+"?pair=BTC-USD", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// DedupStore tests
func TestDedupStore_IsProcessed(t *testing.T) {
	// Test with nil DB (should return error)
	dedupStore := store.NewDedupStore(nil, nil)
	defer dedupStore.Stop()

	ctx := context.Background()

	// Without a real DB, we can't test actual functionality
	// But we can verify the methods are callable
	_, err := dedupStore.IsProcessed(ctx, "test-msg-id")
	assert.Error(t, err) // Expected since no DB connection
}

func TestDedupStore_TryProcess(t *testing.T) {
	dedupStore := store.NewDedupStore(nil, nil)
	defer dedupStore.Stop()

	ctx := context.Background()

	// Without a real DB, we can't test actual functionality
	processed, err := dedupStore.TryProcess(ctx, "test-msg-id", "test.event")
	assert.Error(t, err) // Expected since no DB connection
	assert.False(t, processed)
}

func TestDedupMiddleware_Process(t *testing.T) {
	dedupStore := store.NewDedupStore(nil, nil)
	defer dedupStore.Stop()

	middleware := store.NewDedupMiddleware(dedupStore)
	ctx := context.Background()

	handlerCalled := false
	err := middleware.Process(ctx, "test-msg-id", "test.event", func() error {
		handlerCalled = true
		return nil
	})

	// Without DB, should return error
	assert.Error(t, err)
	assert.False(t, handlerCalled)
}

// Test timeout handling
func TestOrderBookManager_ConcurrentOrders(t *testing.T) {
	obManager := engine.NewOrderBookManager()
	numOrders := 1000

	// Add orders concurrently
	done := make(chan bool, numOrders)
	for i := 0; i < numOrders; i++ {
		go func(idx int) {
			order := &models.Order{
				UserID:    int64(idx),
				Pair:      "BTC-USD",
				Side:      models.Buy,
				Price:     50000.0 + float64(idx%10),
				Quantity:  1.0,
				CreatedAt: time.Now(),
				Status:    models.Open,
			}
			wrapper := engine.NewOrderWrapper(order)
			obManager.AddOrder("BTC-USD", wrapper)
			done <- true
		}(i)
	}

	// Wait for all orders
	for i := 0; i < numOrders; i++ {
		<-done
	}

	ob := obManager.GetOrderBook("BTC-USD")
	assert.Equal(t, numOrders, ob.GetOrderCount())
}
