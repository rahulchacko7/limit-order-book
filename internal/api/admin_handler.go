package api

import (
	"net/http"
	"runtime"
	"time"

	"limit-order-book/internal/cache"
	"limit-order-book/internal/engine"
	"limit-order-book/internal/metrics"
	"limit-order-book/internal/ws"

	"github.com/gin-gonic/gin"
)

// AdminHandler provides admin API endpoints.
type AdminHandler struct {
	obManager  *engine.OrderBookManager
	wsHub      *ws.Hub
	redisCache *cache.RedisCache
	metrics    *metrics.Metrics
}

// NewAdminHandler creates a new admin handler.
func NewAdminHandler(obManager *engine.OrderBookManager, wsHub *ws.Hub, redisCache *cache.RedisCache, m *metrics.Metrics) *AdminHandler {
	return &AdminHandler{
		obManager:  obManager,
		wsHub:      wsHub,
		redisCache: redisCache,
		metrics:    m,
	}
}

// RegisterRoutes registers admin routes.
func (h *AdminHandler) RegisterRoutes(r *gin.Engine) {
	admin := r.Group("/admin")
	{
		admin.GET("/health", h.Health)
		admin.GET("/metrics", h.Metrics)
		admin.GET("/orderbook", h.OrderBookStats)
		admin.GET("/connections", h.ConnectionStats)
		admin.GET("/dashboard", h.Dashboard)
	}
}

// AdminHealthResponse represents health check response for admin.
type AdminHealthResponse struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Uptime    time.Duration     `json:"uptime"`
	Services  map[string]string `json:"services"`
	System    SystemInfo        `json:"system"`
}

// SystemInfo contains system information.
type SystemInfo struct {
	GoVersion  string  `json:"go_version"`
	GoRoutines int     `json:"goroutines"`
	MemoryMB   float64 `json:"memory_mb"`
}

// Health returns the health status of the system.
func (h *AdminHandler) Health(c *gin.Context) {
	services := make(map[string]string)

	// Check Redis
	if h.redisCache != nil {
		services["redis"] = "healthy"
	} else {
		services["redis"] = "not configured"
	}

	// Check order book
	pairs := h.obManager.ListPairs()
	if len(pairs) > 0 {
		services["orderbook"] = "healthy"
	} else {
		services["orderbook"] = "empty"
	}

	// Check WebSocket
	connections := h.wsHub.TotalClientCount()
	if connections > 0 {
		services["websocket"] = "healthy"
	} else {
		services["websocket"] = "no connections"
	}

	// Determine overall status
	status := "healthy"
	for _, v := range services {
		if v != "healthy" && v != "no connections" && v != "empty" && v != "not configured" {
			status = "degraded"
			break
		}
	}

	c.JSON(http.StatusOK, AdminHealthResponse{
		Status:    status,
		Timestamp: time.Now(),
		Uptime:    time.Since(startTime),
		Services:  services,
		System: SystemInfo{
			GoVersion:  runtime.Version(),
			GoRoutines: runtime.NumGoroutine(),
			MemoryMB:   float64(getMemoryUsage()) / 1024 / 1024,
		},
	})
}

// Metrics returns Prometheus-formatted metrics.
func (h *AdminHandler) Metrics(c *gin.Context) {
	// Get current metrics
	systemMetrics := SystemMetrics{
		GoRoutines:  runtime.NumGoroutine(),
		MemoryUsage: getMemoryUsage(),
		Connections: h.wsHub.TotalClientCount(),
		OrderBooks:  h.obManager.GetOrderBookCount(),
		TotalPairs:  len(h.obManager.ListPairs()),
	}

	c.JSON(http.StatusOK, gin.H{
		"system":    systemMetrics,
		"orderbook": h.getOrderBookMetrics(),
		"websocket": h.getWebSocketMetrics(),
	})
}

// SystemMetrics represents system-level metrics.
type SystemMetrics struct {
	GoRoutines  int    `json:"goroutines"`
	MemoryUsage uint64 `json:"memory_usage_bytes"`
	Connections int    `json:"websocket_connections"`
	OrderBooks  int    `json:"order_books"`
	TotalPairs  int    `json:"total_pairs"`
}

// OrderBookStats returns order book statistics.
func (h *AdminHandler) OrderBookStats(c *gin.Context) {
	pairs := h.obManager.ListPairs()
	pairStats := make([]PairStats, 0, len(pairs))

	for _, pair := range pairs {
		ob := h.obManager.GetOrderBook(pair)
		if ob == nil {
			continue
		}

		bidPrice, bidVol, bidOk := ob.GetBestBid()
		askPrice, askVol, askOk := ob.GetBestAsk()

		bids, asks := ob.GetDepth(10)

		stats := PairStats{
			Pair:       pair,
			OrderCount: ob.GetOrderCount(),
			BestBid:    bidPrice,
			BestBidVol: bidVol,
			BestBidOk:  bidOk,
			BestAsk:    askPrice,
			BestAskVol: askVol,
			BestAskOk:  askOk,
			BidLevels:  len(bids),
			AskLevels:  len(asks),
		}

		if bidOk && askOk {
			stats.Spread = askPrice - bidPrice
			stats.SpreadPercent = (stats.Spread / askPrice) * 100
		}

		pairStats = append(pairStats, stats)
	}

	c.JSON(http.StatusOK, gin.H{
		"pairs":       pairStats,
		"total_pairs": len(pairs),
	})
}

