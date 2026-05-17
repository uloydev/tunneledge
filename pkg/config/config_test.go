package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateGatewayConfig(t *testing.T) {
	cfg := &Config{
		Observability: ObservabilityConfig{MetricsEnabled: true, MetricsAddr: ":9090"},
		Gateway: GatewayConfig{
			QUICListenAddr:       ":4433",
			PublicListenAddr:     ":443",
			BaseDomain:           "tunneledge.dev",
			RegistryAddr:         "localhost:50051",
			RelayID:              "gateway-1",
			LeaseTTL:             45 * time.Second,
			HealthReportInterval: 15 * time.Second,
			ShutdownTimeout:      15 * time.Second,
			MaxStreams:           100,
			StreamIdleTimeout:    30 * time.Second,
		},
	}
	require.NoError(t, cfg.Validate(ServiceGateway))

	cfg.Gateway.MaxStreams = 0
	require.Error(t, cfg.Validate(ServiceGateway))
}

func TestValidateRegistryConfig(t *testing.T) {
	cfg := &Config{
		Observability: ObservabilityConfig{MetricsEnabled: true, MetricsAddr: ":9090"},
		Registry: RegistryConfig{
			GRPCListenAddr:  ":50051",
			SessionTTL:      5 * time.Minute,
			CleanupInterval: 30 * time.Second,
		},
	}
	require.NoError(t, cfg.Validate(ServiceRegistry))

	cfg.Registry.CleanupInterval = 5 * time.Minute
	require.Error(t, cfg.Validate(ServiceRegistry))
}

func TestValidateDashboardConfig(t *testing.T) {
	cfg := &Config{
		Observability: ObservabilityConfig{MetricsEnabled: true, MetricsAddr: ":9090"},
		Dashboard: DashboardConfig{
			HTTPListenAddr: ":8080",
			JWTSecret:      "secret",
			JWTTTL:         24 * time.Hour,
			BaseURL:        "http://localhost:8080",
		},
	}
	require.NoError(t, cfg.Validate(ServiceDashboard))

	cfg.Dashboard.JWTSecret = ""
	require.Error(t, cfg.Validate(ServiceDashboard))
}

func TestValidateAgentConfig(t *testing.T) {
	cfg := &Config{
		Observability: ObservabilityConfig{MetricsEnabled: true, MetricsAddr: ":9090"},
		Agent: AgentConfig{
			GatewayAddrs:      []string{"localhost:4433", "localhost:4434"},
			Token:             "token",
			ReconnectDelay:    2 * time.Second,
			HeartbeatInterval: 15 * time.Second,
			StreamIdleTimeout: 30 * time.Second,
		},
	}
	require.NoError(t, cfg.Validate(ServiceAgent))

	cfg.Agent.Token = ""
	require.Error(t, cfg.Validate(ServiceAgent))
}

func TestAgentGatewayTargets(t *testing.T) {
	targets := AgentConfig{
		GatewayAddr:  "localhost:4433",
		GatewayAddrs: []string{"localhost:4433", " localhost:4434 ", ""},
	}.GatewayTargets()
	require.Equal(t, []string{"localhost:4433", "localhost:4434"}, targets)
}
