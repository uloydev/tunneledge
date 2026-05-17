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
		Scopes:    scopesToString(agent.Scopes),
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
			"name":             agent.Name,
			"agent_id":         agent.AgentID,
			"token_hash":       agent.TokenHash,
			"scopes":           scopesToString(agent.Scopes),
			"token_expires_at": agent.TokenExpiresAt,
		})
	if result.RowsAffected == 0 {
		return fmt.Errorf("agent profile %d not found", agent.ID)
	}
	return result.Error
}

// UpdateSecurityFields updates auth-related counters and timestamps without
// touching the token hash or other user-controlled fields.
func (r *PGAgentProfileRepository) UpdateSecurityFields(ctx context.Context, agent *domain.AgentProfile) error {
	updates := map[string]interface{}{
		"failed_auth_count": agent.FailedAuthCount,
		"last_used_at":      agent.LastUsedAt,
		"locked_until":      agent.LockedUntil,
	}
	return r.db.WithContext(ctx).Model(&AgentProfileModel{}).Where("id = ?", agent.ID).Updates(updates).Error
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
		ID:              m.ID,
		UserID:          m.UserID,
		Name:            m.Name,
		AgentID:         m.AgentID,
		TokenHash:       m.TokenHash,
		Scopes:          stringToScopes(m.Scopes),
		TokenExpiresAt:  m.TokenExpiresAt,
		LastUsedAt:      m.LastUsedAt,
		FailedAuthCount: m.FailedAuthCount,
		LockedUntil:     m.LockedUntil,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

// scopesToString serialises a scopes slice to a comma-separated string.
func scopesToString(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	result := ""
	for i, s := range scopes {
		if i > 0 {
			result += ","
		}
		result += s
	}
	return result
}

// stringToScopes splits a comma-separated scopes string back into a slice.
func stringToScopes(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if part := s[start:i]; part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}

// ListTokenHashes returns a hash→agentID map from agent_profiles.
// It is used by auth.DBTokenAuthenticator as the canonical token source.
func (r *PGAgentProfileRepository) ListTokenHashes(ctx context.Context) (map[string]string, error) {
	var models []AgentProfileModel
	if err := r.db.WithContext(ctx).Select("agent_id", "token_hash").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("failed to list agent token hashes: %w", err)
	}
	result := make(map[string]string, len(models))
	for _, m := range models {
		if m.TokenHash != "" {
			result[m.TokenHash] = m.AgentID
		}
	}
	return result, nil
}

var _ domain.AgentProfileRepository = (*PGAgentProfileRepository)(nil)
