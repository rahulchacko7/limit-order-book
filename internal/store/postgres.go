package store

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/lib/pq"

	"limit-order-book/internal/models"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) SaveOrder(ctx context.Context, o *models.Order) error {
	query := `
		INSERT INTO orders (user_id, pair, side, price, quantity, filled, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id
	`

	return s.db.QueryRowContext(
		ctx,
		query,
		o.UserID,
		o.Pair,
		o.Side,
		o.Price,
		o.Quantity,
		o.Filled,
		o.Status,
	).Scan(&o.ID)
}

func (s *PostgresStore) UpdateOrder(ctx context.Context, o *models.Order) error {
	query := `
		UPDATE orders
		SET filled = $1, status = $2
		WHERE id = $3
	`
	_, err := s.db.ExecContext(ctx, query, o.Filled, o.Status, o.ID)
	return err
}

func (s *PostgresStore) SaveTrade(ctx context.Context, t *models.Trade) error {
	query := `
		INSERT INTO trades (buy_order_id, sell_order_id, price, quantity)
		VALUES ($1,$2,$3,$4)
	`
	_, err := s.db.ExecContext(
		ctx,
		query,
		t.BuyOrderID,
		t.SellOrderID,
		t.Price,
		t.Quantity,
	)
	return err
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) GetDB() *sql.DB {
	return s.db
}
