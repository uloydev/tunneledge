package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tunneledge/internal/auth"
	"tunneledge/internal/gateway"
	"tunneledge/internal/registry"
	"tunneledge/internal/store/pgstore"
	"tunneledge/pkg/config"
	"tunneledge/pkg/logger"
	"tunneledge/pkg/metrics"
	"tunneledge/pkg/observability"

	"github.com/rs/zerolog/log"
)

func main() {
	cfg, err := config.Load(config.ServiceGateway)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(config.ServiceGateway); err != nil {
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

	authenticator := resolveAuthenticator(cfg)

	registryClient, err := registry.NewGRPCRegistryClientWithOptions(cfg.Gateway.RegistryAddr, registry.ClientOptions{
		TLSCertFile: cfg.Gateway.RegistryTLSCert,
		AuthToken:   cfg.Gateway.GRPCAuthToken,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to registry")
	}
	defer registryClient.Close()

	var m *metrics.Metrics
	var metricsSrv *metrics.Server
	if cfg.Observability.MetricsEnabled {
		m = metrics.New("tunneledge")
		metricsSrv = metrics.NewServer(cfg.Observability.MetricsAddr, m)
		metricsSrv.Start()
	}

	gw, err := gateway.NewGateway(gateway.Options{
		QUICListenAddr:       cfg.Gateway.QUICListenAddr,
		PublicListenAddr:     cfg.Gateway.PublicListenAddr,
		BaseDomain:           cfg.Gateway.BaseDomain,
		RelayID:              cfg.Gateway.RelayID,
		AdvertiseAddr:        cfg.Gateway.AdvertiseAddr,
		TLSCertFile:          cfg.Gateway.TLSCertFile,
		TLSKeyFile:           cfg.Gateway.TLSKeyFile,
		LeaseTTL:             cfg.Gateway.LeaseTTL,
		HealthReportInterval: cfg.Gateway.HealthReportInterval,
		MaxStreams:           cfg.Gateway.MaxStreams,
		ShutdownTimeout:      cfg.Gateway.ShutdownTimeout,
		StreamIdleTimeout:    cfg.Gateway.StreamIdleTimeout,
		Authenticator:        authenticator,
		RegistryClient:       registryClient,
		Metrics:              m,
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

func resolveAuthenticator(cfg *config.Config) auth.Authenticator {
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
		}

		tokenRepo := pgstore.NewPGAgentProfileRepository(db)
		log.Info().Msg("using database-backed token authenticator (agent_profiles table)")
		return auth.NewDBTokenAuthenticator(tokenRepo)
	}

	log.Warn().Msg("using in-memory token store (no database configured)")
	return auth.NewHashedTokenAuthenticator(nil)
}
