package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func init() {
	Register(&SessionsMigration{})
}

type SessionsMigration struct{}

func (m *SessionsMigration) Version() int {
	return 7
}

func (m *SessionsMigration) Name() string {
	return "sessions"
}

func (m *SessionsMigration) Up(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)

	var createTable string
	switch db.Name() {
	case "sqlite":
		createTable = `CREATE TABLE sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`
	case "postgres":
		createTable = `CREATE TABLE sessions (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL,
			token_hash VARCHAR(64) NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT fk_users_sessions FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`
	default:
		return fmt.Errorf("unsupported dialect: %s", db.Name())
	}

	stmts := []string{
		createTable,
		`CREATE UNIQUE INDEX idx_sessions_token_hash ON sessions(token_hash)`,
		`CREATE INDEX idx_sessions_user_id ON sessions(user_id)`,
		`CREATE INDEX idx_sessions_expires_at ON sessions(expires_at)`,
	}
	for _, stmt := range stmts {
		if err := dbCtx.Exec(stmt).Error; err != nil {
			return err
		}
	}

	return nil
}

func (m *SessionsMigration) Down(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec("DROP TABLE IF EXISTS sessions").Error
}
