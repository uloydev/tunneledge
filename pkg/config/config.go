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
	Label      string `mapstructure:"label" yaml:"label"`
	LocalAddr  string `mapstructure:"local_addr" yaml:"local_addr"`
	TunnelType string `mapstructure:"tunnel_type" yaml:"tunnel_type"`
}

type AgentConfig struct {
	GatewayAddr       string         `mapstructure:"gateway_addr"`
	GatewayAddrs      []string       `mapstructure:"gateway_addrs"`
	Token             string         `mapstructure:"token"`
	LocalAddr         string         `mapstructure:"local_addr"`
	Tunnels           []TunnelConfig `mapstructure:"tunnels"`
	ReconnectDelay    time.Duration  `mapstructure:"reconnect_delay"`
	MaxReconnect      int            `mapstructure:"max_reconnect"`
	HeartbeatInterval time.Duration  `mapstructure:"heartbeat_interval"`
	QUICTimeout       time.Duration  `mapstructure:"quic_timeout"`
	TLSCAFile         string         `mapstructure:"tls_ca_file"`
	TLSInsecure       bool           `mapstructure:"tls_insecure"`
	// mTLS: present a client certificate to the gateway.
	MTLSEnabled       bool          `mapstructure:"mtls_enabled"`
	ClientCertFile    string        `mapstructure:"client_cert_file"`
	ClientKeyFile     string        `mapstructure:"client_key_file"`
	APIURL            string        `mapstructure:"api_url"`
	StreamIdleTimeout time.Duration `mapstructure:"stream_idle_timeout"`
	// Phase 4: region routing and session resume.
	PreferredRegion      string `mapstructure:"preferred_region"`
	SessionResumeEnabled bool   `mapstructure:"session_resume_enabled"`
}

type GatewayConfig struct {
	QUICListenAddr   string `mapstructure:"quic_listen_addr"`
	PublicListenAddr string `mapstructure:"public_listen_addr"`
	BaseDomain       string `mapstructure:"base_domain"`
	RegistryAddr     string `mapstructure:"registry_addr"`
	RelayID          string `mapstructure:"relay_id"`
	AdvertiseAddr    string `mapstructure:"advertise_addr"`
	TLSCertFile      string `mapstructure:"tls_cert_file"`
	TLSKeyFile       string `mapstructure:"tls_key_file"`
	// RegistryTLSCert is the PEM path for verifying the registry gRPC TLS cert.
	// Leave empty to use system CAs. Set to "insecure" to skip verification (dev only).
	RegistryTLSCert      string        `mapstructure:"registry_tls_cert"`
	LeaseTTL             time.Duration `mapstructure:"lease_ttl"`
	HealthReportInterval time.Duration `mapstructure:"health_report_interval"`
	ShutdownTimeout      time.Duration `mapstructure:"shutdown_timeout"`
	MaxStreams           int64         `mapstructure:"max_streams"`
	GRPCAuthToken        string        `mapstructure:"grpc_auth_token"`
	StreamIdleTimeout    time.Duration `mapstructure:"stream_idle_timeout"`
	// mTLS: require and validate client certificates from agents.
	MTLSEnabled  bool   `mapstructure:"mtls_enabled"`
	ClientCAFile string `mapstructure:"client_ca_file"`
	// mTLS towards registry: present a client certificate on the gRPC connection.
	RegistryMTLS     bool   `mapstructure:"registry_mtls"`
	RegistryCertFile string `mapstructure:"registry_cert_file"`
	RegistryKeyFile  string `mapstructure:"registry_key_file"`
	// Abuse prevention.
	AuthRateLimitRPM    int   `mapstructure:"auth_rate_limit_rpm"`
	MaxTunnelsPerAgent  int   `mapstructure:"max_tunnels_per_agent"`
	MaxStreamsPerTunnel int64 `mapstructure:"max_streams_per_tunnel"`
	// Phase 4: region, session resume, UDP.
	Region               string        `mapstructure:"region"`
	SessionResumeEnabled bool          `mapstructure:"session_resume_enabled"`
	SessionResumeTTL     time.Duration `mapstructure:"session_resume_ttl"`
	UDPListenAddr        string        `mapstructure:"udp_listen_addr"`
}

