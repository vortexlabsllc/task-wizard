package config

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_Success(t *testing.T) {
	_ = os.MkdirAll("./config", 0755)
	f, err := os.Create("./config/config.yaml")
	assert.NoError(t, err, "failed to create config.yaml")

	defer func() { _ = os.Remove("./config/config.yaml") }()
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(`server:
  port: 1234
  log_level: debug
  registration: true
  serve_frontend: false
  read_timeout: 10s
  write_timeout: 10s
database:
  type: sqlite
  path: test.db
  migration: true
entra:
  enabled: true
  tenant_id: test-tenant
  client_id: test-client
  audience: api://test-client
scheduler_jobs:
  due_frequency: 5m
  overdue_frequency: 24h
`)

	assert.NoError(t, err)

	viper.Reset()
	cfg := LoadConfig("./config/config.yaml")
	assert.Equal(t, 1234, cfg.Server.Port)
	assert.Equal(t, "debug", cfg.Server.LogLevel)
	assert.Equal(t, true, cfg.Server.Registration)
	assert.Equal(t, false, cfg.Server.ServeFrontend)
	assert.Equal(t, 10*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 10*time.Second, cfg.Server.WriteTimeout)

	assert.Equal(t, "sqlite", cfg.Database.Type)
	assert.Equal(t, "test.db", cfg.Database.FilePath)
	assert.Equal(t, true, cfg.Database.Migration)

	assert.Equal(t, true, cfg.Entra.Enabled)
	assert.Equal(t, "test-tenant", cfg.Entra.TenantID)
	assert.Equal(t, "test-client", cfg.Entra.ClientID)
	assert.Equal(t, "api://test-client", cfg.Entra.Audience)

	assert.Equal(t, 5*time.Minute, cfg.SchedulerJobs.DueFrequency)
	assert.Equal(t, 24*time.Hour, cfg.SchedulerJobs.OverdueFrequency)
}

func TestParseTrustedProxies(t *testing.T) {
	nets, err := ParseTrustedProxies([]string{"10.0.0.0/8", "192.168.1.1", " ", "::1"})
	assert.NoError(t, err)
	assert.Len(t, nets, 3)

	assert.True(t, nets[0].Contains(net.ParseIP("10.1.2.3")))
	assert.False(t, nets[0].Contains(net.ParseIP("11.0.0.1")))
	assert.True(t, nets[1].Contains(net.ParseIP("192.168.1.1")))
	assert.False(t, nets[1].Contains(net.ParseIP("192.168.1.2")))
	assert.True(t, nets[2].Contains(net.ParseIP("::1")))
}

func TestEntraConfigAuthorityTenantID(t *testing.T) {
	assert.Equal(t, "common", (EntraConfig{}).AuthorityTenantID())
	assert.Equal(t, "common", (EntraConfig{TenantID: "  "}).AuthorityTenantID())
	assert.Equal(t, "tenant-id", (EntraConfig{TenantID: "tenant-id"}).AuthorityTenantID())
}

func TestParseTrustedProxies_Empty(t *testing.T) {
	nets, err := ParseTrustedProxies(nil)
	assert.NoError(t, err)
	assert.Empty(t, nets)
}

func TestParseTrustedProxies_Invalid(t *testing.T) {
	_, err := ParseTrustedProxies([]string{"not-an-ip"})
	assert.Error(t, err)
}

func TestLoadConfig_PanicOnMissingFile(t *testing.T) {
	// Temporarily rename the real config.yaml so viper can't find any config
	renamed := false
	if _, err := os.Stat("./config.yaml"); err == nil {
		err = os.Rename("./config.yaml", "./config.yaml.bak")
		assert.NoError(t, err)
		renamed = true
	}

	_ = os.Remove("./config/config.yaml")
	viper.Reset()

	defer func() {
		if renamed {
			_ = os.Rename("./config.yaml.bak", "./config.yaml")
		}
	}()

	assert.Panics(t, func() {
		_ = LoadConfig("")
	})
}

func TestLoadConfig_EntraEnvOverride(t *testing.T) {
	_ = os.MkdirAll("./config", 0755)
	f, err := os.Create("./config/config.yaml")
	assert.NoError(t, err)
	defer func() { _ = os.Remove("./config/config.yaml") }()
	defer func() { _ = f.Close() }()

	_, err = f.WriteString(`entra:
  enabled: false
  tenant_id: file-tenant
  client_id: file-client
  audience: api://file-client
server:
  port: 1234
`)
	assert.NoError(t, err)

	_ = os.Setenv("TW_ENTRA_ENABLED", "true")
	_ = os.Setenv("TW_ENTRA_TENANT_ID", "env-tenant")
	_ = os.Setenv("TW_ENTRA_CLIENT_ID", "env-client")
	_ = os.Setenv("TW_ENTRA_AUDIENCE", "api://env-client")

	viper.Reset()
	cfg := LoadConfig("./config/config.yaml")

	assert.Equal(t, true, cfg.Entra.Enabled)
	assert.Equal(t, "env-tenant", cfg.Entra.TenantID)
	assert.Equal(t, "env-client", cfg.Entra.ClientID)
	assert.Equal(t, "api://env-client", cfg.Entra.Audience)

	_ = os.Unsetenv("TW_ENTRA_ENABLED")
	_ = os.Unsetenv("TW_ENTRA_TENANT_ID")
	_ = os.Unsetenv("TW_ENTRA_CLIENT_ID")
	_ = os.Unsetenv("TW_ENTRA_AUDIENCE")
}

