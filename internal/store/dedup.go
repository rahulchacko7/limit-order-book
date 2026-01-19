package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

type DedupStore struct {
	db          *sql.DB
	cleanupDone chan struct{}
	mu          sync.RWMutex
}

type DedupConfig struct {
	MessageTTL      time.Duration // How long to keep message records
	CleanupInterval time.Duration // How often to run cleanup
}

func DefaultDedupConfig() *DedupConfig {
	return &DedupConfig{
		MessageTTL:      7 * 24 * time.Hour, // 7 days
		CleanupInterval: 1 * time.Hour,      // Every hour
	}
}

func NewDedupStore(db *sql.DB, config *DedupConfig) *DedupStore {
	if config == nil {
		config = DefaultDedupConfig()
	}

	store := &DedupStore{
		db:          db,
		cleanupDone: make(chan struct{}),
	}

	go store.startCleanup(config.CleanupInterval)

	return store
}

func (s *DedupStore) Stop() {
	close(s.cleanupDone)
}

func (s *DedupStore) startCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.cleanupDone:
			log.Println("🧹 DedupStore cleanup stopped")
			return
		case <-ticker.C:
			count, err := s.CleanupExpired(context.Background())
			if err != nil {
				log.Printf("⚠️ Failed to cleanup expired messages: %v", err)
			} else if count > 0 {
				log.Printf("🧹 Cleaned up %d expired message records", count)
			}
		}
	}
}

func (s *DedupStore) IsProcessed(ctx context.Context, messageID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM processed_messages
			WHERE message_id = $1 AND expires_at > NOW()
		)
	`
	var exists bool
	err := s.db.QueryRowContext(ctx, query, messageID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check message processed status: %w", err)
	}
	return exists, nil
}

func (s *DedupStore) MarkProcessed(ctx context.Context, messageID, eventType string) error {
	query := `
		INSERT INTO processed_messages (message_id, event_type, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '7 days')
		ON CONFLICT (message_id) DO NOTHING
	`
	_, err := s.db.ExecContext(ctx, query, messageID, eventType)
	if err != nil {
		return fmt.Errorf("failed to mark message as processed: %w", err)
	}
	return nil
}

func (s *DedupStore) MarkProcessedWithTx(ctx context.Context, tx *sql.Tx, messageID, eventType string) error {
	query := `
		INSERT INTO processed_messages (message_id, event_type, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '7 days')
		ON CONFLICT (message_id) DO NOTHING
	`
	_, err := tx.ExecContext(ctx, query, messageID, eventType)
	if err != nil {
		return fmt.Errorf("failed to mark message as processed: %w", err)
	}
	return nil
}

func (s *DedupStore) TryProcess(ctx context.Context, messageID, eventType string) (bool, error) {
	processed, err := s.IsProcessed(ctx, messageID)
	if err != nil {
		return false, err
	}
	if processed {
		return false, nil
	}

	if err := s.MarkProcessed(ctx, messageID, eventType); err != nil {
		processed, checkErr := s.IsProcessed(ctx, messageID)
		if checkErr != nil {
			return false, fmt.Errorf("deduplication check failed after insert error: %w", checkErr)
		}
		if processed {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (s *DedupStore) CleanupExpired(ctx context.Context) (int, error) {
	query := `
		DELETE FROM processed_messages WHERE expires_at < NOW()
	`
	result, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired messages: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

func (s *DedupStore) GetStats(ctx context.Context) (*DedupStats, error) {
	stats := &DedupStats{}

	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM processed_messages
	`).Scan(&stats.TotalCount)
	if err != nil {
		return nil, err
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM processed_messages WHERE expires_at < NOW()
	`).Scan(&stats.ExpiredCount)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT event_type, COUNT(*) as count
		FROM processed_messages
		GROUP BY event_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var eventType string
		var count int
		if err := rows.Scan(&eventType, &count); err != nil {
			return nil, err
		}
		stats.ByEventType = append(stats.ByEventType, EventTypeCount{
			EventType: eventType,
			Count:     count,
		})
	}

	return stats, nil
}

type DedupStats struct {
	TotalCount   int              `json:"total_count"`
	ExpiredCount int              `json:"expired_count"`
	ByEventType  []EventTypeCount `json:"by_event_type"`
}

type EventTypeCount struct {
	EventType string `json:"event_type"`
	Count     int    `json:"count"`
}

type DedupMiddleware struct {
	store *DedupStore
}

func NewDedupMiddleware(store *DedupStore) *DedupMiddleware {
	return &DedupMiddleware{store: store}
}

func (m *DedupMiddleware) Process(ctx context.Context, messageID, eventType string, handler func() error) error {
	processed, err := m.store.TryProcess(ctx, messageID, eventType)
	if err != nil {
		return fmt.Errorf("deduplication check failed: %w", err)
	}

	if !processed {
		log.Printf("⏭️ Message already processed, skipping: %s", messageID)
		return nil
	}

	return handler()
}
