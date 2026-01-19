package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all application metrics.
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal    *prometheus.CounterVec
	HTTPRequestDuration  *prometheus.HistogramVec
	HTTPRequestsInFlight prometheus.Gauge

	// Order book metrics
	OrdersPlaced    prometheus.Counter
	OrdersCancelled prometheus.Counter
	OrdersFilled    prometheus.Counter
	OrderBookSize   *prometheus.GaugeVec
	OrderBookDepth  *prometheus.GaugeVec

	// Trade metrics
	TradesTotal prometheus.Counter
	TradeVolume *prometheus.CounterVec
	TradeValue  *prometheus.CounterVec

	// WebSocket metrics
	WSConnections      prometheus.Gauge
	WSMessagesSent     *prometheus.CounterVec
	WSMessagesReceived *prometheus.CounterVec

	// RabbitMQ metrics
	MQMessagesPublished *prometheus.CounterVec
	MQMessagesConsumed  *prometheus.CounterVec
	MQQueueDepth        *prometheus.GaugeVec

	// Cache metrics
	CacheHits    prometheus.Counter
	CacheMisses  prometheus.Counter
	CacheLatency *prometheus.HistogramVec

	// System metrics
	GoRoutines  prometheus.Gauge
	MemoryUsage *prometheus.GaugeVec
}

// NewMetrics creates and registers all application metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		// HTTP metrics
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		HTTPRequestsInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "http_requests_in_flight",
				Help: "Current number of HTTP requests being processed",
			},
		),

		// Order book metrics
		OrdersPlaced: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "orders_placed_total",
				Help: "Total number of orders placed",
			},
		),
		OrdersCancelled: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "orders_cancelled_total",
				Help: "Total number of orders cancelled",
			},
		),
		OrdersFilled: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "orders_filled_total",
				Help: "Total number of orders filled",
			},
		),
		OrderBookSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "orderbook_size",
				Help: "Number of orders in the order book",
			},
			[]string{"pair", "side"},
		),
		OrderBookDepth: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "orderbook_depth",
				Help: "Total volume at each price level",
			},
			[]string{"pair", "side"},
		),

		// Trade metrics
		TradesTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "trades_total",
				Help: "Total number of trades executed",
			},
		),
		TradeVolume: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "trade_volume_total",
				Help: "Total trading volume by pair",
			},
			[]string{"pair"},
		),
		TradeValue: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "trade_value_total",
				Help: "Total trade value by pair",
			},
			[]string{"pair"},
		),

		// WebSocket metrics
		WSConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "ws_connections_active",
				Help: "Current number of active WebSocket connections",
			},
		),
		WSMessagesSent: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ws_messages_sent_total",
				Help: "Total number of WebSocket messages sent",
			},
			[]string{"pair", "type"},
		),
		WSMessagesReceived: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ws_messages_received_total",
				Help: "Total number of WebSocket messages received",
			},
			[]string{"type"},
		),

		// RabbitMQ metrics
		MQMessagesPublished: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mq_messages_published_total",
				Help: "Total number of messages published to RabbitMQ",
			},
			[]string{"exchange", "routing_key"},
		),
		MQMessagesConsumed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mq_messages_consumed_total",
				Help: "Total number of messages consumed from RabbitMQ",
			},
			[]string{"queue"},
		),
		MQQueueDepth: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mq_queue_depth",
				Help: "Current queue depth in RabbitMQ",
			},
			[]string{"queue"},
		),

		// Cache metrics
		CacheHits: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "cache_hits_total",
				Help: "Total number of cache hits",
			},
		),
		CacheMisses: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "cache_misses_total",
				Help: "Total number of cache misses",
			},
		),
		CacheLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "cache_operation_duration_seconds",
				Help:    "Cache operation latency in seconds",
				Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
			},
			[]string{"operation"},
		),

		// System metrics
		GoRoutines: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "go_goroutines",
				Help: "Current number of Go goroutines",
			},
		),
		MemoryUsage: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "memory_usage_bytes",
				Help: "Memory usage in bytes",
			},
			[]string{"type"},
		),
	}
}

// RecordOrderPlaced records a new order placement.
func (m *Metrics) RecordOrderPlaced(pair string) {
	m.OrdersPlaced.Inc()
	m.OrderBookSize.WithLabelValues(pair, "total").Inc()
}

// RecordOrderCancelled records an order cancellation.
func (m *Metrics) RecordOrderCancelled(pair string) {
	m.OrdersCancelled.Inc()
	m.OrderBookSize.WithLabelValues(pair, "total").Dec()
}

// RecordTrade records a trade execution.
func (m *Metrics) RecordTrade(pair string, volume, value float64) {
	m.TradesTotal.Inc()
	m.TradeVolume.WithLabelValues(pair).Add(volume)
	m.TradeValue.WithLabelValues(pair).Add(value)
}

// RecordWSSent records a WebSocket message sent.
func (m *Metrics) RecordWSSent(pair, msgType string) {
	m.WSMessagesSent.WithLabelValues(pair, msgType).Inc()
}

// RecordWSReceived records a WebSocket message received.
func (m *Metrics) RecordWSReceived(msgType string) {
	m.WSMessagesReceived.WithLabelValues(msgType).Inc()
}

// RecordCacheHit records a cache hit.
func (m *Metrics) RecordCacheHit() {
	m.CacheHits.Inc()
}

// RecordCacheMiss records a cache miss.
func (m *Metrics) RecordCacheMiss() {
	m.CacheMisses.Inc()
}

// RecordHTTRequest records an HTTP request.
func (m *Metrics) RecordHTTPRequest(method, path, status string, duration float64) {
	m.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration)
}
