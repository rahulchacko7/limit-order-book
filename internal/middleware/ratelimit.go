package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter provides token bucket rate limiting.
// TOKEN BUCKET ALGORITHM:
//   - Tokens are added to the bucket at a fixed rate
//   - Each request consumes one token
//   - Requests are rejected when the bucket is empty
//   - Burst allows temporary exceeding of rate limit
type RateLimiter struct {
	rate  float64 // Tokens per second
	burst int     // Maximum burst size
	store RateLimitStore
}

// RateLimitStore defines the interface for storing rate limit state.
type RateLimitStore interface {
	// GetRemaining returns (remaining, resetTime, allowed)
	GetRemaining(key string, rate float64, burst int, window time.Duration) (int, time.Time, bool)
	// Reset resets the rate limit for a key
	Reset(key string)
}

// RateLimitConfig holds configuration for rate limiting.
type RateLimitConfig struct {
	RequestsPerSecond float64       // Token refill rate
	Burst             int           // Maximum burst size
	Window            time.Duration // Time window for rate limiting
	Prefix            string        // Redis key prefix
	Enabled           bool          // Enable/disable rate limiting
	SkipOnError       bool          // Continue if store fails
	SkipPaths         []string      // Paths to skip rate limiting
}

// DefaultRateLimitConfig returns default rate limit configuration.
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		RequestsPerSecond: 10.0,
		Burst:             20,
		Window:            time.Second,
		Prefix:            "ratelimit:",
		Enabled:           true,
		SkipOnError:       true,
		SkipPaths: []string{
			"/health",
			"/ready",
		},
	}
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config *RateLimitConfig) *RateLimiter {
	if config == nil {
		config = DefaultRateLimitConfig()
	}

	var store RateLimitStore
	if config.Enabled {
		// Use in-memory store by default
		store = NewInMemoryRateStore()
	}

	return &RateLimiter{
		rate:  config.RequestsPerSecond,
		burst: config.Burst,
		store: store,
	}
}

// GinMiddleware returns the Gin middleware for rate limiting.
func (r *RateLimiter) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip if rate limiting is disabled
		if r.store == nil {
			c.Next()
			return
		}

		// Skip for certain paths
		path := c.Request.URL.Path
		for _, skipPath := range []string{"/health", "/ready"} {
			if path == skipPath {
				c.Next()
				return
			}
		}

		// Generate rate limit key
		// Use user ID if authenticated, otherwise use IP
		key := r.getRateLimitKey(c)

		// Check rate limit
		remaining, resetTime, allowed := r.store.GetRemaining(
			key,
			r.rate,
			r.burst,
			time.Second,
		)

		// Set rate limit headers
		c.Header("X-RateLimit-Limit", strconv.Itoa(r.burst))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", resetTime.Format(time.RFC3339))

		if !allowed {
			retryAfter := time.Until(resetTime)
			c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))

			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate_limit_exceeded",
				"message":     "too many requests, please retry later",
				"code":        "RATE_LIMIT_EXCEEDED",
				"retry_after": retryAfter.Seconds(),
				"limit":       r.burst,
				"remaining":   0,
				"reset_at":    resetTime.Format(time.RFC3339),
			})
			return
		}

		c.Next()
	}
}

// getRateLimitKey generates a unique key for rate limiting.
func (r *RateLimiter) getRateLimitKey(c *gin.Context) string {
	// Try to get user ID from context
	if userID, exists := c.Get(ContextKeyUserID); exists {
		if id, ok := userID.(int64); ok {
			return "user:" + strconv.FormatInt(id, 10)
		}
	}

	// Fall back to IP address
	clientIP := c.ClientIP()
	return "ip:" + clientIP
}

// InMemoryRateStore provides in-memory rate limiting.
// WARNING: Not suitable for distributed systems.
// Use RedisRateStore for production/distributed deployments.
type InMemoryRateStore struct {
	mu      sync.RWMutex
	buckets map[string]*TokenBucket
}

// TokenBucket represents a token bucket.
type TokenBucket struct {
	Tokens        float64
	LastRefill    time.Time
	RefillRate    float64
	BurstCapacity int
}

// NewInMemoryRateStore creates a new in-memory rate store.
func NewInMemoryRateStore() *InMemoryRateStore {
	return &InMemoryRateStore{
		buckets: make(map[string]*TokenBucket),
	}
}

