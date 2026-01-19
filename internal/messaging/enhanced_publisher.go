package messaging

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/streadway/amqp"
)

// MessageConfig holds configuration for message publishing.
type MessageConfig struct {
	Persistent   bool          // Whether to persist messages
	ContentType  string        // MIME type of the message
	Priority     uint8         // Message priority (0-9)
	Expiration   time.Duration // Message TTL
	Confirmation bool          // Wait for publisher confirms
}

// DefaultMessageConfig returns default message configuration.
func DefaultMessageConfig() *MessageConfig {
	return &MessageConfig{
		Persistent:   true,
		ContentType:  "application/json",
		Priority:     0,
		Expiration:   0,
		Confirmation: true,
	}
}

// EnhancedPublisher provides advanced RabbitMQ publishing capabilities.
// ENHANCED FEATURES:
//   - Publisher confirms for reliable delivery
//   - Automatic message ID generation
//   - Correlation ID for request tracing
//   - Idempotency support
//   - Retry with backoff
type EnhancedPublisher struct {
	conn        *amqp.Connection
	channel     *amqp.Channel
	exchange    string
	config      *MessageConfig
	confirms    <-chan amqp.Confirmation
	mu          sync.RWMutex
	pendingacks map[uint64]chan error
	done        chan struct{}
	wg          sync.WaitGroup
}

// NewEnhancedPublisher creates a new enhanced publisher with confirms enabled.
func NewEnhancedPublisher(amqpURL, exchange string, config *MessageConfig) (*EnhancedPublisher, error) {
	if config == nil {
		config = DefaultMessageConfig()
	}

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Enable publisher confirms if configured
	if config.Confirmation {
		if err := ch.Confirm(false); err != nil {
			ch.Close()
			conn.Close()
			return nil, err
		}
	}

	// Declare exchange
	err = ch.ExchangeDeclare(
		exchange,
		"topic",
		true, // durable
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	publisher := &EnhancedPublisher{
		conn:        conn,
		channel:     ch,
		exchange:    exchange,
		config:      config,
		pendingacks: make(map[uint64]chan error),
		done:        make(chan struct{}),
	}

	// Start confirmation listener
	if config.Confirmation {
		publisher.confirms = ch.NotifyPublish(make(chan amqp.Confirmation))
		publisher.wg.Add(1)
		go publisher.handleConfirms()
	}

	return publisher, nil
}

// handleConfirms processes publisher confirmations.
func (p *EnhancedPublisher) handleConfirms() {
	defer p.wg.Done()

	for {
		select {
		case <-p.done:
			return
		case confirm, ok := <-p.confirms:
			if !ok {
				return
			}

			p.mu.Lock()
			ackChan, exists := p.pendingacks[confirm.DeliveryTag]
			delete(p.pendingacks, confirm.DeliveryTag)
			p.mu.Unlock()

			if exists {
				if confirm.Ack {
					ackChan <- nil
				} else {
					ackChan <- &PublishError{
						DeliveryTag: confirm.DeliveryTag,
						Message:     "Message was nacked by broker",
					}
				}
				close(ackChan)
			}
		}
	}
}

// PublishOptions holds optional parameters for publishing.
type PublishOptions struct {
	MessageID      string        // Unique message identifier
	CorrelationID  string        // For request tracing
	ReplyTo        string        // Reply queue
	Priority       uint8         // Message priority
	Expiration     time.Duration // Message TTL
	Timestamp      time.Time     // Message timestamp
	Headers        amqp.Table    // Custom headers
	IdempotencyKey string        // For deduplication
}

// Publish publishes a message with optional parameters.
func (p *EnhancedPublisher) Publish(routingKey string, payload interface{}, opts *PublishOptions) error {
	// Generate message ID if not provided
	msgID := uuid.New().String()

	// Generate correlation ID if not provided
	correlationID := msgID
	if opts != nil && opts.CorrelationID != "" {
		correlationID = opts.CorrelationID
	}

	// Marshal payload
	body, err := json.Marshal(payload)
	if err != nil {
		return &PublishError{Message: err.Error()}
	}

	// Set defaults
	priority := p.config.Priority
	expiration := p.config.Expiration
	timestamp := time.Now()
	headers := amqp.Table{}

	if opts != nil {
		if opts.MessageID != "" {
			msgID = opts.MessageID
		}
		if opts.Priority != 0 {
			priority = opts.Priority
		}
		if opts.Expiration > 0 {
			expiration = opts.Expiration
		}
		if !opts.Timestamp.IsZero() {
			timestamp = opts.Timestamp
		}
		if opts.Headers != nil {
			headers = opts.Headers
		}
	}

	// Create publishing
	pub := amqp.Publishing{
		ContentType:   p.config.ContentType,
		DeliveryMode:  amqp.Persistent,
		Priority:      priority,
		Timestamp:     timestamp,
		MessageId:     msgID,
		CorrelationId: correlationID,
		Body:          body,
		Headers:       headers,
	}

	if expiration > 0 {
		pub.Expiration = string(rune(int(expiration/time.Millisecond))) + "ms"
	}

	if opts != nil && opts.ReplyTo != "" {
		pub.ReplyTo = opts.ReplyTo
	}

	// Add idempotency key to headers if provided
	if opts != nil && opts.IdempotencyKey != "" {
		if pub.Headers == nil {
			pub.Headers = amqp.Table{}
		}
		pub.Headers["idempotency_key"] = opts.IdempotencyKey
	}

	// Wait for confirmation if enabled
	if p.config.Confirmation {
		ackChan := make(chan error, 1)

		// Get next delivery tag before publishing
		p.mu.RLock()
		nextTag := uint64(len(p.pendingacks) + 1)
		p.mu.RUnlock()

		p.mu.Lock()
		p.pendingacks[nextTag] = ackChan
		p.mu.Unlock()

		// Publish the message
		err = p.channel.Publish(
			p.exchange,
			routingKey,
			false, // mandatory
			false, // immediate
			pub,
		)
		if err != nil {
			p.mu.Lock()
			delete(p.pendingacks, nextTag)
			p.mu.Unlock()
			return &PublishError{Message: err.Error()}
		}

		// Wait for confirmation with timeout
		select {
		case ackErr := <-ackChan:
			if ackErr != nil {
				return ackErr
			}
		case <-time.After(10 * time.Second):
			p.mu.Lock()
			delete(p.pendingacks, nextTag)
			p.mu.Unlock()
			return &PublishError{
				DeliveryTag: nextTag,
				Message:     "Confirmation timeout",
			}
		}
	} else {
		// Fire and forget
		err = p.channel.Publish(
			p.exchange,
			routingKey,
			false,
			false,
			pub,
		)
		if err != nil {
			return &PublishError{Message: err.Error()}
		}
	}

	log.Printf("📤 Event published: %s (ID: %s, CorrID: %s)", routingKey, msgID, correlationID)
	return nil
}

// PublishWithTrace publishes a message with tracing information.
func (p *EnhancedPublisher) PublishWithTrace(routingKey string, payload interface{}, traceID string) error {
	return p.Publish(routingKey, payload, &PublishOptions{
		CorrelationID: traceID,
		Headers: amqp.Table{
			"trace_id": traceID,
		},
	})
}

// PublishWithRetry publishes a message with automatic retry on failure.
func (p *EnhancedPublisher) PublishWithRetry(routingKey string, payload interface{}, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := p.Publish(routingKey, payload, nil)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("⚠️ Publish attempt %d failed: %v", attempt+1, err)

		if attempt < maxRetries {
			// Exponential backoff
			delay := time.Duration(attempt+1) * time.Second
			time.Sleep(delay)
		}
	}

	return lastErr
}

