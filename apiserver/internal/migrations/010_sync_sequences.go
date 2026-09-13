package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func init() {
	Register(&SyncSequencesMigration{})
}

// SyncSequencesMigration repairs PostgreSQL identity/serial sequences that have
// fallen behind the maximum value in their table. When a sequence lags, new
// inserts draw ids that already exist and fail with a duplicate key violation
// on the primary key (e.g. "task_histories_pkey"). This can happen when rows
// are inserted without going through the sequence or when data is restored
// into an existing database.
type SyncSequencesMigration struct{}

func (m *SyncSequencesMigration) Version() int {
	return 10
}

func (m *SyncSequencesMigration) Name() string {
	return "sync_sequences"
}

func (m *SyncSequencesMigration) Up(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)

	switch db.Name() {
	case "postgres":
		// For every owned sequence, advance it past the current maximum of
		// its column so future nextval() calls cannot collide with existing rows.
		stmt := `
			SELECT format('SELECT setval(%L, COALESCE(MAX(%I), 1), MAX(%I) IS NOT NULL)',
				quote_nspconcat(schemaname) || '.' || sequencename,
				column_name, column_name)
			FROM pg_sequences
			WHERE schemaname NOT IN ('pg_catalog', 'information_schema')`

		var setvals []string
		if err := dbCtx.Raw(stmt).Scan(&setvals).Error; err != nil {
			return fmt.Errorf("failed to enumerate sequences: %w", err)
		}

		for _, q := range setvals {
			if err := dbCtx.Exec(q).Error; err != nil {
				return fmt.Errorf("failed to sync sequence: %w", err)
			}
		}
		return nil

	case "sqlite":
		// SQLite uses INTEGER PRIMARY KEY AUTOINCREMENT backed by
		// sqlite_sequence; keep it in sync for parity.
		var tables []string
		if err := dbCtx.Raw(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables).Error; err != nil {
			return fmt.Errorf("failed to enumerate tables: %w", err)
		}

		for _, table := range tables {
			// Best effort: only tables with an autoincrement "id" column matter.
			q := fmt.Sprintf(
				`INSERT OR REPLACE INTO sqlite_sequence (name, seq) SELECT '%s', COALESCE(MAX(id), 0) FROM %s`,
				table, table,
			)
			_ = dbCtx.Exec(q).Error
		}
		return nil

	default:
		return fmt.Errorf("unsupported dialect: %s", db.Name())
	}
}

func (m *SyncSequencesMigration) Down(ctx context.Context, db *gorm.DB) error {
	// Nothing to roll back: advancing a sequence is safe and monotonic.
	return nil
}
