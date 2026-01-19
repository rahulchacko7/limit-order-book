package store

import (
	"context"
	"database/sql"
	"fmt"

	"limit-order-book/internal/models"
)

type PairStore struct {
	db *sql.DB
}

func NewPairStore(db *sql.DB) *PairStore {
	return &PairStore{db: db}
}

func (s *PairStore) Create(ctx context.Context, pair *models.Pair) error {
	query := `INSERT INTO currency_pairs (base, quote) VALUES ($1, $2)`
	_, err := s.db.ExecContext(ctx, query, pair.Base, pair.Quote)
	return err
}

func (s *PairStore) GetByBaseQuote(ctx context.Context, base, quote string) (*models.Pair, error) {
	query := `SELECT base, quote FROM currency_pairs WHERE base = $1 AND quote = $2`
	var pair models.Pair
	err := s.db.QueryRowContext(ctx, query, base, quote).Scan(&pair.Base, &pair.Quote)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pair, nil
}

func (s *PairStore) List(ctx context.Context) ([]*models.Pair, error) {
	query := `SELECT base, quote FROM currency_pairs ORDER BY base, quote`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []*models.Pair
	for rows.Next() {
		var pair models.Pair
		if err := rows.Scan(&pair.Base, &pair.Quote); err != nil {
			return nil, err
		}
		pairs = append(pairs, &pair)
	}
	return pairs, rows.Err()
}

func (s *PairStore) Exists(ctx context.Context, base, quote string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM currency_pairs WHERE base = $1 AND quote = $2)`
	var exists bool
	err := s.db.QueryRowContext(ctx, query, base, quote).Scan(&exists)
	return exists, err
}

func (s *PairStore) SeedDefaultPairs(ctx context.Context, defaults []*models.Pair) (int, error) {
	seeded := 0
	for _, p := range defaults {
		exists, err := s.Exists(ctx, p.Base, p.Quote)
		if err != nil {
			return seeded, fmt.Errorf("checking pair %s/%s: %w", p.Base, p.Quote, err)
		}
		if !exists {
			if err := s.Create(ctx, p); err != nil {
				return seeded, fmt.Errorf("creating pair %s/%s: %w", p.Base, p.Quote, err)
			}
			seeded++
		}
	}
	return seeded, nil
}
