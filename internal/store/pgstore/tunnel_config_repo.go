package pgstore

import (
	"context"
	"fmt"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

type PGTunnelConfigRepository struct {
	db *gorm.DB
}

func NewPGTunnelConfigRepository(db *gorm.DB) *PGTunnelConfigRepository {
	return &PGTunnelConfigRepository{db: db}
}

func (r *PGTunnelConfigRepository) Create(ctx context.Context, tunnel *domain.TunnelConfig) error {
	tunnelType := tunnel.TunnelType
	if tunnelType == "" {
		tunnelType = "tcp"
	}
	m := &TunnelDefinitionModel{
		AgentProfileID: tunnel.AgentProfileID,
		Label:          tunnel.Label,
		LocalAddr:      tunnel.LocalAddr,
		TunnelType:     tunnelType,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("failed to create tunnel definition: %w", err)
	}
	tunnel.ID = m.ID
	tunnel.CreatedAt = m.CreatedAt
	tunnel.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *PGTunnelConfigRepository) GetByID(ctx context.Context, id uint) (*domain.TunnelConfig, error) {
	var m TunnelDefinitionModel
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("tunnel definition %d not found: %w", id, err)
	}
	return modelToTunnelDef(&m), nil
}

func (r *PGTunnelConfigRepository) ListByAgentProfileID(ctx context.Context, agentProfileID uint) ([]*domain.TunnelConfig, error) {
	var models []TunnelDefinitionModel
	if err := r.db.WithContext(ctx).Where("agent_profile_id = ?", agentProfileID).Find(&models).Error; err != nil {
		return nil, err
	}

	result := make([]*domain.TunnelConfig, 0, len(models))
	for i := range models {
		result = append(result, modelToTunnelDef(&models[i]))
	}
	return result, nil
}

func (r *PGTunnelConfigRepository) Update(ctx context.Context, tunnel *domain.TunnelConfig) error {
	result := r.db.WithContext(ctx).Model(&TunnelDefinitionModel{}).Where("id = ?", tunnel.ID).
		Updates(map[string]interface{}{
			"label":       tunnel.Label,
			"local_addr":  tunnel.LocalAddr,
			"tunnel_type": tunnel.TunnelType,
		})
	if result.RowsAffected == 0 {
		return fmt.Errorf("tunnel definition %d not found", tunnel.ID)
	}
	return result.Error
}

func (r *PGTunnelConfigRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&TunnelDefinitionModel{}, id)
	if result.RowsAffected == 0 {
		return fmt.Errorf("tunnel definition %d not found", id)
	}
	return result.Error
}

func modelToTunnelDef(m *TunnelDefinitionModel) *domain.TunnelConfig {
	return &domain.TunnelConfig{
		ID:             m.ID,
		AgentProfileID: m.AgentProfileID,
		Label:          m.Label,
		LocalAddr:      m.LocalAddr,
		TunnelType:     m.TunnelType,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

var _ domain.TunnelConfigRepository = (*PGTunnelConfigRepository)(nil)
