package migrations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"taskwiz.app/core/config"
	dbutil "taskwiz.app/core/internal/utils/database"
)

// MigrationRoundTripSuite verifies that the full migration history can be
// applied (MigrateUp to latest) and fully rolled back (MigrateDown to 0), and
// that re-applying the history from a rolled-back state succeeds. This proves
// the Down migrations restore a schema the Up migrations can build on.
//
// It runs on a scratch SQLite database by default, and on PostgreSQL when
// TW_TEST_DSN is set (tables are dropped/recreated around each test).
type MigrationRoundTripSuite struct {
	suite.Suite
	db        *dbutil.DB
	dbFilePath string
}

func TestMigrationRoundTrip(t *testing.T) {
	suite.Run(t, new(MigrationRoundTripSuite))
}

func (s *MigrationRoundTripSuite) SetupTest() {
	var (
		db  *dbutil.DB
		err error
	)

	if dsn := os.Getenv("TW_TEST_DSN"); dsn != "" {
		db, err = dbutil.New(&config.Config{Database: config.DatabaseConfig{Type: "postgres", DSN: dsn}})
		s.Require().NoError(err)

		// Clean slate: drop every schema table the migrations manage.
		for _, table := range []string{
			"notifications",
			"notification_settings",
			"task_histories",
			"task_labels",
			"tasks",
			"labels",
			"app_tokens",
			"user_password_resets",
			"sessions",
			"users",
			"schema_versions",
		} {
			_ = db.RW().Exec("DROP TABLE IF EXISTS " + table + " CASCADE").Error
		}
	} else {
		s.dbFilePath = fmt.Sprintf("%s/migration_roundtrip_%d.db", os.TempDir(), time.Now().UnixNano())
		db, err = dbutil.New(&config.Config{Database: config.DatabaseConfig{FilePath: s.dbFilePath}})
		s.Require().NoError(err)
	}

	s.db = db
}

func (s *MigrationRoundTripSuite) TearDownTest() {
	if s.db != nil {
		_ = s.db.Close()
	}
	if s.dbFilePath != "" {
		_ = os.Remove(s.dbFilePath)
	}
}

func (s *MigrationRoundTripSuite) TestUpThenDownThenUpRoundTrip() {
	ctx := context.Background()
	runner := NewRunner(s.db.RW())

	// Up to latest.
	s.Require().NoError(runner.MigrateUp(ctx, 0))
	version, err := runner.GetCurrentVersion(ctx)
	s.Require().NoError(err)
	s.Equal(GetLatestVersion(), version)

	// Spot-check the final schema: every table from migration 1 (except the
	// dropped ones) plus sessions from migration 7 must exist.
	for _, table := range []string{"users", "tasks", "task_labels", "task_histories", "notification_settings", "notifications", "sessions"} {
		s.True(s.db.RW().Migrator().HasTable(table), "expected table %s to exist after MigrateUp", table)
	}
	for _, table := range []string{"app_tokens", "user_password_resets"} {
		s.False(s.db.RW().Migrator().HasTable(table), "expected table %s to be absent after MigrateUp", table)
	}

	// Down to 0: full rollback.
	s.Require().NoError(runner.MigrateDown(ctx, 0))
	version, err = runner.GetCurrentVersion(ctx)
	s.Require().NoError(err)
	s.Equal(0, version)
	s.False(s.db.RW().Migrator().HasTable("users"), "expected users table to be dropped after MigrateDown to 0")

	// Up again from the rolled-back state: proves the Down migrations
	// restored a base the Up migrations can build on.
	s.Require().NoError(runner.MigrateUp(ctx, 0))
	version, err = runner.GetCurrentVersion(ctx)
	s.Require().NoError(err)
	s.Equal(GetLatestVersion(), version)
	s.True(s.db.RW().Migrator().HasTable("users"))
}
