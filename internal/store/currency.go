package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"limit-order-book/internal/models"
)

type CurrencyStore struct {
	db *sql.DB
}

func NewCurrencyStore(db *sql.DB) *CurrencyStore {
	return &CurrencyStore{db: db}
}

func (s *CurrencyStore) Create(ctx context.Context, c *models.Currency) error {
	query := `
		INSERT INTO currencies (code, name, precision, min_amount, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := s.db.ExecContext(
		ctx,
		query,
		c.Code,
		c.Name,
		c.Precision,
		c.MinAmount,
		c.IsActive,
		c.CreatedAt,
		c.UpdatedAt,
	)
	return err
}

func (s *CurrencyStore) GetByCode(ctx context.Context, code string) (*models.Currency, error) {
	query := `
		SELECT code, name, precision, min_amount, is_active, created_at, updated_at
		FROM currencies
		WHERE code = $1
	`

	var c models.Currency
	err := s.db.QueryRowContext(ctx, query, code).Scan(
		&c.Code,
		&c.Name,
		&c.Precision,
		&c.MinAmount,
		&c.IsActive,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *CurrencyStore) List(ctx context.Context, includeInactive bool) ([]*models.Currency, error) {
	var query string
	var args []interface{}

	if includeInactive {
		query = `
			SELECT code, name, precision, min_amount, is_active, created_at, updated_at
			FROM currencies
			ORDER BY code ASC
		`
	} else {
		query = `
			SELECT code, name, precision, min_amount, is_active, created_at, updated_at
			FROM currencies
			WHERE is_active = true
			ORDER BY code ASC
		`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var currencies []*models.Currency
	for rows.Next() {
		var c models.Currency
		if err := rows.Scan(
			&c.Code,
			&c.Name,
			&c.Precision,
			&c.MinAmount,
			&c.IsActive,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		currencies = append(currencies, &c)
	}

	return currencies, rows.Err()
}

func (s *CurrencyStore) Update(ctx context.Context, c *models.Currency) error {
	query := `
		UPDATE currencies
		SET name = $1, precision = $2, min_amount = $3, is_active = $4, updated_at = $5
		WHERE code = $6
	`

	c.UpdatedAt = time.Now()
	_, err := s.db.ExecContext(
		ctx,
		query,
		c.Name,
		c.Precision,
		c.MinAmount,
		c.IsActive,
		c.UpdatedAt,
		c.Code,
	)
	return err
}

func (s *CurrencyStore) Delete(ctx context.Context, code string) error {
	query := `
		UPDATE currencies
		SET is_active = false, updated_at = $1
		WHERE code = $2
	`

	_, err := s.db.ExecContext(ctx, query, time.Now(), code)
	return err
}

func (s *CurrencyStore) HardDelete(ctx context.Context, code string) error {
	query := `DELETE FROM currencies WHERE code = $1`
	_, err := s.db.ExecContext(ctx, query, code)
	return err
}

func (s *CurrencyStore) Exists(ctx context.Context, code string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM currencies WHERE code = $1)`
	var exists bool
	err := s.db.QueryRowContext(ctx, query, code).Scan(&exists)
	return exists, err
}

func (s *CurrencyStore) SeedDefaultCurrencies(ctx context.Context, defaults []*models.Currency) (int, error) {
	seeded := 0
	for _, c := range defaults {
		exists, err := s.Exists(ctx, c.Code)
		if err != nil {
			return seeded, fmt.Errorf("checking currency %s: %w", c.Code, err)
		}
		if !exists {
			if err := s.Create(ctx, c); err != nil {
				return seeded, fmt.Errorf("creating currency %s: %w", c.Code, err)
			}
			seeded++
		}
	}
	return seeded, nil
}
