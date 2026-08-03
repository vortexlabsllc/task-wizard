package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Database      DatabaseConfig  `mapstructure:"database" yaml:"database"`
	Entra         EntraConfig     `mapstructure:"entra" yaml:"entra"`
	Server        ServerConfig    `mapstructure:"server" yaml:"server"`
	SchedulerJobs SchedulerConfig `mapstructure:"scheduler_jobs" yaml:"scheduler_jobs"`
}

type DatabaseConfig struct {
	Type      string `mapstructure:"type" yaml:"type" default:"sqlite"`
	FilePath  string `mapstructure:"path" yaml:"path" default:"/config/task-wizard.db"`
	Host      string `mapstructure:"host" yaml:"host"`
	Port      int    `mapstructure:"port" yaml:"port" default:"3306"`
	Database  string `mapstructure:"database" yaml:"database"`
	Username  string `mapstructure:"username" yaml:"username"`
	Password  string `mapstructure:"password" yaml:"password"`
	Migration bool   `mapstructure:"migration" yaml:"migration"`
}

type EntraConfig struct {
	Enabled  bool   `mapstructure:"enabled" yaml:"enabled"`
	TenantID string `mapstructure:"tenant_id" yaml:"tenant_id"`
	ClientID string `mapstructure:"client_id" yaml:"client_id"`
	Audience string `mapstructure:"audience" yaml:"audience"`
	Issuer   string `mapstructure:"issuer" yaml:"issuer"`
}

func (c EntraConfig) AuthorityTenantID() string {
	tenantID := strings.TrimSpace(c.TenantID)
	if tenantID == "" {
		return "common"
	}

	return tenantID
}

type ServerConfig struct {
	HostName             string        `mapstructure:"host_name" yaml:"host_name"`
	Port                 int           `mapstructure:"port" yaml:"port"`
	RatePeriod           time.Duration `mapstructure:"rate_period" yaml:"rate_period"`
	RateLimit            int           `mapstructure:"rate_limit" yaml:"rate_limit"`
	ReadTimeout          time.Duration `mapstructure:"read_timeout" yaml:"read_timeout"`
	WriteTimeout         time.Duration `mapstructure:"write_timeout" yaml:"write_timeout"`
	ServeFrontend        bool          `mapstructure:"serve_frontend" yaml:"serve_frontend"`
	Registration         bool          `mapstructure:"registration" yaml:"registration"`
	LogLevel             string        `mapstructure:"log_level" yaml:"log_level"`
	AllowedOrigins       []string      `mapstructure:"allowed_origins" yaml:"allowed_origins"`
	AllowCorsCredentials bool          `mapstructure:"allow_cors_credentials" yaml:"allow_cors_credentials"`
	SessionDuration      time.Duration `mapstructure:"session_duration" yaml:"session_duration" default:"720h"`
	AllowInsecureNoAuth  bool          `mapstructure:"allow_insecure_no_auth" yaml:"allow_insecure_no_auth"`
	TrustedProxies       []string      `mapstructure:"trusted_proxies" yaml:"trusted_proxies"`
}

type SchedulerConfig struct {
	DueFrequency             time.Duration `mapstructure:"due_frequency" yaml:"due_frequency" default:"5m"`
	OverdueFrequency         time.Duration `mapstructure:"overdue_frequency" yaml:"overdue_frequency" default:"1d"`
	NotificationCleanup      time.Duration `mapstructure:"notification_cleanup" yaml:"notification_cleanup" default:"10m"`
	AccountDeletionFrequency time.Duration `mapstructure:"account_deletion_frequency" yaml:"account_deletion_frequency" default:"15m"`
}

func LoadConfig(configFile string) *Config {
	viper.SetConfigType("yaml")

	if configFile == "" {
		if envFile := os.Getenv("TW_CONFIG_FILE"); envFile != "" {
			configFile = envFile
		}
	}

	if configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		viper.SetConfigName("config")
		viper.AddConfigPath(".")
		viper.AddConfigPath("./config")
	}

	_ = viper.BindEnv("entra.enabled", "TW_ENTRA_ENABLED")
	_ = viper.BindEnv("entra.tenant_id", "TW_ENTRA_TENANT_ID")
	_ = viper.BindEnv("entra.client_id", "TW_ENTRA_CLIENT_ID")
	_ = viper.BindEnv("entra.audience", "TW_ENTRA_AUDIENCE")
	_ = viper.BindEnv("entra.issuer", "TW_ENTRA_ISSUER")
	_ = viper.BindEnv("server.session_duration", "TW_SESSION_DURATION")
	_ = viper.BindEnv("server.allow_insecure_no_auth", "TW_ALLOW_INSECURE_NO_AUTH")
	_ = viper.BindEnv("database.type", "TW_DATABASE_TYPE")
	_ = viper.BindEnv("database.host", "TW_DATABASE_HOST")
	_ = viper.BindEnv("database.port", "TW_DATABASE_PORT")
	_ = viper.BindEnv("database.database", "TW_DATABASE_NAME")
	_ = viper.BindEnv("database.username", "TW_DATABASE_USERNAME")
	_ = viper.BindEnv("database.password", "TW_DATABASE_PASSWORD")

	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}

	var config Config
	err = viper.Unmarshal(&config)
	if err != nil {
		panic(err)
	}

	return &config
}

// ValidateCorsConfig rejects insecure CORS configurations. Allowing credentials
// together with a wildcard origin would let any website make credentialed
// cross-origin requests and read the responses, so the server must refuse to
// start in that case.
func ValidateCorsConfig(cfg *Config) error {
	if !cfg.Server.AllowCorsCredentials {
		return nil
	}

	for _, origin := range cfg.Server.AllowedOrigins {
		if origin == "*" {
			return fmt.Errorf("insecure CORS configuration: allow_cors_credentials cannot be enabled together with a wildcard ('*') entry in allowed_origins; specify explicit origins instead")
		}
	}

	return nil
}

// ParseTrustedProxies converts the configured trusted proxy entries into
// network ranges. Each entry may be a CIDR (e.g. "10.0.0.0/8") or a bare IP
// address (e.g. "192.168.1.1"), which is treated as a single-host range. An
// empty configuration yields no trusted ranges, meaning no proxy is trusted.
func ParseTrustedProxies(entries []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		if _, ipNet, err := net.ParseCIDR(entry); err == nil {
			nets = append(nets, ipNet)
			continue
		}

		ip := net.ParseIP(entry)
		if ip == nil {
			return nil, fmt.Errorf("invalid trusted proxy entry: %q", entry)
		}

		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}

		bits := len(ip) * 8
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}

	return nets, nil
}
