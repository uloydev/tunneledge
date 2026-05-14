package store

import (
	"fmt"
	"time"

	"tunneledge/internal/auth"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store is the legacy store helper for backward compatibility with
// cmd/gateway and cmd/registry token loading.
type Store struct {
	db *gorm.DB
}

func NewStore(dsn string) (*Store, error) {
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

	return &Store{db: db}, nil
}

func NewStoreWithDB(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) AutoMigrate() error {
	return s.db.AutoMigrate(&Token{}, &TunnelSession{})
}

func (s *Store) GetDB() *gorm.DB {
	return s.db
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// LoadTokens returns token_hash → agentID map for use with HashedTokenAuthenticator.
func (s *Store) LoadTokens() (map[string]string, error) {
	var tokens []Token
	if err := s.db.Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("failed to load tokens: %w", err)
	}

	result := make(map[string]string, len(tokens))
	for _, t := range tokens {
		result[t.Token] = t.AgentID
	}
	return result, nil
}

func (s *Store) SeedDefaultTokens() error {
	var count int64
	s.db.Model(&Token{}).Count(&count)
	if count > 0 {
		return nil
	}

	type seed struct {
		plaintext string
		agentID   string
	}
	seeds := []seed{
		{"dev-token", "agent-1"},
		{"dev-token-2", "agent-2"},
	}

	var rows []Token
	for _, s := range seeds {
		hash, err := auth.HashToken(s.plaintext)
		if err != nil {
			return fmt.Errorf("failed to hash token for %s: %w", s.agentID, err)
		}
		rows = append(rows, Token{Token: hash, AgentID: s.agentID})
	}

	return s.db.Create(&rows).Error
}

func (s *Store) AddToken(token, agentID string) error {
	return s.db.Create(&Token{Token: token, AgentID: agentID}).Error
}

func (s *Store) RemoveToken(token string) error {
	return s.db.Where("token = ?", token).Delete(&Token{}).Error
}