type RegistryConfig struct {
	GRPCListenAddr  string        `mapstructure:"grpc_listen_addr"`
	TLSCertFile     string        `mapstructure:"tls_cert_file"`
	TLSKeyFile      string        `mapstructure:"tls_key_file"`
	SessionTTL      time.Duration `mapstructure:"session_ttl"`
	CleanupInterval time.Duration `mapstructure:"cleanup_interval"`
	GRPCAuthToken   string        `mapstructure:"grpc_auth_token"`
	// EtcdEndpoints enables the etcd Coordinator backend for HA deployments.
	// When empty (default), the in-memory coordinator is used.
	EtcdEndpoints   []string      `mapstructure:"etcd_endpoints"`
	EtcdDialTimeout time.Duration `mapstructure:"etcd_dial_timeout"`
	// mTLS: require and validate client certificates from gateways.
	MTLSEnabled      bool   `mapstructure:"mtls_enabled"`
	ClientCAFile     string `mapstructure:"client_ca_file"`
	AuthRateLimitRPM int    `mapstructure:"auth_rate_limit_rpm"`
}

type DashboardConfig struct {
	HTTPListenAddr string        `mapstructure:"http_listen_addr"`
	JWTSecret      string        `mapstructure:"jwt_secret"`
	JWTTTL         time.Duration `mapstructure:"jwt_ttl"`
	BaseURL        string        `mapstructure:"base_url"`
	SMTPHost       string        `mapstructure:"smtp_host"`
	SMTPPort       int           `mapstructure:"smtp_port"`
	SMTPFrom       string        `mapstructure:"smtp_from"`
	// Token hardening.
	RefreshTokenEnabled bool          `mapstructure:"refresh_token_enabled"`
	RefreshTokenTTL     time.Duration `mapstructure:"refresh_token_ttl"`
	AuthRateLimitRPM    int           `mapstructure:"auth_rate_limit_rpm"`
	MaxFailedLogins     int           `mapstructure:"max_failed_logins"`
	LockoutDuration     time.Duration `mapstructure:"lockout_duration"`
}

// LogConfig holds structured logging settings.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type ObservabilityConfig struct {
	MetricsEnabled  bool   `mapstructure:"metrics_enabled"`
	MetricsAddr     string `mapstructure:"metrics_addr"`
	TracingEnabled  bool   `mapstructure:"tracing_enabled"`
	TracingEndpoint string `mapstructure:"tracing_endpoint"`
}

// DBConfig holds database connection settings.
type DBConfig struct {
	Driver          string        `mapstructure:"driver"`
	DSN             string        `mapstructure:"dsn"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	AutoMigrate     bool          `mapstructure:"auto_migrate"`
}

// Config is the union of all service configurations. Each binary only populates
// the section(s) it cares about; unused sections remain at their zero values.
type Config struct {
	ServiceName   string              `mapstructure:"service_name"`
	Log           LogConfig           `mapstructure:"log"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Agent         AgentConfig         `mapstructure:"agent"`
	Gateway       GatewayConfig       `mapstructure:"gateway"`
	Registry      RegistryConfig      `mapstructure:"registry"`
	Dashboard     DashboardConfig     `mapstructure:"dashboard"`
	DB            DBConfig            `mapstructure:"db"`
}

type ServiceType string

const (
	ServiceAgent     ServiceType = "agent"
	ServiceGateway   ServiceType = "gateway"
	ServiceRegistry  ServiceType = "registry"
	ServiceDashboard ServiceType = "dashboard"
)

