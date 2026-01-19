package messaging

import (
	"encoding/json"
	"log"

	"github.com/streadway/amqp"
)

// Publisher is responsible for publishing domain events
// (e.g., OrderPlaced, TradeExecuted) to RabbitMQ.
//
// MESSAGING STRATEGY (Optional but Scalable):
// Events are published asynchronously and consumed by:
//   - Notifications: Email/push notifications to users
//   - Analytics: Real-time trade volume, order book depth
//   - Persistence: Database writes (event consumers)
//   - WebSockets: Real-time price updates to clients
//
// WHY EVENT-DRIVEN?
// This is how real exchanges scale - the matching engine is purely computational,
// and all I/O happens through async message passing.
//
// EVENTS PUBLISHED:
//   - order.placed: When an order enters the book
//   - trade.executed: When a match occurs (includes trade details)
//   - order.filled: When an order is fully filled and removed from book
type Publisher struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	exchange string
}

// NewPublisher initializes a RabbitMQ publisher with the given exchange.
func NewPublisher(amqpURL, exchange string) (*Publisher, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	// Declare a topic exchange for flexible routing
	// Topic exchanges allow patterns like: trade.executed.*, order.*
	err = ch.ExchangeDeclare(
		exchange,
		"topic", // topic exchange for flexibility
		true,    // durable
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}

	return &Publisher{
		conn:     conn,
		channel:  ch,
		exchange: exchange,
	}, nil
}

// Publish sends an event message to RabbitMQ with the given routing key.
func (p *Publisher) Publish(routingKey string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	err = p.channel.Publish(
		p.exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		return err
	}

	log.Printf("📤 Event published: %s", routingKey)
	return nil
}

// Close shuts down RabbitMQ resources gracefully.
func (p *Publisher) Close() {
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
}
