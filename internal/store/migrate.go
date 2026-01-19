package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/lib/pq"
)

type Migration struct {
	Version int
	Name    string
	SQL     string
}

type Migrator struct {
	db *sql.DB
}

func NewMigrator(dsn string) (*Migrator, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Migrator{db: db}, nil
}

func (m *Migrator) Close() error {
	return m.db.Close()
}

func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)
	`
	_, err := m.db.ExecContext(ctx, query)
	return err
}

func (m *Migrator) getAppliedMigrations(ctx context.Context) (map[int]bool, error) {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return nil, err
	}

	rows, err := m.db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}

	return applied, rows.Err()
}

func (m *Migrator) markMigrationApplied(ctx context.Context, version int, name string) error {
	query := `
		INSERT INTO schema_migrations (version, name)
		VALUES ($1, $2)
		ON CONFLICT (version) DO NOTHING
	`
	_, err := m.db.ExecContext(ctx, query, version, name)
	return err
}

func (m *Migrator) LoadMigrationsFromDir(dir string) ([]Migration, error) {
	var migrations []Migration

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", path, err)
		}

		baseName := filepath.Base(path)
		var version int
		if _, err := fmt.Sscanf(baseName, "%d_", &version); err != nil {
			return fmt.Errorf("invalid migration file name format: %s (expected: NNN_name.sql)", baseName)
		}

		name := strings.TrimSuffix(baseName, ".sql")
		if idx := strings.Index(name, "_"); idx > 0 {
			name = name[idx+1:]
		}

		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			SQL:     string(content),
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func (m *Migrator) Run(ctx context.Context, migrations []Migration) error {
	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}

		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute migration %d_%s: %w", migration.Version, migration.Name, err)
		}

		if err := m.markMigrationApplied(ctx, migration.Version, migration.Name); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to mark migration as applied: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration: %w", err)
		}
	}

	return nil
}

func (m *Migrator) Migrate(ctx context.Context, migrationsDir string) error {
	migrations, err := m.LoadMigrationsFromDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	return m.Run(ctx, migrations)
}
