package models

import (
	"errors"
	"time"
)

type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

type Status string

const (
	Open      Status = "open"
	Partial   Status = "partial"
	Filled    Status = "filled"
	Cancelled Status = "cancelled"
)

type Order struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Pair      string    `json:"pair"`
	Side      Side      `json:"side"`
	Price     float64   `json:"price"`
	Quantity  float64   `json:"quantity"`
	Filled    float64   `json:"filled"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (o *Order) Validate() error {
	if o.UserID <= 0 {
		return errors.New("user_id must be greater than 0")
	}
	if o.Pair == "" {
		return errors.New("pair is required")
	}
	if o.Side != Buy && o.Side != Sell {
		return errors.New("side must be 'buy' or 'sell'")
	}
	if o.Price <= 0 {
		return errors.New("price must be greater than 0")
	}
	if o.Quantity <= 0 {
		return errors.New("quantity must be greater than 0")
	}
	if o.Filled < 0 {
		return errors.New("filled quantity cannot be negative")
	}
	if o.Filled > o.Quantity {
		return errors.New("filled quantity cannot exceed total quantity")
	}
	if o.Status != Open && o.Status != Partial && o.Status != Filled && o.Status != Cancelled {
		return errors.New("invalid status")
	}
	return nil
}

func (s Side) IsValid() bool {
	return s == Buy || s == Sell
}

func (st Status) IsValid() bool {
	return st == Open || st == Partial || st == Filled || st == Cancelled
}
