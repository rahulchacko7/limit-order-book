package middleware

import (
	"errors"
	"sync"
	"time"
)

// CircuitState represents the state of the circuit breaker.
type CircuitState int

const (
	// CircuitClosed - normal operation, requests pass through
	CircuitClosed CircuitState = iota
	// CircuitOpen - circuit is open, requests fail immediately
	CircuitOpen
	// CircuitHalfOpen - testing if service has recovered
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

var (
	// ErrCircuitOpen is returned when the circuit is open
	ErrCircuitOpen = errors.New("circuit is open")
	// ErrCircuitTimeout is returned when the timeout is exceeded
	ErrCircuitTimeout = errors.New("circuit breaker timeout")
)

// CircuitBreakerConfig holds configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int           // Number of failures before opening circuit
	SuccessThreshold int           // Number of successes in half-open to close
	Timeout          time.Duration // Time to wait before trying half-open
	RequestTimeout   time.Duration // Timeout for individual requests
	Interval         time.Duration // Window for counting failures
}

// DefaultCircuitBreakerConfig returns default configuration.
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		RequestTimeout:   10 * time.Second,
		Interval:         60 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	name            string
	config          *CircuitBreakerConfig
	state           CircuitState
	failures        int
	successes       int
	lastStateChange time.Time
	lastFailure     time.Time
	mu              sync.RWMutex
	stateChanged    chan CircuitState
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(name string, config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreaker{
		name:            name,
		config:          config,
		state:           CircuitClosed,
		stateChanged:    make(chan CircuitState, 1),
		lastStateChange: time.Now(),
	}
}

// Name returns the circuit breaker name.
func (c *CircuitBreaker) Name() string {
	return c.name
}

// State returns the current state.
func (c *CircuitBreaker) State() CircuitState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// StateChanged returns a channel that receives state change notifications.
func (c *CircuitBreaker) StateChanged() <-chan CircuitState {
	return c.stateChanged
}

// Execute runs the given function if the circuit allows it.
func (c *CircuitBreaker) Execute(fn func() error) error {
	if !c.allowRequest() {
		return ErrCircuitOpen
	}

	// Create a channel for the result
	result := make(chan error, 1)

	go func() {
		result <- c.executeWithTimeout(fn)
	}()

	select {
	case err := <-result:
		c.recordResult(err)
		return err
	case <-time.After(c.config.RequestTimeout):
		c.recordResult(ErrCircuitTimeout)
		return ErrCircuitTimeout
	}
}

// executeWithTimeout runs the function with timeout.
func (c *CircuitBreaker) executeWithTimeout(fn func() error) error {
	done := make(chan struct{})
	var err error

	go func() {
		err = fn()
		close(done)
	}()

	select {
	case <-done:
		return err
	case <-time.After(c.config.RequestTimeout):
		return ErrCircuitTimeout
	}
}

// allowRequest checks if a request should be allowed.
func (c *CircuitBreaker) allowRequest() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if timeout has elapsed
		if time.Since(c.lastStateChange) >= c.config.Timeout {
			c.setState(CircuitHalfOpen)
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	default:
		return false
	}
}

// recordResult records the result of a request.
func (c *CircuitBreaker) recordResult(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err == nil {
		c.successes++
		if c.state == CircuitHalfOpen && c.successes >= c.config.SuccessThreshold {
			c.setState(CircuitClosed)
			c.failures = 0
			c.successes = 0
		}
	} else {
		c.failures++
		if c.state == CircuitClosed && c.failures >= c.config.FailureThreshold {
			c.setState(CircuitOpen)
		} else if c.state == CircuitHalfOpen {
			c.setState(CircuitOpen)
		}
	}

	c.lastFailure = time.Now()
}

// setState changes the circuit state.
func (c *CircuitBreaker) setState(state CircuitState) {
	if c.state != state {
		c.state = state
		c.lastStateChange = time.Now()

		// Reset counters
		if state == CircuitClosed {
			c.failures = 0
			c.successes = 0
		} else if state == CircuitHalfOpen {
			c.successes = 0
		}

		// Notify state change
		select {
		case c.stateChanged <- state:
		default:
		}
	}
}

// Reset resets the circuit breaker to closed state.
func (c *CircuitBreaker) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setState(CircuitClosed)
	c.failures = 0
	c.successes = 0
}

// ForceOpen forces the circuit breaker to open state.
func (c *CircuitBreaker) ForceOpen() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setState(CircuitOpen)
}

// Metrics returns current metrics.
func (c *CircuitBreaker) Metrics() CircuitBreakerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CircuitBreakerMetrics{
		Name:            c.name,
		State:           c.state.String(),
		Failures:        c.failures,
		Successes:       c.successes,
		LastStateChange: c.lastStateChange,
	}
}

// CircuitBreakerMetrics holds metrics for a circuit breaker.
type CircuitBreakerMetrics struct {
	Name            string        `json:"name"`
	State           string        `json:"state"`
	Failures        int           `json:"failures"`
	Successes       int           `json:"successes"`
	LastStateChange time.Time     `json:"last_state_change"`
	Uptime          time.Duration `json:"uptime"`
}

// CircuitBreakerManager manages multiple circuit breakers.
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	mu       sync.RWMutex
}

// NewCircuitBreakerManager creates a new circuit breaker manager.
func NewCircuitBreakerManager() *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// Register registers a circuit breaker.
func (m *CircuitBreakerManager) Register(cb *CircuitBreaker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.breakers[cb.Name()] = cb
}

// Get retrieves a circuit breaker by name.
func (m *CircuitBreakerManager) Get(name string) (*CircuitBreaker, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cb, ok := m.breakers[name]
	return cb, ok
}

// All returns all circuit breakers.
func (m *CircuitBreakerManager) All() []*CircuitBreaker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*CircuitBreaker, 0, len(m.breakers))
	for _, cb := range m.breakers {
		result = append(result, cb)
	}
	return result
}

// Metrics returns metrics for all circuit breakers.
func (m *CircuitBreakerManager) Metrics() []CircuitBreakerMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]CircuitBreakerMetrics, 0, len(m.breakers))
	for _, cb := range m.breakers {
		metrics := cb.Metrics()
		metrics.Uptime = time.Since(metrics.LastStateChange)
		result = append(result, metrics)
	}
	return result
}
