package engine

import (
	"sync"
	"testing"
	"time"

	"limit-order-book/internal/models"
)

// BenchmarkOrderBook_AddOrder benchmarks order insertion performance.
func BenchmarkOrderBook_AddOrder(b *testing.B) {
	ob := NewOrderBook()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		order := createBenchmarkOrder(int64(i), "buy", 50000+float64(i%100), 1.0)
		ob.AddOrder(order)
	}
}

// BenchmarkOrderBook_MatchOrders benchmarks order matching performance.
func BenchmarkOrderBook_MatchOrders(b *testing.B) {
	ob := NewOrderBook()

	// Pre-populate with some orders
	for i := 0; i < 1000; i++ {
		ob.AddOrder(createBenchmarkOrder(int64(i), "sell", 50000+float64(i%100), 0.1))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		order := createBenchmarkOrder(int64(b.N+i), "buy", 50050, 1.0)
		ob.AddOrder(order)
	}
}

// BenchmarkOrderBook_ConcurrentAdd benchmarks concurrent order insertion.
func BenchmarkOrderBook_ConcurrentAdd(b *testing.B) {
	ob := NewOrderBook()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := int64(0)
		for pb.Next() {
			order := createBenchmarkOrder(i, "buy", 50000+float64(int64(i)%100), 1.0)
			ob.AddOrder(order)
			i++
		}
	})
}

// BenchmarkOrderBook_CancelOrder benchmarks order cancellation.
func BenchmarkOrderBook_CancelOrder(b *testing.B) {
	ob := NewOrderBook()

	// Pre-populate with orders
	orders := make([]int64, b.N)
	for i := 0; int64(i) < int64(b.N); i++ {
		id := int64(i)
		orders[i] = id
		ob.AddOrder(createBenchmarkOrder(id, "buy", 50000+float64(i%100), 1.0))
	}

	b.ResetTimer()
	for i := 0; int64(i) < int64(b.N); i++ {
		ob.CancelOrder(orders[i])
	}
}

// BenchmarkOrderBook_GetBestPrice benchmarks best price lookup.
func BenchmarkOrderBook_GetBestPrice(b *testing.B) {
	ob := NewOrderBook()

	// Pre-populate with orders
	for i := 0; i < 10000; i++ {
		ob.AddOrder(createBenchmarkOrder(int64(i), "buy", 50000+float64(i%100), 1.0))
		ob.AddOrder(createBenchmarkOrder(int64(i+10000), "sell", 51000+float64(i%100), 1.0))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ob.GetBestBid()
		ob.GetBestAsk()
	}
}

// BenchmarkOrderBook_GetDepth benchmarks order book depth retrieval.
func BenchmarkOrderBook_GetDepth(b *testing.B) {
	ob := NewOrderBook()

	// Pre-populate with orders
	for i := 0; i < 1000; i++ {
		ob.AddOrder(createBenchmarkOrder(int64(i), "buy", 50000+float64(i%100), 1.0))
		ob.AddOrder(createBenchmarkOrder(int64(i+1000), "sell", 51000+float64(i%100), 1.0))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ob.GetDepth(10)
	}
}

// BenchmarkOrderBookManager_ConcurrentPairs benchmarks multi-pair order book access.
func BenchmarkOrderBookManager_ConcurrentPairs(b *testing.B) {
	manager := NewOrderBookManager()
	var wg sync.WaitGroup

	b.ResetTimer()
	for i := 0; i < 10; i++ { // 10 pairs
		wg.Add(1)
		go func(pairIdx int) {
			defer wg.Done()
			for j := 0; j < b.N/10; j++ {
				pairs := []string{"BTC-USD", "ETH-USD", "SOL-USD", "ADA-USD", "DOT-USD",
					"LTC-USD", "XRP-USD", "LINK-USD", "AVAX-USD", "MATIC-USD"}
				order := createBenchmarkOrder(int64(j), "buy", 50000+float64(j%100), 1.0)
				order.Order.Pair = pairs[pairIdx]
				manager.AddOrder(pairs[pairIdx], order)
			}
		}(i)
	}
	wg.Wait()
}

// BenchmarkOrderBook_CancelNonExistent benchmarks cancelling non-existent orders.
func BenchmarkOrderBook_CancelNonExistent(b *testing.B) {
	ob := NewOrderBook()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ob.CancelOrder(int64(i + 1000000))
	}
}

// createBenchmarkOrder creates an order for benchmarking.
func createBenchmarkOrder(id int64, side string, price, quantity float64) *OrderWrapper {
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