// PairStats represents statistics for a trading pair.
type PairStats struct {
	Pair          string  `json:"pair"`
	OrderCount    int     `json:"order_count"`
	BestBid       float64 `json:"best_bid"`
	BestBidVol    float64 `json:"best_bid_volume"`
	BestBidOk     bool    `json:"best_bid_ok"`
	BestAsk       float64 `json:"best_ask"`
	BestAskVol    float64 `json:"best_ask_volume"`
	BestAskOk     bool    `json:"best_ask_ok"`
	Spread        float64 `json:"spread"`
	SpreadPercent float64 `json:"spread_percent"`
	BidLevels     int     `json:"bid_levels"`
	AskLevels     int     `json:"ask_levels"`
}

// ConnectionStats returns WebSocket connection statistics.
func (h *AdminHandler) ConnectionStats(c *gin.Context) {
	pairs := h.wsHub.Pairs()
	pairConnections := make([]PairConnections, 0, len(pairs))

	for _, pair := range pairs {
		count := h.wsHub.ClientCount(pair)
		pairConnections = append(pairConnections, PairConnections{
			Pair:        pair,
			Connections: count,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total_connections": h.wsHub.TotalClientCount(),
		"pairs":             pairConnections,
	})
}

// PairConnections represents connection count per pair.
type PairConnections struct {
	Pair        string `json:"pair"`
	Connections int    `json:"connections"`
}

// Dashboard returns an HTML dashboard.
func (h *AdminHandler) Dashboard(c *gin.Context) {
	html := DashboardHTML
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, html)
}

// getOrderBookMetrics returns order book metrics.
func (h *AdminHandler) getOrderBookMetrics() map[string]interface{} {
	pairs := h.obManager.ListPairs()
	totalOrders := 0

	for _, pair := range pairs {
		ob := h.obManager.GetOrderBook(pair)
		if ob != nil {
			totalOrders += ob.GetOrderCount()
		}
	}

	return map[string]interface{}{
		"total_pairs":  len(pairs),
		"total_orders": totalOrders,
		"pairs":        pairs,
	}
}

// getWebSocketMetrics returns WebSocket metrics.
func (h *AdminHandler) getWebSocketMetrics() map[string]interface{} {
	pairs := h.wsHub.Pairs()
	connections := make(map[string]int)
	totalConnections := 0

	for _, pair := range pairs {
		count := h.wsHub.ClientCount(pair)
		connections[pair] = count
		totalConnections += count
	}

	return map[string]interface{}{
		"total_connections": totalConnections,
		"pair_connections":  connections,
	}
}

// Global start time for uptime calculation
var startTime = time.Now()

// getMemoryUsage returns current memory usage in bytes.
func getMemoryUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}

