package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tunneledge/internal/auth"
	"tunneledge/internal/gateway"
	"tunneledge/pkg/config"
	"tunneledge/pkg/logger"
	"tunneledge/pkg/metrics"

	"github.com/rs/zerolog/log"
)

func main() {
	cfg, err := config.Load(config.ServiceGateway)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logr := logger.New(logger.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})
	log.Logger = logr.Logger

	tokens := map[string]string{
		"dev-token": "agent-1",
	}

	var m *metrics.Metrics
	var metricsSrv *metrics.Server
	if cfg.Observability.MetricsEnabled {
		m = metrics.New("tunneledge")
		metricsSrv = metrics.NewServer(cfg.Observability.MetricsAddr, m)
		metricsSrv.Start()
	}

	gw, err := gateway.NewGateway(gateway.Options{
		QUICListenAddr:   cfg.Gateway.QUICListenAddr,
		PublicListenAddr: cfg.Gateway.PublicListenAddr,
		BaseDomain:       cfg.Gateway.BaseDomain,
		RegistryAddr:     cfg.Gateway.RegistryAddr,
		TLSCertFile:      cfg.Gateway.TLSCertFile,
		TLSKeyFile:       cfg.Gateway.TLSKeyFile,
		MaxStreams:       cfg.Gateway.MaxStreams,
		Authenticator:    auth.NewTokenAuthenticator(tokens),
		Metrics:          m,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create gateway")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info().
		Str("service", cfg.ServiceName).
		Str("quic_listen_addr", cfg.Gateway.QUICListenAddr).
		Str("public_listen_addr", cfg.Gateway.PublicListenAddr).
		Str("base_domain", cfg.Gateway.BaseDomain).
		Msg("starting gateway")

	if err := gw.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("gateway error")
	}

	if metricsSrv != nil {
		if err := metricsSrv.Stop(context.Background()); err != nil {
			log.Error().Err(err).Msg("failed to stop metrics server")
		}
	}

	log.Info().Msg("gateway stopped")
}
