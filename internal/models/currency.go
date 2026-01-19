package models

import (
	"errors"
	"strings"
	"time"
)

type Currency struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Precision int       `json:"precision"`
	MinAmount float64   `json:"min_amount"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewCurrency(code, name string, precision int, minAmount float64) *Currency {
	now := time.Now()
	return &Currency{
		Code:      code,
		Name:      name,
		Precision: precision,
		MinAmount: minAmount,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (c *Currency) Validate() error {
	if strings.TrimSpace(c.Code) == "" {
		return errors.New("currency code is required")
	}
	if len(c.Code) > 10 {
		return errors.New("currency code must be 10 characters or less")
	}
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("currency name is required")
	}
	if c.Precision < 0 || c.Precision > 18 {
		return errors.New("precision must be between 0 and 18")
	}
	if c.MinAmount < 0 {
		return errors.New("min_amount cannot be negative")
	}
	return nil
}

var DefaultCurrencies = []*Currency{
	NewCurrency("USD", "US Dollar", 2, 0.01),
	NewCurrency("BTC", "Bitcoin", 8, 0.00000001),
	NewCurrency("ETH", "Ethereum", 18, 0.000000000000000001),
	NewCurrency("EUR", "Euro", 2, 0.01),
	NewCurrency("GBP", "British Pound", 2, 0.01),
	NewCurrency("JPY", "Japanese Yen", 0, 1.0),
	NewCurrency("USDT", "Tether", 2, 0.01),
	NewCurrency("BNB", "Binance Coin", 8, 0.00000001),
}
