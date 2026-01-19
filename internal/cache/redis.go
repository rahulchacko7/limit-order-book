package cache

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"limit-order-book/internal/config"
	"limit-order-book/internal/models"
)

// RedisCache provides caching for order book state using Redis.
// CACHING STRATEGY:
//   - Best bid/ask: Cached with 100ms TTL for fast price lookups
//   - Order book depth: Cached with 500ms TTL
//   - Recent trades: Cached with 5s TTL for feed
//   - Order status: Cached with 1s TTL
//
// This reduces database load and provides fast read paths.
type RedisCache struct {
	client     *redis.Client
	ctx        context.Context
	defaultTTL time.Duration
}

// OrderBookState represents cached order book state.
type OrderBookState struct {
	Pair      string           `json:"pair"`
	BestBid   *OrderBookLevel  `json:"best_bid"`
	BestAsk   *OrderBookLevel  `json:"best_ask"`
	Bids      []OrderBookLevel `json:"bids"`
	Asks      []OrderBookLevel `json:"asks"`
	Timestamp time.Time        `json:"timestamp"`
}

// OrderBookLevel represents a price level in the order book.
type OrderBookLevel struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

// TradeFeed represents cached recent trades.
type TradeFeed struct {
	Trades    []models.Trade `json:"trades"`
	Timestamp time.Time      `json:"timestamp"`
}

// OrderBookSnapshot represents a complete order book snapshot for recovery.
// Used for warm start recovery and disaster recovery.
type OrderBookSnapshot struct {
	Version   int64                `json:"version"`
	Timestamp time.Time            `json:"timestamp"`
	Pairs     map[string]*PairSnap `json:"pairs"`
}

// PairSnap represents snapshot data for a single trading pair.
type PairSnap struct {
	Pair          string           `json:"pair"`
	Bids          []OrderBookLevel `json:"bids"`
	Asks          []OrderBookLevel `json:"asks"`
	OrderCount    int              `json:"order_count"`
	LastTradeTime time.Time        `json:"last_trade_time"`
}

