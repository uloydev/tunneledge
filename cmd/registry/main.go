package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"tunneledge/internal/domain"
	"tunneledge/internal/registry"
	"tunneledge/internal/store"
	"tunneledge/internal/store/memstore"
	"tunneledge/internal/store/pgstore"
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

	sessStore := resolveSessionStore(cfg)

	srv := registry.NewServer(sessStore, cfg.Registry.CleanupInterval, cfg.Registry.SessionTTL)
	defer srv.Stop()

	var metricsSrv *metrics.Server
	var m *metrics.Metrics
	if cfg.Observability.MetricsEnabled {
		m = metrics.New("tunneledge")
		metricsSrv = metrics.NewServer(cfg.Observability.MetricsAddr, m)
		metricsSrv.Start()
	}

	grpcSrv := registry.NewGRPCServer(cfg.Registry.GRPCAuthToken)
	pb.RegisterRegistryServiceServer(grpcSrv, srv)

	lis, err := net.Listen("tcp", cfg.Registry.GRPCListenAddr)
	if err != nil {
		log.Fatal().Err(err).Str("addr", cfg.Registry.GRPCListenAddr).Msg("failed to listen")
	}

	log.Info().
		Str("service", cfg.ServiceName).
		Str("grpc_listen_addr", cfg.Registry.GRPCListenAddr).
		Str("db_driver", cfg.DB.Driver).
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

func resolveSessionStore(cfg *config.Config) domain.SessionRepository {
	if cfg.DB.Driver == "postgres" && cfg.DB.DSN != "" {
		dbStore, err := store.NewStore(cfg.DB.DSN)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to connect to database")
		}

		if cfg.DB.AutoMigrate {
			if err := dbStore.AutoMigrate(); err != nil {
				log.Fatal().Err(err).Msg("failed to run auto migrations")
			}
			log.Info().Msg("database migrations completed")
		}

		if err := dbStore.SeedDefaultTokens(); err != nil {
			log.Fatal().Err(err).Msg("failed to seed default tokens")
		}

		log.Info().Msg("using PostgreSQL session store")
		return pgstore.NewPGSessionRepository(dbStore.GetDB())
	}

	log.Warn().Msg("using in-memory session store")
	return memstore.NewMemorySessionRepository()
}
