package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
	"taskwiz.app/core/config"
	"taskwiz.app/core/internal/services/logging"
	"taskwiz.app/core/internal/telemetry"
)

// DB is the connection pool wrapper used across the application.
//
// It supports up to three pools for cloud-native (multi-DSN) setups:
//   - primary (RW): read-write connection. Used for all writes, transactions,
//     migrations, and read-your-writes paths (e.g. session validation).
//   - read (R): ordinary reads. Falls back to the primary pool when
//     database.dsn_r is not configured.
//   - ro (RO): heavy/reporting reads (scheduled scans, joins, aggregations).
//     Falls back to the read pool (which may itself be the primary) when
//     database.dsn_ro is not configured.
//
// For SQLite a single file-backed pool is opened and all three accessors
// return the same pool.
type DB struct {
	primary *gorm.DB
	read    *gorm.DB
	ro      *gorm.DB
}

// New opens the configured connection pool(s) and verifies each one with a
// ping so misconfigured DSNs fail fast at startup.
func New(cfg *config.Config) (*DB, error) {
	dbType := strings.ToLower(cfg.Database.Type)

	switch dbType {
	case "postgres", "postgresql":
		if strings.TrimSpace(cfg.Database.DSN) == "" {
			return nil, fmt.Errorf("database.dsn is required for %s", dbType)
		}

		primary, err := openPostgres(cfg.Database.DSN, cfg.Server.LogLevel)
		if err != nil {
			return nil, fmt.Errorf("failed to open primary (dsn) database: %w", err)
		}

		db := &DB{primary: primary}

		if strings.TrimSpace(cfg.Database.DSNR) != "" {
			read, err := openPostgres(cfg.Database.DSNR, cfg.Server.LogLevel)
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to open read (dsn_r) database: %w", err)
			}
			db.read = read
		} else {
			db.read = primary
		}

		if strings.TrimSpace(cfg.Database.DSNRO) != "" {
			ro, err := openPostgres(cfg.Database.DSNRO, cfg.Server.LogLevel)
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to open read-only (dsn_ro) database: %w", err)
			}
			db.ro = ro
		} else {
			db.ro = db.read
		}

		return db, nil

	case "sqlite", "":
		dialector := sqlite.Open(cfg.Database.FilePath)
		db, err := gorm.Open(dialector, &gorm.Config{Logger: newGormLogger(cfg.Server.LogLevel)})
		if err != nil {
			telemetry.TrackError(context.Background(), "database_open_failed", "database", err, map[string]string{"type": "sqlite"})
			return nil, err
		}

		if err := applySQLitePragmas(db); err != nil {
			_ = closeGorm(db)
			return nil, err
		}

		return &DB{primary: db, read: db, ro: db}, nil

	default:
		return nil, fmt.Errorf("unsupported database type: %s (supported: postgres, sqlite)", cfg.Database.Type)
	}
}

// RW returns the read-write (primary) pool. Use for all writes,
// transactions, migrations, and read-your-writes reads.
func (d *DB) RW() *gorm.DB {
	return d.primary
}

// R returns the ordinary-read pool. Falls back to the primary pool when
// database.dsn_r is not configured.
func (d *DB) R() *gorm.DB {
	return d.read
}

// RO returns the heavy/reporting-read pool. Falls back to the read pool
// (which may itself be the primary) when database.dsn_ro is not configured.
func (d *DB) RO() *gorm.DB {
	return d.ro
}

// Close closes all distinct underlying connection pools.
func (d *DB) Close() error {
	closed := make(map[*sql.DB]struct{})
	var firstErr error

	for _, db := range []*gorm.DB{d.primary, d.read, d.ro} {
		if db == nil {
			continue
		}
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		if _, ok := closed[sqlDB]; ok {
			continue
		}
		closed[sqlDB] = struct{}{}
		if err := sqlDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func openPostgres(dsn, logLevel string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: newGormLogger(logLevel)})
	if err != nil {
		telemetry.TrackError(context.Background(), "database_open_failed", "database", err, map[string]string{"type": "postgres"})
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		telemetry.TrackError(context.Background(), "database_open_failed", "database", err, map[string]string{"type": "postgres"})
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		telemetry.TrackError(context.Background(), "database_ping_failed", "database", err, map[string]string{"type": "postgres"})
		return nil, fmt.Errorf("ping: %w", err)
	}

	return db, nil
}

func applySQLitePragmas(db *gorm.DB) error {
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys = ON;",
	} {
		if err := db.Exec(pragma).Error; err != nil {
			return fmt.Errorf("failed to apply %s: %w", strings.TrimSpace(pragma), err)
		}
	}
	return nil
}

func closeGorm(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func newGormLogger(appLogLevel string) gormLogger.Interface {
	var level gormLogger.LogLevel
	switch strings.ToLower(appLogLevel) {
	case "debug":
		level = gormLogger.Info
		logging.DefaultLogger().Error("DEBUG level set: SQL queries will be logged and may contain sensitive data")
	case "warn", "warning":
		level = gormLogger.Warn
	case "error":
		level = gormLogger.Error
	case "silent":
		level = gormLogger.Silent
	default:
		level = gormLogger.Warn
	}

	return gormLogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormLogger.Config{
			SlowThreshold: time.Second,
			LogLevel:      level,
			Colorful:      false,
		},
	)
}