// CachedOrder represents a cached order for snapshot recovery.
type CachedOrder struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Pair      string    `json:"pair"`
	Side      string    `json:"side"`
	Price     float64   `json:"price"`
	Quantity  float64   `json:"quantity"`
	Filled    float64   `json:"filled"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// NewRedisCache initializes a Redis connection.
func NewRedisCache(cfg *config.Config) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.GetRedisAddr(),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisCache{
		client:     client,
		ctx:        ctx,
		defaultTTL: time.Second,
	}, nil
}

// Close closes the Redis connection.
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// SetBestPrice caches the best bid and ask for a trading pair.
func (c *RedisCache) SetBestPrice(pair string, bid, ask *OrderBookLevel) error {
	key := "ob:best:" + pair

	state := map[string]interface{}{
		"bid_price":  bid.Price,
		"bid_volume": bid.Volume,
		"ask_price":  ask.Price,
		"ask_volume": ask.Volume,
	}

	pipe := c.client.Pipeline()
	pipe.HSet(c.ctx, key, state)
	pipe.Expire(c.ctx, key, 100*time.Millisecond)
	_, err := pipe.Exec(c.ctx)
	return err
}

// GetBestPrice retrieves cached best bid and ask.
func (c *RedisCache) GetBestPrice(pair string) (*OrderBookLevel, *OrderBookLevel, error) {
	key := "ob:best:" + pair
	result, err := c.client.HGetAll(c.ctx, key).Result()
	if err != nil {
		return nil, nil, err
	}
	if len(result) == 0 {
		return nil, nil, nil
	}

	bid := &OrderBookLevel{
		Price:  parseFloat(result["bid_price"]),
		Volume: parseFloat(result["bid_volume"]),
	}
	ask := &OrderBookLevel{
		Price:  parseFloat(result["ask_price"]),
		Volume: parseFloat(result["ask_volume"]),
	}

	return bid, ask, nil
}

// SetOrderBookDepth caches the full order book depth.
func (c *RedisCache) SetOrderBookDepth(pair string, bids, asks []OrderBookLevel) error {
	key := "ob:depth:" + pair
	state := OrderBookState{
		Pair:      pair,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return c.client.Set(c.ctx, key, data, 500*time.Millisecond).Err()
}

// GetOrderBookDepth retrieves cached order book depth.
func (c *RedisCache) GetOrderBookDepth(pair string) (*OrderBookState, error) {
	key := "ob:depth:" + pair
	data, err := c.client.Get(c.ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var state OrderBookState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// AddRecentTrade adds a trade to the recent trades feed.
func (c *RedisCache) AddRecentTrade(pair string, trade models.Trade) error {
	key := "trades:recent:" + pair

	data, err := json.Marshal(trade)
	if err != nil {
		return err
	}

	pipe := c.client.Pipeline()
	pipe.LPush(c.ctx, key, data)
	pipe.LTrim(c.ctx, key, 0, 99) // Keep last 100 trades
	pipe.Expire(c.ctx, key, 5*time.Second)
	_, err = pipe.Exec(c.ctx)
	return err
}

// GetRecentTrades retrieves recent trades for a pair.
func (c *RedisCache) GetRecentTrades(pair string, limit int64) ([]models.Trade, error) {
	key := "trades:recent:" + pair
	values, err := c.client.LRange(c.ctx, key, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}

	trades := make([]models.Trade, 0, len(values))
	for _, v := range values {
		var trade models.Trade
		if err := json.Unmarshal([]byte(v), &trade); err != nil {
			continue
		}
		trades = append(trades, trade)
	}

	return trades, nil
}

// SetOrderStatus caches an order's status.
func (c *RedisCache) SetOrderStatus(orderID int64, status string) error {
	key := "order:status:" + strconv.FormatInt(orderID, 10)
	return c.client.Set(c.ctx, key, status, time.Second).Err()
}

// GetOrderStatus retrieves cached order status.
func (c *RedisCache) GetOrderStatus(orderID int64) (string, error) {
	key := "order:status:" + strconv.FormatInt(orderID, 10)
	return c.client.Get(c.ctx, key).Result()
}

// IncrementCounter increments a counter (useful for metrics).
func (c *RedisCache) IncrementCounter(name string) error {
	return c.client.Incr(c.ctx, name).Err()
}

// GetCounter gets a counter value.
func (c *RedisCache) GetCounter(name string) (int64, error) {
	return c.client.Get(c.ctx, name).Int64()
}

// parseFloat safely parses a string to float64.
func parseFloat(s string) float64 {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return 0
}

// ==================== SNAPSHOT METHODS ====================
// These methods are used for warm start recovery and disaster recovery.

// SaveOrderBookSnapshot saves a complete order book snapshot.
// The snapshot is stored with a version number for versioning support.
func (c *RedisCache) SaveOrderBookSnapshot(snapshot *OrderBookSnapshot) error {
	// Increment version
	newVersion, err := c.IncrementSnapshotVersion(snapshot.Pairs)
	if err != nil {
		return err
	}
	snapshot.Version = newVersion
	snapshot.Timestamp = time.Now()

	// Serialize snapshot
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	// Save snapshot with key based on version
	key := "ob:snapshot:v" + strconv.FormatInt(newVersion, 10)
	return c.client.Set(c.ctx, key, data, 0).Err()
}

// LoadOrderBookSnapshot loads the latest order book snapshot.
// Returns nil if no snapshot exists.
func (c *RedisCache) LoadOrderBookSnapshot() (*OrderBookSnapshot, error) {
	// Get the latest version
	version, err := c.GetLatestSnapshotVersion()
	if err != nil || version == 0 {
		return nil, err
	}

	// Load snapshot
	key := "ob:snapshot:v" + strconv.FormatInt(version, 10)
	data, err := c.client.Get(c.ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var snapshot OrderBookSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}

	return &snapshot, nil
}

// IncrementSnapshotVersion increments and returns the new version number.
func (c *RedisCache) IncrementSnapshotVersion(pairs map[string]*PairSnap) (int64, error) {
	key := "ob:snapshot:version"
	version, err := c.client.Incr(c.ctx, key).Result()
	if err != nil {
		return 0, err
	}

	// Store list of pairs that have snapshots
	if pairs != nil && len(pairs) > 0 {
		pairsKey := "ob:snapshot:pairs"
		pairsList := make([]string, 0, len(pairs))
		for pair := range pairs {
			pairsList = append(pairsList, pair)
		}
		c.client.Del(c.ctx, pairsKey)
		if len(pairsList) > 0 {
			c.client.RPush(c.ctx, pairsKey, pairsList)
		}
	}

	return version, nil
}

// GetLatestSnapshotVersion returns the current snapshot version.
func (c *RedisCache) GetLatestSnapshotVersion() (int64, error) {
	key := "ob:snapshot:version"
	val, err := c.client.Get(c.ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

// GetSnapshotPairs returns the list of pairs that have snapshots.
func (c *RedisCache) GetSnapshotPairs() ([]string, error) {
	key := "ob:snapshot:pairs"
	return c.client.LRange(c.ctx, key, 0, -1).Result()
}

// SaveOrderToSnapshot saves an individual order to the snapshot cache.
// This is used during snapshot creation.
func (c *RedisCache) SaveOrderToSnapshot(order *CachedOrder) error {
	key := "ob:orders:" + order.Pair
	data, err := json.Marshal(order)
	if err != nil {
		return err
	}
	return c.client.RPush(c.ctx, key, data).Err()
}

// GetOrdersFromSnapshot retrieves all orders for a pair from snapshot cache.
func (c *RedisCache) GetOrdersFromSnapshot(pair string) ([]*CachedOrder, error) {
	key := "ob:orders:" + pair
	values, err := c.client.LRange(c.ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	orders := make([]*CachedOrder, 0, len(values))
	for _, v := range values {
		var order CachedOrder
		if err := json.Unmarshal([]byte(v), &order); err != nil {
			continue
		}
		orders = append(orders, &order)
	}

	return orders, nil
}

// ClearSnapshot removes all snapshot data.
func (c *RedisCache) ClearSnapshot() error {
	// Get current version
	version, err := c.GetLatestSnapshotVersion()
	if err != nil {
		return err
	}

	// Delete current snapshot
	if version > 0 {
		key := "ob:snapshot:v" + strconv.FormatInt(version, 10)
		c.client.Del(c.ctx, key)
	}

	// Reset version
	c.client.Set(c.ctx, "ob:snapshot:version", 0, 0)

	// Clear order snapshots
	pairs, _ := c.GetSnapshotPairs()
	for _, pair := range pairs {
		c.client.Del(c.ctx, "ob:orders:"+pair)
	}
	c.client.Del(c.ctx, "ob:snapshot:pairs")

	return nil
}

// SaveRecentTrades saves recent trades for snapshot recovery.
func (c *RedisCache) SaveRecentTrades(pair string, trades []models.Trade) error {
	key := "ob:trades:" + pair

	pipe := c.client.Pipeline()
	for _, trade := range trades {
		data, err := json.Marshal(trade)
		if err != nil {
			continue
		}
		pipe.RPush(c.ctx, key, data)
	}
	pipe.LTrim(c.ctx, key, 0, 99)         // Keep last 100 trades
	pipe.Expire(c.ctx, key, 24*time.Hour) // Keep for 24 hours
	_, err := pipe.Exec(c.ctx)
	return err
}

// GetRecentTradesFromSnapshot retrieves recent trades from snapshot.
func (c *RedisCache) GetRecentTradesFromSnapshot(pair string) ([]models.Trade, error) {
	key := "ob:trades:" + pair
	values, err := c.client.LRange(c.ctx, key, 0, 99).Result()
	if err != nil {
		return nil, err
	}

	trades := make([]models.Trade, 0, len(values))
	for _, v := range values {
		var trade models.Trade
		if err := json.Unmarshal([]byte(v), &trade); err != nil {
			continue
		}
		trades = append(trades, trade)
	}

	return trades, nil
}
