package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tunneledge/internal/dashboard"
	"tunneledge/internal/email"
	"tunneledge/internal/store/pgstore"
	"tunneledge/pkg/config"
	"tunneledge/pkg/logger"
	"tunneledge/pkg/observability"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

func main() {
	cfg, err := config.Load(config.ServiceDashboard)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(config.ServiceDashboard); err != nil {
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

	jwtCfg := dashboard.JWTConfig{
		Secret: cfg.Dashboard.JWTSecret,
		TTL:    cfg.Dashboard.JWTTTL,
	}

	emailSvc := email.NewService(email.Config{
		SMTPHost: cfg.Dashboard.SMTPHost,
		SMTPPort: cfg.Dashboard.SMTPPort,
		From:     cfg.Dashboard.SMTPFrom,
	})

	opts := dashboard.ServerOptions{
		Addr:         cfg.Dashboard.HTTPListenAddr,
		JWTCfg:       jwtCfg,
		BaseURL:      cfg.Dashboard.BaseURL,
		EmailService: emailSvc,
	}

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
				log.Fatal().Err(err).Msg("failed to run migrations")
			}
			log.Info().Msg("database migrations completed")
		}

		opts.Users = pgstore.NewPGUserRepository(db)
		opts.Agents = pgstore.NewPGAgentProfileRepository(db)
		opts.Tokens = pgstore.NewPGTokenRepository(db)
		opts.Tunnels = pgstore.NewPGTunnelConfigRepository(db)
		opts.Sessions = pgstore.NewPGSessionRepository(db)
		opts.Verifications = pgstore.NewPGEmailVerificationRepository(db)
		// Phase 3: security repos
		pgRefreshRepo := pgstore.NewRefreshTokenRepository(db)
		opts.RevokedJTIs = pgstore.NewRevokedJTIRepository(db)
		opts.RefreshTokens = pgRefreshRepo
		opts.RefreshTokenEnabled = cfg.Dashboard.RefreshTokenEnabled
		opts.RefreshTokenTTL = cfg.Dashboard.RefreshTokenTTL
		opts.AuthRateLimitRPM = cfg.Dashboard.AuthRateLimitRPM
		opts.TunnelACLs = pgstore.NewTunnelACLRepository(db)
		opts.AuditRepo = pgstore.NewAuditRepository(db)
	} else {
		log.Fatal().Msg("dashboard requires postgres database, set db_driver=postgres and db_dsn")
	}

	srv := dashboard.NewServer(opts)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		err := srv.Start()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	log.Info().
		Str("service", "dashboard").
		Str("addr", cfg.Dashboard.HTTPListenAddr).
		Msg("dashboard started")

	select {
	case <-ctx.Done():
		log.Info().Msg("shutting down dashboard")
	case <-gctx.Done():
	}

	if err := srv.Stop(context.Background()); err != nil {
		log.Error().Err(err).Msg("failed to stop dashboard")
	}
	if err := g.Wait(); err != nil {
		log.Fatal().Err(err).Msg("dashboard server error")
	}

	log.Info().Msg("dashboard stopped")
}