func defaults(svc ServiceType) {
	// Shared defaults
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")

	viper.SetDefault("observability.metrics_enabled", true)
	viper.SetDefault("observability.metrics_addr", ":9090")
	viper.SetDefault("observability.tracing_enabled", false)
	viper.SetDefault("observability.tracing_endpoint", "localhost:4317")

	viper.SetDefault("db.driver", "memory")
	viper.SetDefault("db.dsn", "")
	viper.SetDefault("db.max_open_conns", 10)
	viper.SetDefault("db.max_idle_conns", 5)
	viper.SetDefault("db.conn_max_lifetime", 5*time.Minute)
	viper.SetDefault("db.auto_migrate", true)

	switch svc {
	case ServiceAgent:
		viper.SetDefault("agent.gateway_addr", "localhost:4433")
		viper.SetDefault("agent.gateway_addrs", []string{"localhost:4433"})
		viper.SetDefault("agent.reconnect_delay", 2*time.Second)
		viper.SetDefault("agent.max_reconnect", 0)
		viper.SetDefault("agent.heartbeat_interval", 15*time.Second)
		viper.SetDefault("agent.quic_timeout", 30*time.Second)
		viper.SetDefault("agent.tls_insecure", true)
		viper.SetDefault("agent.stream_idle_timeout", 30*time.Second)
		viper.SetDefault("agent.mtls_enabled", false)
	case ServiceGateway:
		viper.SetDefault("gateway.quic_listen_addr", ":4433")
		viper.SetDefault("gateway.public_listen_addr", ":443")
		viper.SetDefault("gateway.base_domain", "tunneledge.dev")
		viper.SetDefault("gateway.registry_addr", "localhost:50051")
		viper.SetDefault("gateway.relay_id", "gateway-1")
		viper.SetDefault("gateway.advertise_addr", "")
		viper.SetDefault("gateway.registry_tls_cert", "insecure")
		viper.SetDefault("gateway.lease_ttl", 45*time.Second)
		viper.SetDefault("gateway.health_report_interval", 15*time.Second)
		viper.SetDefault("gateway.shutdown_timeout", 15*time.Second)
		viper.SetDefault("gateway.max_streams", int64(1000))
		viper.SetDefault("gateway.grpc_auth_token", "")
		viper.SetDefault("gateway.stream_idle_timeout", 30*time.Second)
		viper.SetDefault("gateway.mtls_enabled", false)
		viper.SetDefault("gateway.registry_mtls", false)
		viper.SetDefault("gateway.auth_rate_limit_rpm", 60)
		viper.SetDefault("gateway.max_tunnels_per_agent", 20)
		viper.SetDefault("gateway.max_streams_per_tunnel", int64(100))
	case ServiceRegistry:
		viper.SetDefault("registry.grpc_listen_addr", ":50051")
		viper.SetDefault("registry.session_ttl", 5*time.Minute)
		viper.SetDefault("registry.cleanup_interval", 30*time.Second)
		viper.SetDefault("registry.grpc_auth_token", "")
		viper.SetDefault("registry.mtls_enabled", false)
		viper.SetDefault("registry.auth_rate_limit_rpm", 120)
	case ServiceDashboard:
		viper.SetDefault("dashboard.http_listen_addr", ":8080")
		viper.SetDefault("dashboard.jwt_secret", "")
		viper.SetDefault("dashboard.jwt_ttl", 24*time.Hour)
		viper.SetDefault("dashboard.base_url", "http://localhost:8080")
		viper.SetDefault("dashboard.smtp_host", "localhost")
		viper.SetDefault("dashboard.smtp_port", 1025)
		viper.SetDefault("dashboard.smtp_from", "noreply@tunneledge.dev")
		viper.SetDefault("dashboard.refresh_token_enabled", false)
		viper.SetDefault("dashboard.refresh_token_ttl", 7*24*time.Hour)
		viper.SetDefault("dashboard.auth_rate_limit_rpm", 5)
		viper.SetDefault("dashboard.max_failed_logins", 10)
		viper.SetDefault("dashboard.lockout_duration", 15*time.Minute)
	}
}

