package models

import (
	"errors"
	"time"
)

type Trade struct {
	ID          int64     `json:"id"`
	BuyOrderID  int64     `json:"buy_order_id"`
	SellOrderID int64     `json:"sell_order_id"`
	Price       float64   `json:"price"`
	Quantity    float64   `json:"quantity"`
	CreatedAt   time.Time `json:"created_at"`
}

func (t *Trade) Validate() error {
	if t.BuyOrderID <= 0 {
		return errors.New("buy_order_id must be greater than 0")
	}
	if t.SellOrderID <= 0 {
		return errors.New("sell_order_id must be greater than 0")
	}
	if t.BuyOrderID == t.SellOrderID {
		return errors.New("buy_order_id and sell_order_id must be different")
	}
	if t.Price <= 0 {
		return errors.New("price must be greater than 0")
	}
	if t.Quantity <= 0 {
		return errors.New("quantity must be greater than 0")
	}
	return nil
}
