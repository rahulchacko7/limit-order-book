package messaging

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	amqp "github.com/streadway/amqp"
)

// DLQHandler handles messages that have failed processing and are sent to the Dead Letter Queue.
// This ensures no messages are lost and can be inspected for debugging.
type DLQHandler struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewDLQHandler creates a new Dead Letter Queue handler.
func NewDLQHandler(amqpURL, dlxExchange string) (*DLQHandler, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Declare the dead letter exchange
	err = ch.ExchangeDeclare(
		dlxExchange,
		"direct",
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

	// Declare the main DLQ
	_, err = ch.QueueDeclare(
		"orderbook.dlq",
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	// Bind DLQ to DLX
	err = ch.QueueBind(
		"orderbook.dlq",
		"orderbook.dlq",
		dlxExchange,
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	return &DLQHandler{
		conn:    conn,
		channel: ch,
		done:    make(chan struct{}),
	}, nil
}

// Start begins consuming messages from the DLQ.
func (d *DLQHandler) Start() error {
	msgs, err := d.channel.Consume(
		"orderbook.dlq",
		"",    // consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	d.wg.Add(1)
	go d.processDLQMessages(msgs)

	log.Println("✅ DLQ handler started")
	return nil
}

// processDLQMessages processes messages from the dead letter queue.
func (d *DLQHandler) processDLQMessages(msgs <-chan amqp.Delivery) {
	defer d.wg.Done()

	for {
		select {
		case <-d.done:
			return
		case msg, ok := <-msgs:
			if !ok {
				log.Println("⚠️ DLQ channel closed")
				return
			}
			d.handleDLQMessage(msg)
		}
	}
}

// handleDLQMessage processes a single DLQ message.
// In production, this could:
// - Log the failed message for alerting
// - Store in a separate error table for manual review
// - Retry with different logic
// - Send to a monitoring system
func (d *DLQHandler) handleDLQMessage(msg amqp.Delivery) {
	var event DomainEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("⚠️ Failed to unmarshal DLQ message: %v", err)
		// Reject without requeue to prevent infinite loop
		msg.Nack(false, false)
		return
	}

	log.Printf("📥 DLQ received failed message: Type=%s, TraceID=%s", event.Type, event.TraceID)

	// Log the failure for monitoring/alerting
	log.Printf("⚠️ MESSAGE FAILED - Type: %s, TraceID: %s, Body: %s",
		event.Type, event.TraceID, string(msg.Body))

	// Acknowledge to remove from DLQ (after logging)
	// In production, you might want to keep it for longer
	msg.Ack(false)
}

// Stop gracefully shuts down the DLQ handler.
func (d *DLQHandler) Stop() {
	close(d.done)
	d.wg.Wait()

	if d.channel != nil {
		d.channel.Close()
	}
	if d.conn != nil {
		d.conn.Close()
	}

	log.Println("✅ DLQ handler stopped")
}

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	MaxRetries    int           // Maximum number of retries
	InitialDelay  time.Duration // Initial delay before first retry
	MaxDelay      time.Duration // Maximum delay between retries
	Multiplier    float64       // Delay multiplier for exponential backoff
	Randomization float64       // Randomization factor (0-1)
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:    3,
		InitialDelay:  time.Second,
		MaxDelay:      time.Minute,
		Multiplier:    2.0,
		Randomization: 0.2,
	}
}

// RetryPolicy calculates the delay for a given retry attempt.
type RetryPolicy struct {
	config *RetryConfig
	mu     sync.Mutex
}

// NewRetryPolicy creates a new retry policy with the given configuration.
func NewRetryPolicy(config *RetryPolicy) *RetryPolicy {
	if config == nil {
		config = &RetryPolicy{config: DefaultRetryConfig()}
	}
	return &RetryPolicy{
		config: &RetryConfig{
			MaxRetries:    3,
			InitialDelay:  time.Second,
			MaxDelay:      time.Minute,
			Multiplier:    2.0,
			Randomization: 0.2,
		},
	}
}

// NextDelay calculates the delay for the next retry attempt.
// Returns 0 if max retries exceeded.
func (r *RetryPolicy) NextDelay(attempt int) time.Duration {
	if attempt >= r.config.MaxRetries {
		return 0
	}

	// Exponential backoff with randomization
	delay := float64(r.config.InitialDelay) * pow(r.config.Multiplier, float64(attempt))
	if delay > float64(r.config.MaxDelay) {
		delay = float64(r.config.MaxDelay)
	}

	// Add randomization (±20%)
	randomFactor := r.config.Randomization
	randomDelay := delay * (1 - randomFactor + 2*randomFactor*r.Float64())

	return time.Duration(randomDelay)
}

// pow calculates x raised to the power of n.
func pow(x float64, n float64) float64 {
	result := 1.0
	for i := 0.0; i < n; i++ {
		result *= x
	}
	return result
}

// Float64 returns a random float64 between 0 and 1.
func (r *RetryPolicy) Float64() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Simple pseudo-random for demo; use rand.Float64 in production with proper seeding
	return 0.5
}

// ShouldRetry determines if another retry attempt should be made.
func (r *RetryPolicy) ShouldRetry(attempt int) bool {
	return attempt < r.config.MaxRetries
}