func Load(svc ServiceType, opts ...Option) (*Config, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	// Use service name as config file name unless overridden.
	configName := o.configName
	if configName == "" {
		configName = string(svc)
	}

	viper.SetConfigName(configName)
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/etc/tunneledge")

	// TE_LOG_LEVEL=debug maps to log.level via the dot→underscore replacer.
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

func (c *Config) Validate(svc ServiceType) error {
	if c.Observability.MetricsEnabled && c.Observability.MetricsAddr == "" {
		return fmt.Errorf("observability.metrics_addr is required when metrics are enabled")
	}
	if c.Observability.TracingEnabled && c.Observability.TracingEndpoint == "" {
		return fmt.Errorf("observability.tracing_endpoint is required when tracing is enabled")
	}
	if c.DB.Driver == "postgres" && c.DB.DSN == "" {
		return fmt.Errorf("db.dsn is required when db.driver=postgres")
	}

	switch svc {
	case ServiceAgent:
		if len(c.Agent.GatewayTargets()) == 0 {
			return fmt.Errorf("agent.gateway_addr or agent.gateway_addrs is required")
		}
		if c.Agent.Token == "" {
			return fmt.Errorf("agent.token is required")
		}
		if c.Agent.ReconnectDelay <= 0 {
			return fmt.Errorf("agent.reconnect_delay must be greater than zero")
		}
		if c.Agent.HeartbeatInterval <= 0 {
			return fmt.Errorf("agent.heartbeat_interval must be greater than zero")
		}
		if c.Agent.StreamIdleTimeout <= 0 {
			return fmt.Errorf("agent.stream_idle_timeout must be greater than zero")
		}
		if c.Agent.MaxReconnect < 0 {
			return fmt.Errorf("agent.max_reconnect cannot be negative")
		}
	case ServiceGateway:
		if c.Gateway.QUICListenAddr == "" {
			return fmt.Errorf("gateway.quic_listen_addr is required")
		}
		if c.Gateway.PublicListenAddr == "" {
			return fmt.Errorf("gateway.public_listen_addr is required")
		}
		if c.Gateway.BaseDomain == "" {
			return fmt.Errorf("gateway.base_domain is required")
		}
		if c.Gateway.RegistryAddr == "" {
			return fmt.Errorf("gateway.registry_addr is required")
		}
		if c.Gateway.RelayID == "" {
			return fmt.Errorf("gateway.relay_id is required")
		}
		if c.Gateway.LeaseTTL <= 0 {
			return fmt.Errorf("gateway.lease_ttl must be greater than zero")
		}
		if c.Gateway.HealthReportInterval <= 0 {
			return fmt.Errorf("gateway.health_report_interval must be greater than zero")
		}
		if c.Gateway.ShutdownTimeout <= 0 {
			return fmt.Errorf("gateway.shutdown_timeout must be greater than zero")
		}
		if c.Gateway.StreamIdleTimeout <= 0 {
			return fmt.Errorf("gateway.stream_idle_timeout must be greater than zero")
		}
		if c.Gateway.MaxStreams <= 0 {
			return fmt.Errorf("gateway.max_streams must be greater than zero")
		}
	case ServiceRegistry:
		if c.Registry.GRPCListenAddr == "" {
			return fmt.Errorf("registry.grpc_listen_addr is required")
		}
		if c.Registry.SessionTTL <= 0 {
			return fmt.Errorf("registry.session_ttl must be greater than zero")
		}
		if c.Registry.CleanupInterval <= 0 {
			return fmt.Errorf("registry.cleanup_interval must be greater than zero")
		}
		if c.Registry.CleanupInterval >= c.Registry.SessionTTL {
			return fmt.Errorf("registry.cleanup_interval must be less than registry.session_ttl")
		}
	case ServiceDashboard:
		if c.Dashboard.HTTPListenAddr == "" {
			return fmt.Errorf("dashboard.http_listen_addr is required")
		}
		if c.Dashboard.JWTSecret == "" {
			return fmt.Errorf("dashboard.jwt_secret is required")
		}
		if c.Dashboard.JWTTTL <= 0 {
			return fmt.Errorf("dashboard.jwt_ttl must be greater than zero")
		}
		if c.Dashboard.BaseURL == "" {
			return fmt.Errorf("dashboard.base_url is required")
		}
	}

	return nil
}

// SaveConfig is the serializable form of an agent config written to disk.
// It uses the nested YAML structure that matches Config.
type SaveConfig struct {
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
	Observability struct {
		MetricsEnabled bool   `yaml:"metrics_enabled"`
		MetricsAddr    string `yaml:"metrics_addr"`
	} `yaml:"observability"`
	Agent struct {
		GatewayAddr       string         `yaml:"gateway_addr"`
		GatewayAddrs      []string       `yaml:"gateway_addrs,omitempty"`
		Token             string         `yaml:"token"`
		LocalAddr         string         `yaml:"local_addr,omitempty"`
		Tunnels           []TunnelConfig `yaml:"tunnels,omitempty"`
		ReconnectDelay    string         `yaml:"reconnect_delay"`
		MaxReconnect      int            `yaml:"max_reconnect"`
		HeartbeatInterval string         `yaml:"heartbeat_interval"`
		QUICTimeout       string         `yaml:"quic_timeout"`
	} `yaml:"agent"`
}

func Save(cfg *Config, path string) error {
	var sc SaveConfig
	sc.Log.Level = cfg.Log.Level
	sc.Log.Format = cfg.Log.Format
	sc.Observability.MetricsEnabled = cfg.Observability.MetricsEnabled
	sc.Observability.MetricsAddr = cfg.Observability.MetricsAddr
	targets := cfg.Agent.GatewayTargets()
	if len(targets) > 0 {
		sc.Agent.GatewayAddr = targets[0]
	}
	sc.Agent.GatewayAddrs = targets
	sc.Agent.Token = cfg.Agent.Token
	sc.Agent.LocalAddr = cfg.Agent.LocalAddr
	sc.Agent.Tunnels = cfg.Agent.Tunnels
	sc.Agent.ReconnectDelay = cfg.Agent.ReconnectDelay.String()
	sc.Agent.MaxReconnect = cfg.Agent.MaxReconnect
	sc.Agent.HeartbeatInterval = cfg.Agent.HeartbeatInterval.String()
	sc.Agent.QUICTimeout = cfg.Agent.QUICTimeout.String()

	data, err := yaml.Marshal(sc)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (c AgentConfig) GatewayTargets() []string {
	seen := make(map[string]struct{})
	targets := make([]string, 0, 1+len(c.GatewayAddrs))
	appendTarget := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		if _, exists := seen[addr]; exists {
			return
		}
		seen[addr] = struct{}{}
		targets = append(targets, addr)
	}
	appendTarget(c.GatewayAddr)
	for _, addr := range c.GatewayAddrs {
		appendTarget(addr)
	}
	return targets
}
