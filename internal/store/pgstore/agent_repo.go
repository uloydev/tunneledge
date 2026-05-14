package pgstore

import (
	"context"
	"fmt"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

type PGAgentProfileRepository struct {
	db *gorm.DB
}

func NewPGAgentProfileRepository(db *gorm.DB) *PGAgentProfileRepository {
	return &PGAgentProfileRepository{db: db}
}

func (r *PGAgentProfileRepository) Create(ctx context.Context, agent *domain.AgentProfile) error {
	m := &AgentProfileModel{
		UserID:    agent.UserID,
		Name:      agent.Name,
		AgentID:   agent.AgentID,
		TokenHash: agent.TokenHash,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("failed to create agent profile: %w", err)
	}
	agent.ID = m.ID
	agent.CreatedAt = m.CreatedAt
	agent.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *PGAgentProfileRepository) GetByID(ctx context.Context, id uint) (*domain.AgentProfile, error) {
	var m AgentProfileModel
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("agent profile %d not found: %w", id, err)
	}
	return modelToAgentProfile(&m), nil
}

func (r *PGAgentProfileRepository) GetByAgentID(ctx context.Context, agentID string) (*domain.AgentProfile, error) {
	var m AgentProfileModel
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).First(&m).Error; err != nil {
		return nil, fmt.Errorf("agent profile %s not found: %w", agentID, err)
	}
	return modelToAgentProfile(&m), nil
}

func (r *PGAgentProfileRepository) ListByUserID(ctx context.Context, userID uint) ([]*domain.AgentProfile, error) {
	var models []AgentProfileModel
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&models).Error; err != nil {
		return nil, err
	}

	result := make([]*domain.AgentProfile, 0, len(models))
	for i := range models {
		result = append(result, modelToAgentProfile(&models[i]))
	}
	return result, nil
}

func (r *PGAgentProfileRepository) Update(ctx context.Context, agent *domain.AgentProfile) error {
	result := r.db.WithContext(ctx).Model(&AgentProfileModel{}).Where("id = ?", agent.ID).
		Updates(map[string]interface{}{
			"name":       agent.Name,
			"agent_id":   agent.AgentID,
			"token_hash": agent.TokenHash,
		})
	if result.RowsAffected == 0 {
		return fmt.Errorf("agent profile %d not found", agent.ID)
	}
	return result.Error
}

func (r *PGAgentProfileRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&AgentProfileModel{}, id)
	if result.RowsAffected == 0 {
		return fmt.Errorf("agent profile %d not found", id)
	}
	return result.Error
}

func modelToAgentProfile(m *AgentProfileModel) *domain.AgentProfile {
	return &domain.AgentProfile{
		ID:        m.ID,
		UserID:    m.UserID,
		Name:      m.Name,
		AgentID:   m.AgentID,
		TokenHash: m.TokenHash,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

var _ domain.AgentProfileRepository = (*PGAgentProfileRepository)(nil)
