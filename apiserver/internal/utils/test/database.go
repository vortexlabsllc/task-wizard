package test

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/stretchr/testify/suite"
	"gorm.io/gorm"
	"taskwiz.app/core/config"
	"taskwiz.app/core/internal/migrations"
	dbutil "taskwiz.app/core/internal/utils/database"
)

// DatabaseTestSuite is the base suite for all repo/service tests.
//
// By default it creates a fresh SQLite file database per test (fast, no
// service required). When the TW_TEST_DSN environment variable is set to a
// PostgreSQL DSN, the suite instead runs against Postgres (tables are dropped
// and re-migrated around each test), enabling the same test suites to verify
// the Postgres dialect.
type DatabaseTestSuite struct {
	suite.Suite
	// DB is the primary (read-write) *gorm.DB, for direct test assertions.
	DB     *gorm.DB
	// DBPool is the full pool wrapper handed to repositories under test.
	DBPool *dbutil.DB

	dbFilePath   string
	isPostgres   bool
}

// TWTestDSN is the environment variable that switches the suite to Postgres.
const TWTestDSN = "TW_TEST_DSN"

func (suite *DatabaseTestSuite) SetupTest() {
	suite.isPostgres = false

	if dsn := os.Getenv(TWTestDSN); dsn != "" {
		cfg := &config.Config{Database: config.DatabaseConfig{Type: "postgres", DSN: dsn}}
		dbPool, err := dbutil.New(cfg)
		suite.Require().NoError(err)

		// Start from a clean schema for each test.
		suite.dropAllPostgresTables(dbPool.RW())

		runner := migrations.NewRunner(dbPool.RW())
		err = runner.MigrateUp(context.Background(), 0)
		suite.Require().NoError(err)

		suite.DBPool = dbPool
		suite.DB = dbPool.RW()
		suite.isPostgres = true
		return
	}

	suite.dbFilePath = fmt.Sprintf("%s/testdb_%d.db", os.TempDir(), time.Now().UnixNano())
	cfg := &config.Config{Database: config.DatabaseConfig{FilePath: suite.dbFilePath}}
	dbPool, err := dbutil.New(cfg)
	suite.Require().NoError(err)

	runner := migrations.NewRunner(dbPool.RW())
	err = runner.MigrateUp(context.Background(), 0)
	suite.Require().NoError(err)

	suite.DBPool = dbPool
	suite.DB = dbPool.RW()
}

func (suite *DatabaseTestSuite) TearDownTest() {
	// Close the database connections
	for _, pool := range []*gorm.DB{suite.DBPool.RW(), suite.DBPool.R(), suite.DBPool.RO()} {
		db, err := pool.DB()
		if err != nil {
			log.Printf("failed to get database connection: %v", err)
			continue
		}
		_ = db.Close()
	}

	// Remove the temporary database file created for the test (SQLite only)
	if suite.dbFilePath != "" {
		if err := os.Remove(suite.dbFilePath); err != nil && !os.IsNotExist(err) {
			suite.Require().NoError(err, fmt.Sprintf("failed to remove db file %s", suite.dbFilePath))
		}
	}
}

// dropAllPostgresTables drops the schema tables (plus migration bookkeeping)
// in dependency order so each test starts from a clean database.
func (suite *DatabaseTestSuite) dropAllPostgresTables(db *gorm.DB) {
	tables := []string{
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
	}
	for _, table := range tables {
		_ = db.Exec("DROP TABLE IF EXISTS " + table + " CASCADE").Error
	}
}
