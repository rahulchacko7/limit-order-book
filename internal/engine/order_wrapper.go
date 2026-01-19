package engine

import (
	"time"

	"limit-order-book/internal/models"
)

// OrderWrapper wraps a domain Order with engine-specific behavior.
// The timestamp field is critical for price-time priority ordering.
// Using nanosecond precision ensures accurate FIFO ordering at same price.
type OrderWrapper struct {
	*models.Order
	timestamp int64 // Nanosecond timestamp for FIFO ordering
}

// NewOrderWrapper creates a new wrapped order with a timestamp.
// Timestamp is captured at creation to ensure deterministic ordering.
func NewOrderWrapper(o *models.Order) *OrderWrapper {
	return &OrderWrapper{
		Order:     o,
		timestamp: time.Now().UnixNano(),
	}
}

// Remaining returns the unfilled quantity of the order.
func (o *OrderWrapper) Remaining() float64 {
	return o.Quantity - o.Filled
}

// Fill updates the order's filled quantity and status.
// Handles both partial fills and complete fills.
// EDGE CASE: Floating-point precision - in production, use int64 with smallest unit
func (o *OrderWrapper) Fill(qty float64) {
	o.Filled += qty
	if o.Filled >= o.Quantity {
		o.Status = models.Filled
	} else {
		o.Status = models.Partial
	}
}