// DashboardHTML is the admin dashboard HTML template.
const DashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Order Book Admin Dashboard</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #1a1a2e; color: #eee; }
        .header { background: #16213e; padding: 20px; display: flex; justify-content: space-between; align-items: center; }
        .header h1 { color: #e94560; }
        .status { padding: 5px 15px; border-radius: 20px; font-size: 14px; }
        .status.healthy { background: #27ae60; }
        .status.degraded { background: #f39c12; }
        .container { padding: 20px; max-width: 1400px; margin: 0 auto; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; margin-bottom: 20px; }
        .card { background: #16213e; border-radius: 10px; padding: 20px; }
        .card h2 { color: #e94560; margin-bottom: 15px; font-size: 18px; border-bottom: 1px solid #0f3460; padding-bottom: 10px; }
        .stat { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #0f3460; }
        .stat:last-child { border-bottom: none; }
        .stat-value { color: #4ecca3; font-weight: bold; }
        .pair-table { width: 100%; border-collapse: collapse; }
        .pair-table th, .pair-table td { padding: 10px; text-align: left; border-bottom: 1px solid #0f3460; }
        .pair-table th { color: #e94560; }
        .pair-table tr:hover { background: #0f3460; }
        .positive { color: #27ae60; }
        .negative { color: #e74c3c; }
        .refresh-btn { background: #e94560; color: white; border: none; padding: 10px 20px; border-radius: 5px; cursor: pointer; }
        .refresh-btn:hover { background: #c73e54; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Order Book Admin Dashboard</h1>
        <div>
            <button class="refresh-btn" onclick="refreshDashboard()">Refresh</button>
        </div>
    </div>
    <div class="container">
        <div class="grid">
            <div class="card">
                <h2>System Status</h2>
                <div class="stat"><span>Status</span><span id="status" class="status">Loading...</span></div>
                <div class="stat"><span>Uptime</span><span id="uptime" class="stat-value">Loading...</span></div>
                <div class="stat"><span>Go Version</span><span id="go-version" class="stat-value">Loading...</span></div>
                <div class="stat"><span>Go Routines</span><span id="goroutines" class="stat-value">Loading...</span></div>
                <div class="stat"><span>Memory</span><span id="memory" class="stat-value">Loading...</span></div>
            </div>
            <div class="card">
                <h2>Order Book</h2>
                <div class="stat"><span>Total Pairs</span><span id="total-pairs" class="stat-value">Loading...</span></div>
                <div class="stat"><span>Total Orders</span><span id="total-orders" class="stat-value">Loading...</span></div>
                <div class="stat"><span>Redis Cache</span><span id="redis-status" class="stat-value">Loading...</span></div>
            </div>
            <div class="card">
                <h2>WebSocket</h2>
                <div class="stat"><span>Total Connections</span><span id="ws-connections" class="stat-value">Loading...</span></div>
                <div class="stat"><span>Active Pairs</span><span id="ws-pairs" class="stat-value">Loading...</span></div>
            </div>
        </div>
        <div class="card">
            <h2>Trading Pairs</h2>
            <table class="pair-table">
                <thead>
                    <tr>
                        <th>Pair</th>
                        <th>Orders</th>
                        <th>Best Bid</th>
                        <th>Best Ask</th>
                        <th>Spread</th>
                        <th>Spread %</th>
                        <th>Connections</th>
                    </tr>
                </thead>
                <tbody id="pairs-table">
                    <tr><td colspan="7">Loading...</td></tr>
                </tbody>
            </table>
        </div>
    </div>
    <script>
        async function refreshDashboard() {
            try {
                const [healthRes, orderbookRes, wsRes] = await Promise.all([
                    fetch('/admin/health'),
                    fetch('/admin/orderbook'),
                    fetch('/admin/connections')
                ]);
                
                const health = await healthRes.json();
                const orderbook = await orderbookRes.json();
                const ws = await wsRes.json();
                
                document.getElementById('status').textContent = health.status.toUpperCase();
                document.getElementById('status').className = 'status ' + health.status;
                document.getElementById('uptime').textContent = formatDuration(health.uptime);
                document.getElementById('go-version').textContent = health.system.go_version;
                document.getElementById('goroutines').textContent = health.system.goroutines;
                document.getElementById('memory').textContent = health.system.memory_mb.toFixed(2) + ' MB';
                
                document.getElementById('total-pairs').textContent = orderbook.total_pairs;
                document.getElementById('total-orders').textContent = orderbook.pairs.reduce((sum, p) => sum + p.order_count, 0);
                document.getElementById('redis-status').textContent = health.services.redis || 'unknown';
                
                document.getElementById('ws-connections').textContent = ws.total_connections;
                document.getElementById('ws-pairs').textContent = ws.pairs.length;
                
                const wsConnections = {};
                ws.pairs.forEach(p => wsConnections[p.pair] = p.connections);
                
                const tbody = document.getElementById('pairs-table');
                tbody.innerHTML = orderbook.pairs.map(p => {
                    const spreadClass = p.spread >= 0 ? 'positive' : 'negative';
                    const connections = wsConnections[p.pair] || 0;
                    return '<tr>' +
                        '<td><strong>' + p.pair + '</strong></td>' +
                        '<td>' + p.order_count + '</td>' +
                        '<td>' + (p.best_bid_ok ? p.best_bid.toFixed(2) : 'N/A') + '</td>' +
                        '<td>' + (p.best_ask_ok ? p.best_ask.toFixed(2) : 'N/A') + '</td>' +
                        '<td class="' + spreadClass + '">' + (p.best_bid_ok && p.best_ask_ok ? p.spread.toFixed(2) : 'N/A') + '</td>' +
                        '<td class="' + spreadClass + '">' + (p.best_bid_ok && p.best_ask_ok ? p.spread_percent.toFixed(4) + '%' : 'N/A') + '</td>' +
                        '<td>' + connections + '</td>' +
                        '</tr>';
                }).join('');
                
            } catch (error) {
                console.error('Failed to fetch dashboard data:', error);
            }
        }
        
        function formatDuration(duration) {
            const seconds = Math.floor(duration);
            const hours = Math.floor(seconds / 3600);
            const minutes = Math.floor((seconds % 3600) / 60);
            const secs = seconds % 60;
            return hours + 'h ' + minutes + 'm ' + secs + 's';
        }
        
        refreshDashboard();
        setInterval(refreshDashboard, 5000);
    </script>
</body>
</html>`