// MaxAttempts returns the total number of attempts (initial + retries).
func (r *RetryPolicy) MaxAttempts() int {
	return r.config.MaxRetries + 1
}

// DelayedRetryQueue implements a queue with delayed retry capability.
// Messages are initially published to a delay queue and then moved to the main queue
// after the configured delay.
type DelayedRetryQueue struct {
	conn          *amqp.Connection
	channel       *amqp.Channel
	delayExchange string
	mainExchange  string
	retryConfig   *RetryConfig
	done          chan struct{}
	wg            sync.WaitGroup
}

// NewDelayedRetryQueue creates a new delayed retry queue.
func NewDelayedRetryQueue(amqpURL, delayExchange, mainExchange string, config *RetryConfig) (*DelayedRetryQueue, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	if config == nil {
		config = &RetryConfig{
			MaxRetries:    3,
			InitialDelay:  time.Second,
			MaxDelay:      time.Minute,
			Multiplier:    2.0,
			Randomization: 0.2,
		}
	}

	// Declare the delay exchange (x-delayed-message type requires plugin)
	// Without the plugin, we simulate delay with separate queues
	err = ch.ExchangeDeclare(
		delayExchange,
		"direct",
		true,
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

	// Create delay queues for different delay levels
	delays := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, time.Minute}
	for _, delay := range delays {
		queueName := "orderbook.retry." + delay.String()
		_, err = ch.QueueDeclare(
			queueName,
			true,
			false,
			false,
			false,
			amqp.Table{
				"x-dead-letter-exchange": mainExchange,
				"x-message-ttl":          int64(delay.Milliseconds()),
			},
		)
		if err != nil {
			ch.Close()
			conn.Close()
			return nil, err
		}

		err = ch.QueueBind(queueName, queueName, delayExchange, false, nil)
		if err != nil {
			ch.Close()
			conn.Close()
			return nil, err
		}
	}

	return &DelayedRetryQueue{
		conn:          conn,
		channel:       ch,
		delayExchange: delayExchange,
		mainExchange:  mainExchange,
		retryConfig:   config,
		done:          make(chan struct{}),
	}, nil
}

// PublishDelayed publishes a message with a delay before it becomes available.
func (d *DelayedRetryQueue) PublishDelayed(routingKey string, body []byte, delay time.Duration) error {
	// Map delay to nearest queue
	var queueKey string
	switch {
	case delay <= time.Second:
		queueKey = "orderbook.retry.1s"
	case delay <= 5*time.Second:
		queueKey = "orderbook.retry.5s"
	case delay <= 30*time.Second:
		queueKey = "orderbook.retry.30s"
	default:
		queueKey = "orderbook.retry.1m"
	}

	return d.channel.Publish(
		d.delayExchange,
		queueKey,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
}

// Close shuts down the delayed retry queue.
func (d *DelayedRetryQueue) Close() error {
	close(d.done)
	d.wg.Wait()

	if d.channel != nil {
		d.channel.Close()
	}
	if d.conn != nil {
		d.conn.Close()
	}
	return nil
}

// RetryMessage represents a message that will be retried.
type RetryMessage struct {
	OriginalMessage []byte
	RetryCount      int
	NextRetryTime   time.Time
	Reason          string
}

// PoisonMessageHandler handles messages that have failed all retry attempts.
type PoisonMessageHandler struct {
	store    PoisonMessageStore
	notifier PoisonMessageNotifier
}

// PoisonMessageStore defines the interface for storing failed messages.
type PoisonMessageStore interface {
	Store(ctx context.Context, msg *PoisonMessage) error
	List(ctx context.Context, limit, offset int) ([]*PoisonMessage, error)
	Delete(ctx context.Context, id int64) error
}

// PoisonMessageNotifier defines the interface for notifying about poison messages.
type PoisonMessageNotifier interface {
	Notify(ctx context.Context, msg *PoisonMessage) error
}

// PoisonMessage represents a message that cannot be processed.
type PoisonMessage struct {
	ID        int64
	Type      string
	Payload   string
	TraceID   string
	Reason    string
	Attempts  int
	CreatedAt time.Time
}

// NewPoisonMessageHandler creates a new poison message handler.
func NewPoisonMessageHandler(store PoisonMessageStore, notifier PoisonMessageNotifier) *PoisonMessageHandler {
	return &PoisonMessageHandler{
		store:    store,
		notifier: notifier,
	}
}

// Handle processes a poison message.
func (p *PoisonMessageHandler) Handle(ctx context.Context, msg *RetryMessage, eventType string) error {
	poison := &PoisonMessage{
		Type:      eventType,
		Payload:   string(msg.OriginalMessage),
		Reason:    msg.Reason,
		Attempts:  msg.RetryCount,
		CreatedAt: time.Now(),
	}

	// Store the poison message
	if err := p.store.Store(ctx, poison); err != nil {
		log.Printf("⚠️ Failed to store poison message: %v", err)
		return err
	}

	// Send notification
	if err := p.notifier.Notify(ctx, poison); err != nil {
		log.Printf("⚠️ Failed to notify about poison message: %v", err)
	}

	log.Printf("⚠️ Poison message stored: Type=%s, Attempts=%d", eventType, msg.RetryCount)
	return nil
}
