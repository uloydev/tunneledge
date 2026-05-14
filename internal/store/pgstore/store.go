package pgstore

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewDB opens a PostgreSQL connection and returns the *gorm.DB handle.
func NewDB(dsn string) (*gorm.DB, error) {
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

	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

// AutoMigrate runs GORM auto-migration for all pgstore models.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&TokenModel{},
		&TunnelSessionModel{},
		&UserModel{},
		&AgentProfileModel{},
		&TunnelDefinitionModel{},
		&EmailVerificationModel{},
	)
}
