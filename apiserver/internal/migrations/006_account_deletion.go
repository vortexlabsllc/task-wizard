package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func init() {
	Register(&AccountDeletionMigration{})
}

type AccountDeletionMigration struct{}

func (m *AccountDeletionMigration) Version() int {
	return 6
}

func (m *AccountDeletionMigration) Name() string {
	return "account_deletion"
}

func (m *AccountDeletionMigration) Up(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)

	var stmt string
	switch db.Name() {
	case "sqlite":
		stmt = "ALTER TABLE users ADD COLUMN deletion_requested_at DATETIME DEFAULT NULL"
	case "postgres":
		stmt = "ALTER TABLE users ADD COLUMN deletion_requested_at TIMESTAMPTZ DEFAULT NULL"
	default:
		return fmt.Errorf("unsupported dialect: %s", db.Name())
	}

	return dbCtx.Exec(stmt).Error
}

func (m *AccountDeletionMigration) Down(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)
	dialect := db.Name()

	switch dialect {
	case "sqlite":
		stmts := []string{
			`CREATE TABLE users_new (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				directory_id TEXT NOT NULL DEFAULT '',
				object_id TEXT NOT NULL DEFAULT '',
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT NULL,
				disabled BOOLEAN DEFAULT false
			)`,
			`INSERT INTO users_new (id, directory_id, object_id, created_at, updated_at, disabled)
				SELECT id, directory_id, object_id, created_at, updated_at, disabled FROM users`,
			`DROP TABLE users`,
			`ALTER TABLE users_new RENAME TO users`,
		}
		for _, stmt := range stmts {
			if err := dbCtx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	case "postgres":
		return dbCtx.Exec("ALTER TABLE users DROP COLUMN deletion_requested_at").Error
	default:
		return fmt.Errorf("unsupported dialect: %s", dialect)
	}
}