func TestLoadConfig_EnvFile(t *testing.T) {
	f, err := os.Create("envconfig.yaml")
	assert.NoError(t, err)
	defer func() { _ = os.Remove("envconfig.yaml") }()
	defer func() { _ = f.Close() }()

	_, err = f.WriteString("server:\n  port: 4444\n")
	assert.NoError(t, err)

	_ = os.Setenv("TW_CONFIG_FILE", "envconfig.yaml")
	viper.Reset()
	cfg := LoadConfig("")
	assert.Equal(t, 4444, cfg.Server.Port)
	_ = os.Unsetenv("TW_CONFIG_FILE")
}

func TestLoadConfig_CLIOverridesEnv(t *testing.T) {
	f1, err := os.Create("env.yaml")
	assert.NoError(t, err)
	defer func() { _ = os.Remove("env.yaml") }()
	defer func() { _ = f1.Close() }()
	_, err = f1.WriteString("server:\n  port: 3333\n")
	assert.NoError(t, err)

	f2, err := os.Create("cli.yaml")
	assert.NoError(t, err)
	defer func() { _ = os.Remove("cli.yaml") }()
	defer func() { _ = f2.Close() }()
	_, err = f2.WriteString("server:\n  port: 2222\n")
	assert.NoError(t, err)

	_ = os.Setenv("TW_CONFIG_FILE", "env.yaml")
	viper.Reset()
	cfg := LoadConfig("cli.yaml")
	assert.Equal(t, 2222, cfg.Server.Port)
	_ = os.Unsetenv("TW_CONFIG_FILE")
}

func TestLoadConfig_DatabaseEnvOverride(t *testing.T) {
	_ = os.MkdirAll("./config", 0755)
	f, err := os.Create("./config/config.yaml")
	assert.NoError(t, err)
	defer func() { _ = os.Remove("./config/config.yaml") }()
	defer func() { _ = f.Close() }()

	_, err = f.WriteString(`database:
  type: sqlite
  path: /config/task-wizard.db
server:
  port: 1234
`)
	assert.NoError(t, err)

	_ = os.Setenv("TW_DATABASE_TYPE", "mysql")
	_ = os.Setenv("TW_DATABASE_HOST", "localhost")
	_ = os.Setenv("TW_DATABASE_PORT", "3307")
	_ = os.Setenv("TW_DATABASE_NAME", "taskwizard")
	_ = os.Setenv("TW_DATABASE_USERNAME", "dbuser")
	_ = os.Setenv("TW_DATABASE_PASSWORD", "dbpass")

	viper.Reset()
	cfg := LoadConfig("./config/config.yaml")

	assert.Equal(t, "mysql", cfg.Database.Type)
	assert.Equal(t, "localhost", cfg.Database.Host)
	assert.Equal(t, 3307, cfg.Database.Port)
	assert.Equal(t, "taskwizard", cfg.Database.Database)
	assert.Equal(t, "dbuser", cfg.Database.Username)
	assert.Equal(t, "dbpass", cfg.Database.Password)

	_ = os.Unsetenv("TW_DATABASE_TYPE")
	_ = os.Unsetenv("TW_DATABASE_HOST")
	_ = os.Unsetenv("TW_DATABASE_PORT")
	_ = os.Unsetenv("TW_DATABASE_NAME")
	_ = os.Unsetenv("TW_DATABASE_USERNAME")
	_ = os.Unsetenv("TW_DATABASE_PASSWORD")
}

func TestLoadConfig_MySQLConfig(t *testing.T) {
	_ = os.MkdirAll("./config", 0755)
	f, err := os.Create("./config/config.yaml")
	assert.NoError(t, err)
	defer func() { _ = os.Remove("./config/config.yaml") }()
	defer func() { _ = f.Close() }()

	_, err = f.WriteString(`database:
  type: mysql
  host: mysql.example.com
  port: 3306
  database: taskwizard
  username: testuser
  password: testpass
  migration: true
server:
  port: 1234
`)
	assert.NoError(t, err)

	viper.Reset()
	cfg := LoadConfig("./config/config.yaml")

	assert.Equal(t, "mysql", cfg.Database.Type)
	assert.Equal(t, "mysql.example.com", cfg.Database.Host)
	assert.Equal(t, 3306, cfg.Database.Port)
	assert.Equal(t, "taskwizard", cfg.Database.Database)
	assert.Equal(t, "testuser", cfg.Database.Username)
	assert.Equal(t, "testpass", cfg.Database.Password)
	assert.Equal(t, true, cfg.Database.Migration)
}

func TestValidateCorsConfig(t *testing.T) {
	cases := []struct {
		name        string
		origins     []string
		credentials bool
		wantErr     bool
	}{
		{
			name:        "wildcard with credentials is rejected",
			origins:     []string{"*"},
			credentials: true,
			wantErr:     true,
		},
		{
			name:        "wildcard among explicit origins with credentials is rejected",
			origins:     []string{"https://app.example.com", "*"},
			credentials: true,
			wantErr:     true,
		},
		{
			name:        "wildcard without credentials is allowed",
			origins:     []string{"*"},
			credentials: false,
			wantErr:     false,
		},
		{
			name:        "explicit origins with credentials is allowed",
			origins:     []string{"https://app.example.com"},
			credentials: true,
			wantErr:     false,
		},
		{
			name:        "empty list is allowed",
			origins:     nil,
			credentials: true,
			wantErr:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					AllowedOrigins:       tc.origins,
					AllowCorsCredentials: tc.credentials,
				},
			}

			err := ValidateCorsConfig(cfg)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
