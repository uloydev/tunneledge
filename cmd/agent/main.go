package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tunneledge/internal/agent"
	"tunneledge/pkg/config"
	"tunneledge/pkg/logger"
	"tunneledge/pkg/metrics"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	flagConfig      string
	flagGatewayAddr string
	flagToken       string
	flagLocalAddr   string
	flagLogLevel    string
	flagLogFormat   string
	flagMetricsAddr string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tunneledge-agent",
		Short: "TunnelEdge Agent — Expose local TCP services via QUIC tunnel",
		Long:  "TunnelEdge Agent connects to a TunnelEdge Gateway via QUIC and relays traffic to a local TCP service.",
		RunE:  runAgent,
	}

	rootCmd.Flags().StringVarP(&flagConfig, "config", "c", "", "config file path")
	rootCmd.Flags().StringVar(&flagGatewayAddr, "gateway-addr", "", "gateway QUIC address (default: localhost:4433)")
	rootCmd.Flags().StringVarP(&flagToken, "token", "t", "", "authentication token (required)")
	rootCmd.Flags().StringVar(&flagLocalAddr, "local-addr", "", "local TCP service address (e.g. localhost:3000)")
	rootCmd.Flags().StringVar(&flagLogLevel, "log-level", "", "log level: debug, info, warn, error")
	rootCmd.Flags().StringVar(&flagLogFormat, "log-format", "", "log format: json, console")
	rootCmd.Flags().StringVar(&flagMetricsAddr, "metrics-addr", "", "metrics server address")

	_ = rootCmd.MarkFlagRequired("token")
	_ = rootCmd.MarkFlagRequired("local-addr")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runAgent(cmd *cobra.Command, args []string) error {
	opts := []config.Option{}
	if flagConfig != "" {
		opts = append(opts, config.WithConfigPath(flagConfig))
	}

	cfg, err := config.Load(config.ServiceAgent, opts...)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if flagGatewayAddr != "" {
		cfg.Agent.GatewayAddr = flagGatewayAddr
	}
	if flagToken != "" {
		cfg.Agent.Token = flagToken
	}
	if flagLocalAddr != "" {
		cfg.Agent.LocalAddr = flagLocalAddr
	}
	if flagLogLevel != "" {
		cfg.Log.Level = flagLogLevel
	}
	if flagLogFormat != "" {
		cfg.Log.Format = flagLogFormat
	}
	if flagMetricsAddr != "" {
		cfg.Observability.MetricsAddr = flagMetricsAddr
	}

	logr := logger.New(logger.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})
	log.Logger = logr.Logger

	log.Info().
		Str("service", cfg.ServiceName).
		Str("gateway_addr", cfg.Agent.GatewayAddr).
		Str("local_addr", cfg.Agent.LocalAddr).
		Msg("starting agent")

	var m *metrics.Metrics
	var metricsSrv *metrics.Server
	if cfg.Observability.MetricsEnabled {
		m = metrics.New("tunneledge")
		metricsSrv = metrics.NewServer(cfg.Observability.MetricsAddr, m)
		metricsSrv.Start()
	}

	a := agent.NewAgent(agent.Options{
		GatewayAddr:       cfg.Agent.GatewayAddr,
		Token:             cfg.Agent.Token,
		LocalAddr:         cfg.Agent.LocalAddr,
		ReconnectDelay:    cfg.Agent.ReconnectDelay,
		MaxReconnect:      cfg.Agent.MaxReconnect,
		HeartbeatInterval: cfg.Agent.HeartbeatInterval,
		Metrics:           m,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := a.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error().Err(err).Msg("agent stopped with error")
	}

	if metricsSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsSrv.Stop(shutdownCtx)
	}

	log.Info().Msg("agent stopped")
	return nil
}