// GetPendingCount returns the number of messages waiting for confirmation.
func (p *EnhancedPublisher) GetPendingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.pendingacks)
}

// Close shuts down the publisher gracefully.
func (p *EnhancedPublisher) Close() error {
	close(p.done)
	p.wg.Wait()

	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			log.Printf("⚠️ Closing with %d pending acks", p.GetPendingCount())
			goto cleanup
		case <-ticker.C:
			p.mu.RLock()
			count := len(p.pendingacks)
			p.mu.RUnlock()
			if count == 0 {
				goto cleanup
			}
		}
	}

cleanup:
	if p.channel != nil {
		if err := p.channel.Close(); err != nil {
			log.Printf("⚠️ Error closing channel: %v", err)
		}
	}
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			log.Printf("⚠️ Error closing connection: %v", err)
		}
	}

	log.Println("✅ Enhanced publisher closed")
	return nil
}

// PublishError represents an error during message publishing.
type PublishError struct {
	DeliveryTag uint64
	Message     string
}

func (e *PublishError) Error() string {
	return e.Message
}

// IdempotencyChecker checks for duplicate messages.
type IdempotencyChecker struct {
	store IdempotencyStore
	ttl   time.Duration
	mu    sync.RWMutex
	seen  map[string]time.Time
}

// IdempotencyStore defines the interface for storing idempotency keys.
type IdempotencyStore interface {
	// Store idempotency key with expiration
	Store(ctx context.Context, key string, ttl time.Duration) error
	// Check if key exists
	Exists(ctx context.Context, key string) (bool, error)
	// Remove key
	Delete(ctx context.Context, key string) error
}

// NewIdempotencyChecker creates a new idempotency checker.
func NewIdempotencyChecker(store IdempotencyStore, ttl time.Duration) *IdempotencyChecker {
	return &IdempotencyChecker{
		store: store,
		ttl:   ttl,
		seen:  make(map[string]time.Time),
	}
}

// Check checks if a message with the given idempotency key has been processed.
func (i *IdempotencyChecker) Check(ctx context.Context, key string) (bool, error) {
	// Check in-memory cache first
	i.mu.RLock()
	if t, ok := i.seen[key]; ok {
		if time.Since(t) < i.ttl {
			i.mu.RUnlock()
			return true, nil
		}
		delete(i.seen, key)
	}
	i.mu.RUnlock()

	// Check persistent store
	exists, err := i.store.Exists(ctx, key)
	if err != nil {
		return false, err
	}

	if exists {
		// Add to in-memory cache
		i.mu.Lock()
		i.seen[key] = time.Now()
		i.mu.Unlock()
		return true, nil
	}

	// Store the key
	if err := i.store.Store(ctx, key, i.ttl); err != nil {
		log.Printf("⚠️ Failed to store idempotency key: %v", err)
	}

	return false, nil
}

// InMemoryIdempotencyStore is a simple in-memory implementation.
type InMemoryIdempotencyStore struct {
	mu   sync.RWMutex
	keys map[string]time.Time
	ttl  time.Duration
}

// NewInMemoryIdempotencyStore creates a new in-memory idempotency store.
func NewInMemoryIdempotencyStore(ttl time.Duration) *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{
		keys: make(map[string]time.Time),
		ttl:  ttl,
	}
}

// Store implements IdempotencyStore.
func (s *InMemoryIdempotencyStore) Store(ctx context.Context, key string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[key] = time.Now().Add(ttl)
	return nil
}

// Exists implements IdempotencyStore.
func (s *InMemoryIdempotencyStore) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.keys[key]; ok {
		if time.Now().Before(t) {
			return true, nil
		}
		delete(s.keys, key)
	}
	return false, nil
}

// Delete implements IdempotencyStore.
func (s *InMemoryIdempotencyStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, key)
	return nil
}
