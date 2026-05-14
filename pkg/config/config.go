package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

type TunnelConfig struct {
	Label     string `mapstructure:"label" yaml:"label"`
	LocalAddr string `mapstructure:"local_addr" yaml:"local_addr"`
}

type AgentConfig struct {
	GatewayAddr       string         `mapstructure:"gateway_addr"`
	Token             string         `mapstructure:"token"`
	LocalAddr         string         `mapstructure:"local_addr"`
	Tunnels           []TunnelConfig `mapstructure:"tunnels"`
	ReconnectDelay    time.Duration  `mapstructure:"reconnect_delay"`
	MaxReconnect      int            `mapstructure:"max_reconnect"`
	HeartbeatInterval time.Duration  `mapstructure:"heartbeat_interval"`
	QUICTimeout       time.Duration  `mapstructure:"quic_timeout"`
	TLSCAFile         string         `mapstructure:"tls_ca_file"`
	TLSInsecure       bool           `mapstructure:"tls_insecure"`
	APIURL            string         `mapstructure:"api_url"`
	StreamIdleTimeout time.Duration  `mapstructure:"stream_idle_timeout"`
}

type GatewayConfig struct {
	QUICListenAddr    string        `mapstructure:"quic_listen_addr"`
	PublicListenAddr  string        `mapstructure:"public_listen_addr"`
	BaseDomain        string        `mapstructure:"base_domain"`
	RegistryAddr      string        `mapstructure:"registry_addr"`
	TLSCertFile       string        `mapstructure:"tls_cert_file"`
	TLSKeyFile        string        `mapstructure:"tls_key_file"`
	ShutdownTimeout   time.Duration `mapstructure:"shutdown_timeout"`
	MaxStreams        int64         `mapstructure:"max_streams"`
	GRPCAuthToken     string        `mapstructure:"grpc_auth_token"`
	StreamIdleTimeout time.Duration `mapstructure:"stream_idle_timeout"`
}

type RegistryConfig struct {
	GRPCListenAddr  string        `mapstructure:"grpc_listen_addr"`
	SessionTTL      time.Duration `mapstructure:"session_ttl"`
	CleanupInterval time.Duration `mapstructure:"cleanup_interval"`
	GRPCAuthToken   string        `mapstructure:"grpc_auth_token"`
}

type DashboardConfig struct {
	HTTPListenAddr string        `mapstructure:"http_listen_addr"`
	JWTSecret      string        `mapstructure:"jwt_secret"`
	JWTTTL         time.Duration `mapstructure:"jwt_ttl"`
	BaseURL        string        `mapstructure:"base_url"`
	SMTPHost       string        `mapstructure:"smtp_host"`
	SMTPPort       int           `mapstructure:"smtp_port"`
	SMTPFrom       string        `mapstructure:"smtp_from"`
}

type LogConfig struct {
	Level  string `mapstructure:"log_level"`
	Format string `mapstructure:"log_format"`
}

type ObservabilityConfig struct {
	MetricsEnabled  bool   `mapstructure:"metrics_enabled"`
	MetricsAddr     string `mapstructure:"metrics_addr"`
	TracingEnabled  bool   `mapstructure:"tracing_enabled"`
	TracingEndpoint string `mapstructure:"tracing_endpoint"`
}

type DBConfig struct {
	Driver          string        `mapstructure:"db_driver"`
	DSN             string        `mapstructure:"db_dsn"`
	MaxOpenConns    int           `mapstructure:"db_max_open_conns"`
	MaxIdleConns    int           `mapstructure:"db_max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"db_conn_max_lifetime"`
	AutoMigrate     bool          `mapstructure:"db_auto_migrate"`
}

type Config struct {
	ServiceName   string              `mapstructure:"service_name"`
	Log           LogConfig           `mapstructure:",squash"`
	Observability ObservabilityConfig `mapstructure:",squash"`
	Agent         AgentConfig         `mapstructure:",squash"`
	Gateway       GatewayConfig       `mapstructure:",squash"`
	Registry      RegistryConfig      `mapstructure:",squash"`
	Dashboard     DashboardConfig     `mapstructure:",squash"`
	DB            DBConfig            `mapstructure:",squash"`
}

type ServiceType string

const (
	ServiceAgent     ServiceType = "agent"
	ServiceGateway   ServiceType = "gateway"
	ServiceRegistry  ServiceType = "registry"
	ServiceDashboard ServiceType = "dashboard"
)

