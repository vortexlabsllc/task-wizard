package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func init() {
	Register(&DropPIIMigration{})
}

type DropPIIMigration struct{}

func (m *DropPIIMigration) Version() int {
	return 5
}

func (m *DropPIIMigration) Name() string {
	return "drop_pii"
}

func (m *DropPIIMigration) Up(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)
	dialect := db.Name()

	switch dialect {
	case "sqlite":
		// SQLite cannot drop columns in older versions reliably, so rebuild
		// the table without the PII columns.
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
	case "postgres":
		for _, col := range []string{"email", "display_name"} {
			if err := dbCtx.Exec("ALTER TABLE users DROP COLUMN " + col).Error; err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported dialect: %s", dialect)
	}

	return nil
}

func (m *DropPIIMigration) Down(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)
	dialect := db.Name()

	switch dialect {
	case "sqlite":
		for _, stmt := range []string{
			"ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''",
		} {
			if err := dbCtx.Exec(stmt).Error; err != nil {
				return err
			}
		}
	case "postgres":
		for _, stmt := range []string{
			"ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''",
		} {
			if err := dbCtx.Exec(stmt).Error; err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported dialect: %s", dialect)
	}

	return nil
}
