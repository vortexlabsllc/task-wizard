package migrations

import (
	"context"

	"gorm.io/gorm"
)

func init() {
	Register(&DropPasswordMigration{})
}

type DropPasswordMigration struct{}

func (m *DropPasswordMigration) Version() int {
	return 3
}

func (m *DropPasswordMigration) Name() string {
	return "drop_password"
}

func (m *DropPasswordMigration) Up(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)
	migrator := dbCtx.Migrator()

	if migrator.HasColumn("users", "password") {
		if err := dbCtx.Exec("ALTER TABLE users DROP COLUMN password").Error; err != nil {
			return err
		}
	}

	return nil
}

func (m *DropPasswordMigration) Down(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)
	migrator := dbCtx.Migrator()

	if !migrator.HasColumn("users", "password") {
		if err := dbCtx.Exec("ALTER TABLE users ADD COLUMN password TEXT NOT NULL DEFAULT ''").Error; err != nil {
			return err
		}
	}

	return nil
}