// GetRemaining implements RateLimitStore.
func (s *InMemoryRateStore) GetRemaining(key string, rate float64, burst int, window time.Duration) (int, time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	bucket, exists := s.buckets[key]
	if !exists {
		// Create new bucket with full tokens
		s.buckets[key] = &TokenBucket{
			Tokens:        float64(burst),
			LastRefill:    now,
			RefillRate:    rate,
			BurstCapacity: burst,
		}
		return burst, now.Add(window), true
	}

	// Calculate tokens to add since last refill
	elapsed := now.Sub(bucket.LastRefill).Seconds()
	tokensToAdd := elapsed * rate
	bucket.Tokens += tokensToAdd
	if bucket.Tokens > float64(burst) {
		bucket.Tokens = float64(burst)
	}
	bucket.LastRefill = now

	// Check if we have tokens
	if bucket.Tokens >= 1 {
		bucket.Tokens -= 1
		remaining := int(bucket.Tokens)
		if remaining < 0 {
			remaining = 0
		}
		return remaining, now.Add(window), true
	}

	// No tokens available
	return 0, now.Add(window), false
}

// Reset implements RateLimitStore.
func (s *InMemoryRateStore) Reset(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buckets, key)
}

// SlidingWindowLimiter provides sliding window rate limiting.
// SLIDING WINDOW ALGORITHM:
//   - More accurate than fixed window
//   - Counts requests in current and previous window
//   - Smooths rate limiting boundaries
type SlidingWindowLimiter struct {
	requests map[string][]time.Time
	mu       sync.RWMutex
	limit    int
	window   time.Duration
}

// NewSlidingWindowLimiter creates a new sliding window limiter.
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

// Allow checks if a request is allowed.
func (s *SlidingWindowLimiter) Allow(key string) (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-s.window)

	// Get existing requests for this key
	times := s.requests[key]

	// Filter out old requests
	var validTimes []time.Time
	for _, t := range times {
		if t.After(windowStart) {
			validTimes = append(validTimes, t)
		}
	}

	// Check if under limit
	if len(validTimes) < s.limit {
		validTimes = append(validTimes, now)
		s.requests[key] = validTimes
		return true, s.limit - len(validTimes) - 1
	}

	// Over limit
	return false, 0
}

// Remaining returns the number of remaining requests.
func (s *SlidingWindowLimiter) Remaining(key string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	windowStart := now.Add(-s.window)

	times := s.requests[key]
	count := 0
	for _, t := range times {
		if t.After(windowStart) {
			count++
		}
	}

	remaining := s.limit - count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset resets the rate limit for a key.
func (s *SlidingWindowLimiter) Reset(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.requests, key)
}

// ConnectionLimiter limits concurrent connections per IP.
// Useful for preventing connection exhaustion attacks.
type ConnectionLimiter struct {
	connections map[string]int
	mu          sync.RWMutex
	limit       int
}

// NewConnectionLimiter creates a new connection limiter.
func NewConnectionLimiter(limit int) *ConnectionLimiter {
	return &ConnectionLimiter{
		connections: make(map[string]int),
		limit:       limit,
	}
}

// Allow checks if a new connection is allowed.
func (l *ConnectionLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := l.connections[ip]
	if count >= l.limit {
		return false
	}
	l.connections[ip]++
	return true
}

// Release removes a connection for an IP.
func (l *ConnectionLimiter) Release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if count, exists := l.connections[ip]; exists && count > 0 {
		l.connections[ip]--
		if l.connections[ip] == 0 {
			delete(l.connections, ip)
		}
	}
}

// Count returns the current connection count for an IP.
func (l *ConnectionLimiter) Count(ip string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.connections[ip]
}

// TotalCount returns the total connection count.
func (l *ConnectionLimiter) TotalCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	total := 0
	for _, count := range l.connections {
		total += count
	}
	return total
}

// RateLimitResponse is the standard rate limit exceeded response.
type RateLimitResponse struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	Code       string `json:"code"`
	RetryAfter int    `json:"retry_after"`
	Limit      int    `json:"limit"`
	Remaining  int    `json:"remaining"`
	ResetAt    string `json:"reset_at"`
}
