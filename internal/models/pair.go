package models

import (
	"errors"
	"strings"
)

type Pair struct {
	Base  string `json:"base"`
	Quote string `json:"quote"`
}

func (p *Pair) Validate() error {
	if strings.TrimSpace(p.Base) == "" {
		return errors.New("base currency is required")
	}
	if strings.TrimSpace(p.Quote) == "" {
		return errors.New("quote currency is required")
	}
	if p.Base == p.Quote {
		return errors.New("base and quote currencies must be different")
	}
	if len(p.Base) > 10 || len(p.Quote) > 10 {
		return errors.New("currency codes must be 10 characters or less")
	}
	return nil
}

func (p *Pair) String() string {
	return p.Base + "/" + p.Quote
}
