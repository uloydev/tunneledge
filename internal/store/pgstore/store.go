package pgstore

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DBOptions carries connection-pool tunables. Zero values fall back to safe defaults.
type DBOptions struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func (o DBOptions) maxOpenConns() int {
	if o.MaxOpenConns > 0 {
		return o.MaxOpenConns
	}
	return 10
}

func (o DBOptions) maxIdleConns() int {
	if o.MaxIdleConns > 0 {
		return o.MaxIdleConns
	}
	return 5
}

func (o DBOptions) connMaxLifetime() time.Duration {
	if o.ConnMaxLifetime > 0 {
		return o.ConnMaxLifetime
	}
	return 5 * time.Minute
}

// NewDB opens a PostgreSQL connection and returns the *gorm.DB handle.
// Pass DBOptions{} to use safe defaults.
func NewDB(dsn string, opts DBOptions) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(opts.maxOpenConns())
	sqlDB.SetMaxIdleConns(opts.maxIdleConns())
	sqlDB.SetConnMaxLifetime(opts.connMaxLifetime())

	return db, nil
}

// AutoMigrate runs GORM auto-migration for all pgstore models.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&TokenModel{},
		&TunnelSessionModel{},
		&RelayModel{},
		&LeaseModel{},
		&RelayHealthModel{},
		&UserModel{},
		&AgentProfileModel{},
		&TunnelDefinitionModel{},
		&EmailVerificationModel{},
		&AuditEventModel{},
		&RefreshTokenModel{},
		&RevokedJTIModel{},
		&TunnelACLModel{},
	)
}
