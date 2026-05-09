package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type AgentConfig struct {
	GatewayAddr       string        `mapstructure:"gateway_addr"`
	Token             string        `mapstructure:"token"`
	LocalAddr         string        `mapstructure:"local_addr"`
	ReconnectDelay    time.Duration `mapstructure:"reconnect_delay"`
	MaxReconnect      int           `mapstructure:"max_reconnect"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	QUICTimeout       time.Duration `mapstructure:"quic_timeout"`
}

type GatewayConfig struct {
	QUICListenAddr   string        `mapstructure:"quic_listen_addr"`
	PublicListenAddr string        `mapstructure:"public_listen_addr"`
	BaseDomain       string        `mapstructure:"base_domain"`
	RegistryAddr     string        `mapstructure:"registry_addr"`
	TLSCertFile      string        `mapstructure:"tls_cert_file"`
	TLSKeyFile       string        `mapstructure:"tls_key_file"`
	ShutdownTimeout  time.Duration `mapstructure:"shutdown_timeout"`
	MaxStreams       int64         `mapstructure:"max_streams"`
}

type RegistryConfig struct {
	GRPCListenAddr  string        `mapstructure:"grpc_listen_addr"`
	SessionTTL      time.Duration `mapstructure:"session_ttl"`
	CleanupInterval time.Duration `mapstructure:"cleanup_interval"`
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

type Config struct {
	ServiceName   string              `mapstructure:"service_name"`
	Log           LogConfig           `mapstructure:",squash"`
	Observability ObservabilityConfig `mapstructure:",squash"`
	Agent         AgentConfig         `mapstructure:",squash"`
	Gateway       GatewayConfig       `mapstructure:",squash"`
	Registry      RegistryConfig      `mapstructure:",squash"`
}

type ServiceType string

const (
	ServiceAgent    ServiceType = "agent"
	ServiceGateway  ServiceType = "gateway"
	ServiceRegistry ServiceType = "registry"
)

func defaults(svc ServiceType) {
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "json")

	viper.SetDefault("metrics_enabled", true)
	viper.SetDefault("metrics_addr", ":9090")
	viper.SetDefault("tracing_enabled", false)
	viper.SetDefault("tracing_endpoint", "localhost:4317")

	switch svc {
	case ServiceAgent:
		viper.SetDefault("gateway_addr", "localhost:4433")
		viper.SetDefault("reconnect_delay", 2*time.Second)
		viper.SetDefault("max_reconnect", 0)
		viper.SetDefault("heartbeat_interval", 15*time.Second)
		viper.SetDefault("quic_timeout", 30*time.Second)
	case ServiceGateway:
		viper.SetDefault("quic_listen_addr", ":4433")
		viper.SetDefault("public_listen_addr", ":443")
		viper.SetDefault("base_domain", "tunneledge.dev")
		viper.SetDefault("registry_addr", "localhost:50051")
		viper.SetDefault("shutdown_timeout", 15*time.Second)
		viper.SetDefault("max_streams", int64(1000))
	case ServiceRegistry:
		viper.SetDefault("grpc_listen_addr", ":50051")
		viper.SetDefault("session_ttl", 5*time.Minute)
		viper.SetDefault("cleanup_interval", 30*time.Second)
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
