package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tunneledge/internal/dashboard"
	"tunneledge/internal/email"
	"tunneledge/internal/store/pgstore"
	"tunneledge/pkg/config"
	"tunneledge/pkg/logger"

	"github.com/rs/zerolog/log"
)

func main() {
	cfg, err := config.Load(config.ServiceDashboard)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logr := logger.New(logger.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})
	log.Logger = logr.Logger

	jwtCfg := dashboard.JWTConfig{
		Secret: cfg.Dashboard.JWTSecret,
		TTL:    cfg.Dashboard.JWTTTL,
	}

	if jwtCfg.Secret == "" {
		log.Fatal().Msg("jwt_secret is required")
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
	} else {
		log.Fatal().Msg("dashboard requires postgres database, set db_driver=postgres and db_dsn")
	}

	srv := dashboard.NewServer(opts)
	srv.Start()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info().
		Str("service", "dashboard").
		Str("addr", cfg.Dashboard.HTTPListenAddr).
		Msg("dashboard started")

	<-ctx.Done()
	log.Info().Msg("shutting down dashboard")

	if err := srv.Stop(context.Background()); err != nil {
		log.Error().Err(err).Msg("failed to stop dashboard")
	}

	log.Info().Msg("dashboard stopped")
}
