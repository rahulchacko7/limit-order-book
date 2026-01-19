package store

import (
	"context"
	"database/sql"
	"fmt"

	"limit-order-book/internal/models"
)

type TxFunc func(ctx context.Context, tx *sql.Tx) error

func (s *PostgresStore) WithTransaction(ctx context.Context, fn TxFunc) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx failed: %v, rollback failed: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *PostgresStore) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
	})
}

func (s *PostgresStore) SaveOrderTx(ctx context.Context, tx *sql.Tx, o *models.Order) error {
	query := `
		INSERT INTO orders (user_id, pair, side, price, quantity, filled, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id
	`

	return tx.QueryRowContext(
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

func (s *PostgresStore) UpdateOrderTx(ctx context.Context, tx *sql.Tx, o *models.Order) error {
	query := `
		UPDATE orders
		SET filled = $1, status = $2
		WHERE id = $3
	`
	_, err := tx.ExecContext(ctx, query, o.Filled, o.Status, o.ID)
	return err
}

func (s *PostgresStore) SaveTradeTx(ctx context.Context, tx *sql.Tx, t *models.Trade) error {
	query := `
		INSERT INTO trades (buy_order_id, sell_order_id, price, quantity)
		VALUES ($1,$2,$3,$4)
		RETURNING id
	`
	return tx.QueryRowContext(
		ctx,
		query,
		t.BuyOrderID,
		t.SellOrderID,
		t.Price,
		t.Quantity,
	).Scan(&t.ID)
}

func (s *PostgresStore) OrderWithTradeTx(ctx context.Context, order *models.Order, trade *models.Trade) error {
	return s.WithTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := s.SaveOrderTx(ctx, tx, order); err != nil {
			return fmt.Errorf("failed to save order: %w", err)
		}

		if trade != nil {
			trade.BuyOrderID = order.ID
			if err := s.SaveTradeTx(ctx, tx, trade); err != nil {
				return fmt.Errorf("failed to save trade: %w", err)
			}
		}

		return nil
	})
}

func (s *PostgresStore) ProcessTradeWithOrdersTx(ctx context.Context, trade *models.Trade, buyOrder, sellOrder *models.Order) error {
	return s.WithTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := s.SaveTradeTx(ctx, tx, trade); err != nil {
			return fmt.Errorf("failed to save trade: %w", err)
		}

		if err := s.UpdateOrderTx(ctx, tx, buyOrder); err != nil {
			return fmt.Errorf("failed to update buy order: %w", err)
		}

		if err := s.UpdateOrderTx(ctx, tx, sellOrder); err != nil {
			return fmt.Errorf("failed to update sell order: %w", err)
		}

		return nil
	})
}

func (s *PostgresStore) UpdateOrderStatusTx(ctx context.Context, tx *sql.Tx, orderID int64, status models.Status) error {
	query := `
		UPDATE orders
		SET status = $1
		WHERE id = $2
	`
	_, err := tx.ExecContext(ctx, query, status, orderID)
	return err
}

func (s *PostgresStore) UpdateOrderFilledTx(ctx context.Context, tx *sql.Tx, orderID int64, filled float64, status models.Status) error {
	query := `
		UPDATE orders
		SET filled = $1, status = $2
		WHERE id = $3
	`
	_, err := tx.ExecContext(ctx, query, filled, status, orderID)
	return err
}

func (s *PostgresStore) ProcessTradeWithOrderUpdatesTx(ctx context.Context, trade *models.Trade, buyOrder, sellOrder *models.Order) error {
	return s.WithTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := s.SaveTradeTx(ctx, tx, trade); err != nil {
			return fmt.Errorf("failed to save trade: %w", err)
		}

		if err := s.UpdateOrderFilledTx(ctx, tx, buyOrder.ID, buyOrder.Filled, buyOrder.Status); err != nil {
			return fmt.Errorf("failed to update buy order: %w", err)
		}

		if err := s.UpdateOrderFilledTx(ctx, tx, sellOrder.ID, sellOrder.Filled, sellOrder.Status); err != nil {
			return fmt.Errorf("failed to update sell order: %w", err)
		}

		return nil
	})
}

func (s *PostgresStore) CancelOrderTx(ctx context.Context, tx *sql.Tx, orderID int64) error {
	return s.UpdateOrderStatusTx(ctx, tx, orderID, models.Cancelled)
}
