package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tunneledge/internal/agent"
	"tunneledge/internal/tui"
	"tunneledge/pkg/config"
	"tunneledge/pkg/logger"
	"tunneledge/pkg/metrics"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	flagConfig      string
	flagGatewayAddr string
	flagToken       string
	flagLocalAddr   string
	flagTunnels     []string
	flagLogLevel    string
	flagLogFormat   string
	flagMetricsAddr string
	flagHeadless    bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tunneledge-agent",
		Short: "TunnelEdge Agent — Expose local TCP services via QUIC tunnel",
		Long:  "TunnelEdge Agent connects to a TunnelEdge Gateway via QUIC and relays traffic to local TCP services.",
		RunE:  runRoot,
	}

	headlessCmd := &cobra.Command{
		Use:   "headless",
		Short: "Run agent in headless mode (for deployment/CI)",
		RunE:  runHeadless,
	}

	rootCmd.Flags().StringVarP(&flagConfig, "config", "c", "", "config file path")
	rootCmd.Flags().StringVar(&flagGatewayAddr, "gateway-addr", "", "gateway QUIC address (default: localhost:4433)")
	rootCmd.Flags().StringVarP(&flagToken, "token", "t", "", "authentication token")
	rootCmd.Flags().StringVar(&flagLocalAddr, "local-addr", "", "local TCP service address (e.g. localhost:3000)")
	rootCmd.Flags().StringArrayVar(&flagTunnels, "tunnel", nil, "tunnel definition: label=local_addr (repeatable)")
	rootCmd.Flags().StringVar(&flagLogLevel, "log-level", "", "log level: debug, info, warn, error")
	rootCmd.Flags().StringVar(&flagLogFormat, "log-format", "", "log format: json, console")
	rootCmd.Flags().StringVar(&flagMetricsAddr, "metrics-addr", "", "metrics server address")
	rootCmd.Flags().BoolVar(&flagHeadless, "headless", false, "run in headless mode (no TUI)")

	headlessCmd.Flags().StringVarP(&flagConfig, "config", "c", "", "config file path")
	headlessCmd.Flags().StringVar(&flagGatewayAddr, "gateway-addr", "", "gateway QUIC address (default: localhost:4433)")
	headlessCmd.Flags().StringVarP(&flagToken, "token", "t", "", "authentication token (required)")
	headlessCmd.Flags().StringVar(&flagLocalAddr, "local-addr", "", "local TCP service address (e.g. localhost:3000)")
	headlessCmd.Flags().StringArrayVar(&flagTunnels, "tunnel", nil, "tunnel definition: label=local_addr (repeatable)")
	headlessCmd.Flags().StringVar(&flagLogLevel, "log-level", "", "log level: debug, info, warn, error")
	headlessCmd.Flags().StringVar(&flagLogFormat, "log-format", "", "log format: json, console")
	headlessCmd.Flags().StringVar(&flagMetricsAddr, "metrics-addr", "", "metrics server address")

	_ = headlessCmd.MarkFlagRequired("token")

	rootCmd.AddCommand(headlessCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func parseTunnelFlag(s string) (label, localAddr string, err error) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid tunnel format %q: expected label=local_addr", s)
	}
	label = strings.TrimSpace(parts[0])
	localAddr = strings.TrimSpace(parts[1])
	if label == "" || localAddr == "" {
		return "", "", fmt.Errorf("invalid tunnel format %q: both label and local_addr required", s)
	}
	return label, localAddr, nil
}

func loadConfig() (*config.Config, error) {
	opts := []config.Option{}
	if flagConfig != "" {
		opts = append(opts, config.WithConfigPath(flagConfig))
	}

	cfg, err := config.Load(config.ServiceAgent, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
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

	return cfg, nil
}

func runRoot(cmd *cobra.Command, args []string) error {
	if flagHeadless {
		return runHeadless(cmd, args)
	}

	return runTUI()
}

func runTUI() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	logr := logger.New(logger.Config{
		Level:  cfg.Log.Level,
		Format: "console",
	})
	log.Logger = logr.Logger

	app := tui.NewApp(cfg, flagConfig)

	logWriter := app.Bridge().LogWriter()
	log.Logger = logr.Output(logWriter)

	p := tea.NewProgram(app, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func runHeadless(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	tunnels, err := resolveTunnels(cfg, flagTunnels, flagLocalAddr)
	if err != nil {
		return err
	}

	logr := logger.New(logger.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})
	log.Logger = logr.Logger

	log.Info().
		Str("service", cfg.ServiceName).
		Str("gateway_addr", cfg.Agent.GatewayAddr).
		Int("tunnel_count", len(tunnels)).
		Msg("starting agent (headless)")

	for _, t := range tunnels {
		log.Info().
			Str("label", t.Label).
			Str("local_addr", t.LocalAddr).
			Msg("configured tunnel")
	}

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
		Tunnels:           tunnels,
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

func resolveTunnels(cfg *config.Config, flagTunnels []string, flagLocalAddr string) ([]config.TunnelConfig, error) {
	if len(flagTunnels) > 0 {
		if flagLocalAddr != "" {
			return nil, fmt.Errorf("cannot use both --local-addr and --tunnel flags")
		}
		tunnels := make([]config.TunnelConfig, 0, len(flagTunnels))
		for _, t := range flagTunnels {
			label, localAddr, err := parseTunnelFlag(t)
			if err != nil {
				return nil, err
			}
			tunnels = append(tunnels, config.TunnelConfig{Label: label, LocalAddr: localAddr})
		}
		return tunnels, nil
	}

	if cfg.Agent.LocalAddr != "" {
		return []config.TunnelConfig{{Label: "default", LocalAddr: cfg.Agent.LocalAddr}}, nil
	}

	if len(cfg.Agent.Tunnels) > 0 {
		return cfg.Agent.Tunnels, nil
	}

	return nil, fmt.Errorf("no tunnels configured: use --tunnel or --local-addr")
}
