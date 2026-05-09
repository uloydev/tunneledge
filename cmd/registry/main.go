package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"tunneledge/internal/auth"
	"tunneledge/internal/registry"
	"tunneledge/internal/session"
	"tunneledge/pkg/config"
	"tunneledge/pkg/logger"
	"tunneledge/pkg/metrics"
	pb "tunneledge/proto/registry/v1"

	"github.com/rs/zerolog/log"
)

func main() {
	cfg, err := config.Load(config.ServiceRegistry)
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
		"dev-token":   "agent-1",
		"dev-token-2": "agent-2",
	}

	authenticator := auth.NewTokenAuthenticator(tokens)
	store := session.NewMemoryStore()
	srv := registry.NewServer(store, authenticator, cfg.Registry.CleanupInterval, cfg.Registry.SessionTTL)
	defer srv.Stop()

	var metricsSrv *metrics.Server
	var m *metrics.Metrics
	if cfg.Observability.MetricsEnabled {
		m = metrics.New("tunneledge")
		metricsSrv = metrics.NewServer(cfg.Observability.MetricsAddr, m)
		metricsSrv.Start()
	}

	grpcSrv := registry.NewGRPCServer()
	pb.RegisterRegistryServiceServer(grpcSrv, srv)

	lis, err := net.Listen("tcp", cfg.Registry.GRPCListenAddr)
	if err != nil {
		log.Fatal().Err(err).Str("addr", cfg.Registry.GRPCListenAddr).Msg("failed to listen")
	}

	log.Info().
		Str("service", cfg.ServiceName).
		Str("grpc_listen_addr", cfg.Registry.GRPCListenAddr).
		Msg("starting registry")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			log.Error().Err(err).Msg("gRPC server error")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down registry")

	grpcSrv.GracefulStop()

	if metricsSrv != nil {
		if err := metricsSrv.Stop(context.Background()); err != nil {
			log.Error().Err(err).Msg("failed to stop metrics server")
		}
	}

	log.Info().Msg("registry stopped")
}
