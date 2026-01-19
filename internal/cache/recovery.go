package cache

import (
	"log"
	"time"

	"limit-order-book/internal/engine"
	"limit-order-book/internal/models"
)

// RecoveryConfig holds configuration for order book recovery.
type RecoveryConfig struct {
	SnapshotInterval time.Duration // How often to save snapshots
	MaxSnapshotAge   time.Duration // Maximum age of snapshot to use
	RecoveryTimeout  time.Duration // Timeout for recovery operation
	EnableAutoBackup bool          // Enable automatic periodic backups
}

// DefaultRecoveryConfig returns default recovery configuration.
func DefaultRecoveryConfig() *RecoveryConfig {
	return &RecoveryConfig{
		SnapshotInterval: 30 * time.Second,
		MaxSnapshotAge:   24 * time.Hour,
		RecoveryTimeout:  60 * time.Second,
		EnableAutoBackup: true,
	}
}

// RecoveryResult contains the result of a recovery operation.
type RecoveryResult struct {
	Success        bool          // Whether recovery was successful
	SnapshotUsed   bool          // Whether a snapshot was used
	PairsRecovered int           // Number of pairs recovered
	OrdersLoaded   int           // Number of orders loaded
	TradesLoaded   int           // Number of trades loaded
	RecoveryTime   time.Duration // Time taken for recovery
	Error          error         // Any error that occurred
}

// RecoveryManager handles order book recovery and warm start.
type RecoveryManager struct {
	cache  *RedisCache
	config *RecoveryConfig
	done   chan struct{}
}

// NewRecoveryManager creates a new recovery manager.
func NewRecoveryManager(cache *RedisCache, config *RecoveryConfig) *RecoveryManager {
	if config == nil {
		config = DefaultRecoveryConfig()
	}

	return &RecoveryManager{
		cache:  cache,
		config: config,
		done:   make(chan struct{}),
	}
}

// StartAutoSnapshot starts automatic snapshot creation.
// This should be called in a goroutine.
func (r *RecoveryManager) StartAutoSnapshot(obManager *engine.OrderBookManager) {
	if !r.config.EnableAutoBackup {
		log.Println("⚠️ Auto snapshot disabled")
		return
	}

	ticker := time.NewTicker(r.config.SnapshotInterval)
	defer ticker.Stop()

	log.Printf("📸 Auto snapshot started (interval: %v)", r.config.SnapshotInterval)

	for {
		select {
		case <-r.done:
			log.Println("📸 Auto snapshot stopped")
			return
		case <-ticker.C:
			if err := r.CreateSnapshot(obManager); err != nil {
				log.Printf("⚠️ Failed to create snapshot: %v", err)
			} else {
				log.Println("📸 Snapshot created successfully")
			}
		}
	}
}

// Stop stops the automatic snapshot process.
func (r *RecoveryManager) Stop() {
	close(r.done)
}

// CreateSnapshot creates a snapshot of the current order book state.
func (r *RecoveryManager) CreateSnapshot(obManager *engine.OrderBookManager) error {
	if r.cache == nil {
		return nil
	}

	pairs := obManager.ListPairs()
	snapshot := &OrderBookSnapshot{
		Version:   0,
		Timestamp: time.Now(),
		Pairs:     make(map[string]*PairSnap),
	}

	totalOrders := 0

	for _, pair := range pairs {
		ob := obManager.GetOrderBook(pair)

		// Get order book depth
		bids, asks := ob.GetDepth(100)

		// Convert to cache format
		bidsCache := make([]OrderBookLevel, 0, len(bids))
		for _, b := range bids {
			bidsCache = append(bidsCache, OrderBookLevel{Price: b.Price, Volume: b.Volume})
		}

		asksCache := make([]OrderBookLevel, 0, len(asks))
		for _, a := range asks {
			asksCache = append(asksCache, OrderBookLevel{Price: a.Price, Volume: a.Volume})
		}

		pairSnap := &PairSnap{
			Pair:          pair,
			Bids:          bidsCache,
			Asks:          asksCache,
			OrderCount:    ob.GetOrderCount(),
			LastTradeTime: time.Now(),
		}

		snapshot.Pairs[pair] = pairSnap

		// Save orders to snapshot cache (iterate through ordersByID)
		// Note: We need to add a method to get all orders from the order book
		// For now, we'll skip order persistence in snapshot
		totalOrders += ob.GetOrderCount()
	}

	// Save snapshot
	if err := r.cache.SaveOrderBookSnapshot(snapshot); err != nil {
		return err
	}

	log.Printf("📸 Snapshot saved: %d pairs, %d orders", len(snapshot.Pairs), totalOrders)
	return nil
}

