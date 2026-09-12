package database

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"
	"gorm.io/gorm"
	"taskwiz.app/core/config"
)

type DatabaseTestSuite struct {
	suite.Suite
	db *DB
}

func TestDatabaseTestSuite(t *testing.T) {
	suite.Run(t, new(DatabaseTestSuite))
}

func (s *DatabaseTestSuite) SetupTest() {
	// Mock configuration: in-memory SQLite
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			FilePath: ":memory:",
		},
	}

	db, err := New(cfg)
	s.Require().NoError(err)
	s.Require().NotNil(db)
	s.db = db
}

func (s *DatabaseTestSuite) TearDownTest() {
	if s.db != nil {
		_ = s.db.Close()
	}
}

func (s *DatabaseTestSuite) TestNewDatabaseConnection() {
	sqlDB, err := s.db.RW().DB()
	s.Require().NoError(err)
	s.NoError(sqlDB.Ping())
}

func (s *DatabaseTestSuite) TestSQLite_SinglePoolForAllAccessors() {
	// SQLite has no replica split: all accessors must return the same pool.
	s.Same(s.db.RW(), s.db.R())
	s.Same(s.db.RW(), s.db.RO())
}

func (s *DatabaseTestSuite) TestPostgres_MissingDSN() {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "postgres",
		},
	}

	_, err := New(cfg)
	s.Require().Error(err)
	s.Contains(err.Error(), "database.dsn is required")
}

func (s *DatabaseTestSuite) TestPostgres_UnreachableDSN_FailsFast() {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "postgres",
			DSN:  "postgres://user:pass@127.0.0.1:59999/nodb?sslmode=disable&connect_timeout=1",
		},
	}

	_, err := New(cfg)
	s.Require().Error(err)
	s.Contains(err.Error(), "failed to open primary (dsn) database")
}

// TestPostgres_FallbackChains verifies the R/RO fallback behavior against a
// live Postgres instance. It is skipped unless TW_TEST_DSN is set, since it
// needs a reachable database.
func (s *DatabaseTestSuite) TestPostgres_FallbackChains() {
	liveDSN := os.Getenv("TW_TEST_DSN")
	if liveDSN == "" {
		s.T().Skip("TW_TEST_DSN not set; skipping live Postgres fallback tests")
	}

	tcases := []struct {
		name      string
		dsnR      string
		dsnRO     string
		wantR     func(d *DB) bool
		wantRO    func(d *DB) bool
		wantPools int // number of distinct underlying pools
	}{
		{
			name:      "single_dsn_all_pools_share_primary",
			dsnR:      "",
			dsnRO:     "",
			wantR:     func(d *DB) bool { return d.read == d.primary },
			wantRO:    func(d *DB) bool { return d.ro == d.primary },
			wantPools: 1,
		},
		{
			name:      "r_set_ro_falls_back_to_r",
			dsnR:      liveDSN,
			dsnRO:     "",
			wantR:     func(d *DB) bool { return d.read != d.primary },
			wantRO:    func(d *DB) bool { return d.ro == d.read },
			wantPools: 2,
		},
		{
			name:      "all_set_three_pools",
			dsnR:      liveDSN,
			dsnRO:     liveDSN,
			wantR:     func(d *DB) bool { return d.read != d.primary },
			wantRO:    func(d *DB) bool { return d.ro != d.primary && d.ro != d.read },
			wantPools: 3,
		},
	}

	for _, tc := range tcases {
		s.Run(tc.name, func() {
			cfg := &config.Config{
				Database: config.DatabaseConfig{
					Type:  "postgres",
					DSN:   liveDSN,
					DSNR:  tc.dsnR,
					DSNRO: tc.dsnRO,
				},
			}
			db, err := New(cfg)
			s.Require().NoError(err)
			defer func() { _ = db.Close() }()

			s.True(tc.wantR(db), "read pool routing mismatch")
			s.True(tc.wantRO(db), "ro pool routing mismatch")
			s.Equal(tc.wantPools, distinctPools(db))
		})
	}
}

// distinctPools counts the distinct underlying *sql.DB pools in use.
func distinctPools(d *DB) int {
	seen := make(map[*sql.DB]struct{})
	for _, db := range []*gorm.DB{d.primary, d.read, d.ro} {
		sqlDB, err := db.DB()
		if err != nil {
			continue
		}
		seen[sqlDB] = struct{}{}
	}
	return len(seen)
}
