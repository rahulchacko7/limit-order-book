package messaging

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/streadway/amqp"

	"limit-order-book/internal/models"
	"limit-order-book/internal/store"
)

// ProcessedMessageStore tracks processed message IDs for idempotency.
type ProcessedMessageStore struct {
	mu        sync.RWMutex
	processed map[string]time.Time
	ttl       time.Duration
}

// NewProcessedMessageStore creates a new processed message store.
func NewProcessedMessageStore(ttl time.Duration) *ProcessedMessageStore {
	return &ProcessedMessageStore{
		processed: make(map[string]time.Time),
		ttl:       ttl,
	}
}

// IsProcessed checks if a message has already been processed.
func (s *ProcessedMessageStore) IsProcessed(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.processed[id]
	if !ok {
		return false
	}
	if time.Since(t) > s.ttl {
		delete(s.processed, id)
		return false
	}
	return true
}

// MarkProcessed marks a message as processed.
func (s *ProcessedMessageStore) MarkProcessed(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processed[id] = time.Now()
}

// Cleanup removes expired entries.
func (s *ProcessedMessageStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, t := range s.processed {
		if now.Sub(t) > s.ttl {
			delete(s.processed, id)
		}
	}
}

// EventType represents the type of domain event.
type EventType string

const (
	EventOrderPlaced   EventType = "order.placed"
	EventTradeExecuted EventType = "trade.executed"
	EventOrderFilled   EventType = "order.filled"
	EventOrderCanceled EventType = "order.canceled"
)

// DomainEvent is the base event structure.
type DomainEvent struct {
	Type    EventType   `json:"type"`
	Payload interface{} `json:"payload"`
	TraceID string      `json:"trace_id"`
}

// OrderPlacedPayload is the payload for order placed events.
type OrderPlacedPayload struct {
	Order *models.Order `json:"order"`
}

// TradeExecutedPayload is the payload for trade executed events.
type TradeExecutedPayload struct {
	Trade *models.Trade `json:"trade"`
}

// OrderFilledPayload is the payload for order filled events.
type OrderFilledPayload struct {
	OrderID int64 `json:"order_id"`
}

// OrderCanceledPayload is the payload for order canceled events.
type OrderCanceledPayload struct {
	OrderID int64  `json:"order_id"`
	Reason  string `json:"reason,omitempty"`
}

// Consumer handles asynchronous event processing.
// CONSUMER ARCHITECTURE:
//   - Events are consumed from RabbitMQ queues
//   - Each event type can have dedicated workers
//   - Failed events are requeued or sent to dead letter queue
//   - Idempotency checking prevents duplicate processing
//   - Transactions ensure atomic order/trade persistence
//
// WORKFLOW:
//  1. Matching engine publishes events (order.placed, trade.executed)
//  2. RabbitMQ delivers events to queues
//  3. Consumer workers process events with idempotency check
//  4. Workers persist data to PostgreSQL using transactions
type Consumer struct {
	conn             *amqp.Connection
	channel          *amqp.Channel
	store            *store.PostgresStore
	workers          int
	done             chan struct{}
	wg               sync.WaitGroup
	idempotencyStore *ProcessedMessageStore
}

// NewConsumer creates a new event consumer.
func NewConsumer(amqpURL string, store *store.PostgresStore, workers int) (*Consumer, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	// Set QoS for fair dispatch
	err = ch.Qos(10, 0, false)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		conn:             conn,
		channel:          ch,
		store:            store,
		workers:          workers,
		done:             make(chan struct{}),
		idempotencyStore: NewProcessedMessageStore(24 * time.Hour),
	}, nil
}