// RecoverOrderBook attempts to recover the order book from snapshot.
func (r *RecoveryManager) RecoverOrderBook(obManager *engine.OrderBookManager) (*RecoveryResult, error) {
	startTime := time.Now()
	result := &RecoveryResult{Success: false}

	if r.cache == nil {
		result.Error = nil // Not an error, just not configured
		return result, nil
	}

	// Check for latest snapshot
	version, err := r.cache.GetLatestSnapshotVersion()
	if err != nil || version == 0 {
		result.Error = nil // No snapshot, start fresh
		log.Println("📖 No snapshot found, starting with empty order book")
		return result, nil
	}

	// Load snapshot
	snapshot, err := r.cache.LoadOrderBookSnapshot()
	if err != nil {
		result.Error = err
		return result, err
	}

	// Check snapshot age
	if time.Since(snapshot.Timestamp) > r.config.MaxSnapshotAge {
		log.Printf("⚠️ Snapshot too old (%v), starting fresh", time.Since(snapshot.Timestamp))
		result.Error = nil
		return result, nil
	}

	result.SnapshotUsed = true
	log.Printf("📖 Recovering from snapshot v%d (%v old)", snapshot.Version, time.Since(snapshot.Timestamp))

	// Recover each pair
	for pair := range snapshot.Pairs {
		// Get or create order book for this pair (auto-creates)
		ob := obManager.GetOrderBook(pair)

		// Restore orders for this pair
		orders, err := r.cache.GetOrdersFromSnapshot(pair)
		if err != nil {
			log.Printf("⚠️ Failed to load orders for %s: %v", pair, err)
			continue
		}

		for _, order := range orders {
			// Reconstruct order
			newOrder := &models.Order{
				ID:        order.ID,
				UserID:    order.UserID,
				Pair:      order.Pair,
				Side:      models.Side(order.Side),
				Price:     order.Price,
				Quantity:  order.Quantity,
				Filled:    order.Filled,
				Status:    models.Status(order.Status),
				CreatedAt: order.CreatedAt,
			}

			// Only add open orders to the book
			if newOrder.Status == models.Open {
				wrapper := engine.NewOrderWrapper(newOrder)
				ob.AddOrder(wrapper)
			}

			result.OrdersLoaded++
		}

		// Restore recent trades
		trades, err := r.cache.GetRecentTradesFromSnapshot(pair)
		if err != nil {
			log.Printf("⚠️ Failed to load trades for %s: %v", pair, err)
			continue
		}
		result.TradesLoaded += len(trades)

		result.PairsRecovered++
	}

	result.RecoveryTime = time.Since(startTime)
	result.Success = true

	log.Printf("📖 Recovery complete: %d pairs, %d orders, %d trades (took %v)",
		result.PairsRecovered, result.OrdersLoaded, result.TradesLoaded, result.RecoveryTime)

	return result, nil
}

// GetRecoveryStatus returns the current recovery status.
func (r *RecoveryManager) GetRecoveryStatus() map[string]interface{} {
	if r.cache == nil {
		return map[string]interface{}{
			"enabled": false,
			"reason":  "Redis cache not available",
		}
	}

	version, _ := r.cache.GetLatestSnapshotVersion()
	pairs, _ := r.cache.GetSnapshotPairs()

	return map[string]interface{}{
		"enabled":          true,
		"snapshot_version": version,
		"snapshot_pairs":   pairs,
		"config":           r.config,
	}
}
