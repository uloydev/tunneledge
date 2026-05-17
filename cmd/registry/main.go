package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/internal/registry"
	"tunneledge/internal/store/memstore"
	"tunneledge/internal/store/pgstore"
	"tunneledge/pkg/config"
	"tunneledge/pkg/logger"
	"tunneledge/pkg/metrics"
	"tunneledge/pkg/observability"
	pb "tunneledge/proto/registry/v1"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

func main() {
	cfg, err := config.Load(config.ServiceRegistry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(config.ServiceRegistry); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	logr := logger.New(logger.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})
	log.Logger = logr.Logger

	traceShutdown := func(context.Context) error { return nil }
	if cfg.Observability.TracingEnabled {
		traceShutdown, err = observability.StartTracing(context.Background(), cfg.ServiceName, cfg.Observability.TracingEndpoint)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to initialize tracing")
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := traceShutdown(shutdownCtx); err != nil {
				log.Error().Err(err).Msg("failed to stop tracing")
			}
		}()
	}

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
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := grpcSrv.Serve(lis); err != nil && gctx.Err() == nil {
			return err
		}
		return nil
	})

	select {
	case <-ctx.Done():
		log.Info().Msg("shutting down registry")
	case <-gctx.Done():
	}

	grpcSrv.GracefulStop()
	if err := g.Wait(); err != nil {
		log.Fatal().Err(err).Msg("gRPC server error")
	}

	if metricsSrv != nil {
		if err := metricsSrv.Stop(context.Background()); err != nil {
			log.Error().Err(err).Msg("failed to stop metrics server")
		}
	}

	log.Info().Msg("registry stopped")
}

func resolveSessionStore(cfg *config.Config) domain.SessionRepository {
	if cfg.DB.Driver == "postgres" && cfg.DB.DSN != "" {
		db, err := pgstore.NewDB(cfg.DB.DSN, pgstore.DBOptions{
			MaxOpenConns:    cfg.DB.MaxOpenConns,
			MaxIdleConns:    cfg.DB.MaxIdleConns,
			ConnMaxLifetime: cfg.DB.ConnMaxLifetime,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("failed to connect to database")
		}

		if cfg.DB.AutoMigrate {
			if err := pgstore.AutoMigrate(db); err != nil {
				log.Fatal().Err(err).Msg("failed to run auto migrations")
			}
			log.Info().Msg("database migrations completed")
		}

		log.Info().Msg("using PostgreSQL session store")
		return pgstore.NewPGSessionRepository(db)
	}

	log.Warn().Msg("using in-memory session store")
	return memstore.NewMemorySessionRepository()
}
