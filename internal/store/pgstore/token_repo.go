package pgstore

import (
	"context"
	"fmt"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

type PGTokenRepository struct {
	db *gorm.DB
}

func NewPGTokenRepository(db *gorm.DB) *PGTokenRepository {
	return &PGTokenRepository{db: db}
}

func (r *PGTokenRepository) Create(ctx context.Context, agentID, tokenHash string) error {
	m := &TokenModel{
		AgentID:   agentID,
		TokenHash: tokenHash,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("failed to create token: %w", err)
	}
	return nil
}

func (r *PGTokenRepository) GetByAgentID(ctx context.Context, agentID string) (string, error) {
	var m TokenModel
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).First(&m).Error; err != nil {
		return "", fmt.Errorf("token not found for agent %s: %w", agentID, err)
	}
	return m.TokenHash, nil
}

func (r *PGTokenRepository) List(ctx context.Context) (map[string]string, error) {
	var models []TokenModel
	if err := r.db.WithContext(ctx).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}
	result := make(map[string]string, len(models))
	for _, m := range models {
		result[m.TokenHash] = m.AgentID
	}
	return result, nil
}

func (r *PGTokenRepository) Delete(ctx context.Context, agentID string) error {
	result := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Delete(&TokenModel{})
	if result.RowsAffected == 0 {
		return fmt.Errorf("token not found for agent %s", agentID)
	}
	return result.Error
}

var _ domain.TokenRepository = (*PGTokenRepository)(nil)