// Start begins consuming events from all queues.
func (c *Consumer) Start(exchange string) error {
	// Declare exchange (same as publisher)
	err := c.channel.ExchangeDeclare(
		exchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// Declare queues for each event type
	queues := []string{
		"order.placed",
		"trade.executed",
		"order.filled",
		"order.canceled",
	}

	for _, queue := range queues {
		// Declare queue
		q, err := c.channel.QueueDeclare(
			queue,
			true,  // durable
			false, // delete when unused
			false, // exclusive
			false, // no-wait
			amqp.Table{
				"x-dead-letter-exchange": exchange + ".dlx",
			},
		)
		if err != nil {
			return err
		}

		// Bind queue to exchange
		err = c.channel.QueueBind(
			q.Name,
			queue,
			exchange,
			false,
			nil,
		)
		if err != nil {
			return err
		}

		// Start workers for this queue
		for i := 0; i < c.workers; i++ {
			c.wg.Add(1)
			go c.consumeQueue(queue, q.Name)
		}
	}

	log.Printf("📥 Consumer started with %d workers per queue", c.workers)
	return nil
}

// consumeQueue continuously consumes messages from a queue.
func (c *Consumer) consumeQueue(eventType, queueName string) {
	defer c.wg.Done()

	msgs, err := c.channel.Consume(
		queueName,
		"",    // consumer tag
		false, // auto-ack (false for manual ack)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		log.Printf("⚠️ Failed to start consuming from %s: %v", queueName, err)
		return
	}

	for {
		select {
		case <-c.done:
			return
		case msg, ok := <-msgs:
			if !ok {
				log.Printf("⚠️ Queue %s closed", queueName)
				return
			}
			c.processMessage(EventType(eventType), msg)
		}
	}
}

// processMessage handles a single message from the queue.
func (c *Consumer) processMessage(eventType EventType, msg amqp.Delivery) {
	var event DomainEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("⚠️ Failed to unmarshal event: %v", err)
		// Reject message without requeue (send to DLQ)
		msg.Nack(false, false)
		return
	}

	// Idempotency check - skip if already processed
	msgID := msg.MessageId
	if msgID == "" {
		msgID = string(event.Type) + ":" + event.TraceID
	}
	if c.idempotencyStore.IsProcessed(msgID) {
		log.Printf("⏭️ Message already processed, skipping: %s", msgID)
		msg.Ack(false)
		return
	}

	ctx := context.Background()

	var processingErr error
	switch event.Type {
	case EventOrderPlaced:
		processingErr = c.handleOrderPlaced(ctx, event)
	case EventTradeExecuted:
		processingErr = c.handleTradeExecuted(ctx, event)
	case EventOrderFilled:
		processingErr = c.handleOrderFilled(ctx, event)
	case EventOrderCanceled:
		processingErr = c.handleOrderCanceled(ctx, event)
	default:
		log.Printf("⚠️ Unknown event type: %s", event.Type)
	}

	if processingErr != nil {
		log.Printf("⚠️ Failed to process event %s: %v", event.Type, processingErr)
		// Nack and send to DLQ for retry
		msg.Nack(false, false)
		return
	}

	// Mark as processed and acknowledge
	c.idempotencyStore.MarkProcessed(msgID)
	msg.Ack(false)
}

// handleOrderPlaced persists a new order to the database using transactions.
func (c *Consumer) handleOrderPlaced(ctx context.Context, event DomainEvent) error {
	var payload OrderPlacedPayload
	if err := json.Unmarshal(event.Payload.(json.RawMessage), &payload); err != nil {
		return fmt.Errorf("failed to parse order placed payload: %w", err)
	}

	if payload.Order != nil {
		// Use transaction for atomicity
		if err := c.store.WithTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
			return c.store.SaveOrderTx(ctx, tx, payload.Order)
		}); err != nil {
			return fmt.Errorf("failed to save order: %w", err)
		}
		log.Printf("💾 Order saved: ID=%d", payload.Order.ID)
	}
	return nil
}

// handleTradeExecuted persists a trade and updates orders using transactions.
func (c *Consumer) handleTradeExecuted(ctx context.Context, event DomainEvent) error {
	var payload TradeExecutedPayload
	if err := json.Unmarshal(event.Payload.(json.RawMessage), &payload); err != nil {
		return fmt.Errorf("failed to parse trade executed payload: %w", err)
	}

	if payload.Trade != nil {
		// Use transaction for atomic trade and order updates
		if err := c.store.WithTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
			return c.store.SaveTradeTx(ctx, tx, payload.Trade)
		}); err != nil {
			return fmt.Errorf("failed to save trade: %w", err)
		}
		log.Printf("💾 Trade saved: ID=%d, BuyID=%d, SellID=%d, Price=%.2f, Qty=%.4f",
			payload.Trade.ID,
			payload.Trade.BuyOrderID,
			payload.Trade.SellOrderID,
			payload.Trade.Price,
			payload.Trade.Quantity)
	}
	return nil
}

// handleOrderFilled updates order status after full fill.
func (c *Consumer) handleOrderFilled(ctx context.Context, event DomainEvent) error {
	var payload OrderFilledPayload
	if err := json.Unmarshal(event.Payload.(json.RawMessage), &payload); err != nil {
		return fmt.Errorf("failed to parse order filled payload: %w", err)
	}

	log.Printf("📝 Order filled: ID=%d", payload.OrderID)
	return nil
}

// handleOrderCanceled handles canceled orders and persists to database.
func (c *Consumer) handleOrderCanceled(ctx context.Context, event DomainEvent) error {
	var payload OrderCanceledPayload
	if err := json.Unmarshal(event.Payload.(json.RawMessage), &payload); err != nil {
		return fmt.Errorf("failed to parse order canceled payload: %w", err)
	}

	// Persist cancellation to database using transaction
	if err := c.store.WithTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		return c.store.CancelOrderTx(ctx, tx, payload.OrderID)
	}); err != nil {
		return fmt.Errorf("failed to cancel order: %w", err)
	}

	log.Printf("📝 Order canceled: ID=%d, Reason=%s", payload.OrderID, payload.Reason)
	return nil
}

// Stop gracefully shuts down the consumer.
func (c *Consumer) Stop() {
	close(c.done)
	c.wg.Wait()

	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}

	log.Println("📥 Consumer stopped")
}
