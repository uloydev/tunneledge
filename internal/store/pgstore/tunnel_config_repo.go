package pgstore

import (
	"context"
	"fmt"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

type PGTunnelDefinitionRepository struct {
	db *gorm.DB
}

func NewPGTunnelDefinitionRepository(db *gorm.DB) *PGTunnelDefinitionRepository {
	return &PGTunnelDefinitionRepository{db: db}
}

func (r *PGTunnelDefinitionRepository) Create(ctx context.Context, tunnel *domain.TunnelDefinition) error {
	m := &TunnelDefinitionModel{
		AgentProfileID: tunnel.AgentProfileID,
		Label:          tunnel.Label,
		LocalAddr:      tunnel.LocalAddr,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("failed to create tunnel definition: %w", err)
	}
	tunnel.ID = m.ID
	tunnel.CreatedAt = m.CreatedAt
	tunnel.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *PGTunnelDefinitionRepository) GetByID(ctx context.Context, id uint) (*domain.TunnelDefinition, error) {
	var m TunnelDefinitionModel
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("tunnel definition %d not found: %w", id, err)
	}
	return modelToTunnelDef(&m), nil
}

func (r *PGTunnelDefinitionRepository) ListByAgentProfileID(ctx context.Context, agentProfileID uint) ([]*domain.TunnelDefinition, error) {
	var models []TunnelDefinitionModel
	if err := r.db.WithContext(ctx).Where("agent_profile_id = ?", agentProfileID).Find(&models).Error; err != nil {
		return nil, err
	}

	result := make([]*domain.TunnelDefinition, 0, len(models))
	for i := range models {
		result = append(result, modelToTunnelDef(&models[i]))
	}
	return result, nil
}

func (r *PGTunnelDefinitionRepository) Update(ctx context.Context, tunnel *domain.TunnelDefinition) error {
	result := r.db.WithContext(ctx).Model(&TunnelDefinitionModel{}).Where("id = ?", tunnel.ID).
		Updates(map[string]interface{}{
			"label":      tunnel.Label,
			"local_addr": tunnel.LocalAddr,
		})
	if result.RowsAffected == 0 {
		return fmt.Errorf("tunnel definition %d not found", tunnel.ID)
	}
	return result.Error
}

func (r *PGTunnelDefinitionRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&TunnelDefinitionModel{}, id)
	if result.RowsAffected == 0 {
		return fmt.Errorf("tunnel definition %d not found", id)
	}
	return result.Error
}

func modelToTunnelDef(m *TunnelDefinitionModel) *domain.TunnelDefinition {
	return &domain.TunnelDefinition{
		ID:             m.ID,
		AgentProfileID: m.AgentProfileID,
		Label:          m.Label,
		LocalAddr:      m.LocalAddr,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

var _ domain.TunnelDefinitionRepository = (*PGTunnelDefinitionRepository)(nil)