func defaults(svc ServiceType) {
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "json")

	viper.SetDefault("metrics_enabled", true)
	viper.SetDefault("metrics_addr", ":9090")
	viper.SetDefault("tracing_enabled", false)
	viper.SetDefault("tracing_endpoint", "localhost:4317")

	viper.SetDefault("db_driver", "memory")
	viper.SetDefault("db_dsn", "")
	viper.SetDefault("db_max_open_conns", 10)
	viper.SetDefault("db_max_idle_conns", 5)
	viper.SetDefault("db_conn_max_lifetime", 5*time.Minute)
	viper.SetDefault("db_auto_migrate", true)

	switch svc {
	case ServiceAgent:
		viper.SetDefault("gateway_addr", "localhost:4433")
		viper.SetDefault("reconnect_delay", 2*time.Second)
		viper.SetDefault("max_reconnect", 0)
		viper.SetDefault("heartbeat_interval", 15*time.Second)
		viper.SetDefault("quic_timeout", 30*time.Second)
		viper.SetDefault("tls_insecure", true)
	case ServiceGateway:
		viper.SetDefault("quic_listen_addr", ":4433")
		viper.SetDefault("public_listen_addr", ":443")
		viper.SetDefault("base_domain", "tunneledge.dev")
		viper.SetDefault("registry_addr", "localhost:50051")
		viper.SetDefault("shutdown_timeout", 15*time.Second)
		viper.SetDefault("max_streams", int64(1000))
		viper.SetDefault("grpc_auth_token", "")
	case ServiceRegistry:
		viper.SetDefault("grpc_listen_addr", ":50051")
		viper.SetDefault("session_ttl", 5*time.Minute)
		viper.SetDefault("cleanup_interval", 30*time.Second)
		viper.SetDefault("grpc_auth_token", "")
	case ServiceDashboard:
		viper.SetDefault("http_listen_addr", ":8080")
		viper.SetDefault("jwt_secret", "")
		viper.SetDefault("jwt_ttl", 24*time.Hour)
		viper.SetDefault("base_url", "http://localhost:8080")
		viper.SetDefault("smtp_host", "localhost")
		viper.SetDefault("smtp_port", 1025)
		viper.SetDefault("smtp_from", "noreply@tunneledge.dev")
	}
}

func Load(svc ServiceType, opts ...Option) (*Config, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	viper.SetConfigName(o.configName)
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/etc/tunneledge")

	viper.SetEnvPrefix("TE")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	defaults(svc)

	if o.configPath != "" {
		viper.SetConfigFile(o.configPath)
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	cfg.ServiceName = string(svc)

	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

type SaveConfig struct {
	LogLevel          string         `yaml:"log_level"`
	LogFormat         string         `yaml:"log_format"`
	MetricsEnabled    bool           `yaml:"metrics_enabled"`
	MetricsAddr       string         `yaml:"metrics_addr"`
	GatewayAddr       string         `yaml:"gateway_addr"`
	Token             string         `yaml:"token"`
	LocalAddr         string         `yaml:"local_addr,omitempty"`
	Tunnels           []TunnelConfig `yaml:"tunnels,omitempty"`
	ReconnectDelay    string         `yaml:"reconnect_delay"`
	MaxReconnect      int            `yaml:"max_reconnect"`
	HeartbeatInterval string         `yaml:"heartbeat_interval"`
	QUICTimeout       string         `yaml:"quic_timeout"`
}

func Save(cfg *Config, path string) error {
	sc := SaveConfig{
		LogLevel:          cfg.Log.Level,
		LogFormat:         cfg.Log.Format,
		MetricsEnabled:    cfg.Observability.MetricsEnabled,
		MetricsAddr:       cfg.Observability.MetricsAddr,
		GatewayAddr:       cfg.Agent.GatewayAddr,
		Token:             cfg.Agent.Token,
		LocalAddr:         cfg.Agent.LocalAddr,
		Tunnels:           cfg.Agent.Tunnels,
		ReconnectDelay:    cfg.Agent.ReconnectDelay.String(),
		MaxReconnect:      cfg.Agent.MaxReconnect,
		HeartbeatInterval: cfg.Agent.HeartbeatInterval.String(),
		QUICTimeout:       cfg.Agent.QUICTimeout.String(),
	}

	data, err := yaml.Marshal(sc)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
