package main

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"go.uber.org/fx"
	"taskwiz.app/core/config"
	database "taskwiz.app/core/internal/utils/database"
	"taskwiz.app/core/internal/services/scheduler"
)

type mockLifecycle struct{}

func (t *mockLifecycle) Append(hook fx.Hook) {}

func newTestDB(t *testing.T) *database.DB {
	t.Helper()
	cfg := &config.Config{Database: config.DatabaseConfig{FilePath: ":memory:"}}
	db, err := database.New(cfg)
	assert.NoError(t, err)
	return db
}

func TestNewServer_DebugMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			LogLevel:     "debug",
			Port:         8080,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		},
		Database: config.DatabaseConfig{},
	}
	db := newTestDB(t)
	ms := &scheduler.Scheduler{}
	lc := &mockLifecycle{}
	r := newServer(lc, cfg, db, ms)
	assert.NotNil(t, r)
	assert.IsType(t, &gin.Engine{}, r)
}

func TestNewServer_ReleaseMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			LogLevel:     "info",
			Port:         8080,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		},
		Database: config.DatabaseConfig{},
	}
	db := newTestDB(t)
	ms := &scheduler.Scheduler{}
	lc := &mockLifecycle{}
	r := newServer(lc, cfg, db, ms)
	assert.NotNil(t, r)
	assert.IsType(t, &gin.Engine{}, r)
}

func TestTimeoutOrDefault(t *testing.T) {
	tests := []struct {
		name       string
		configured time.Duration
		fallback   time.Duration
		expected   time.Duration
	}{
		{
			name:       "zero_configured_uses_fallback",
			configured: 0,
			fallback:   5 * time.Second,
			expected:   5 * time.Second,
		},
		{
			name:       "negative_configured_uses_fallback",
			configured: -1 * time.Second,
			fallback:   5 * time.Second,
			expected:   5 * time.Second,
		},
		{
			name:       "positive_configured_uses_configured",
			configured: 10 * time.Second,
			fallback:   5 * time.Second,
			expected:   10 * time.Second,
		},
		{
			name:       "one_nanosecond_configured_uses_configured",
			configured: 1 * time.Nanosecond,
			fallback:   5 * time.Second,
			expected:   1 * time.Nanosecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := timeoutOrDefault(tt.configured, tt.fallback)
			assert.Equal(t, tt.expected, result)
		})
	}
}
