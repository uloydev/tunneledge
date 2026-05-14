package pgstore

import (
	"context"
	"fmt"
	"time"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

type PGSessionRepository struct {
	db *gorm.DB
}

func NewPGSessionRepository(db *gorm.DB) *PGSessionRepository {
	return &PGSessionRepository{db: db}
}

func (r *PGSessionRepository) Register(ctx context.Context, sess *domain.Session) error {
	if sess.TunnelID == "" {
		return fmt.Errorf("tunnel_id is required")
	}

	m := &TunnelSessionModel{
		TunnelID:      sess.TunnelID,
		AgentID:       sess.AgentID,
		PublicHost:    sess.PublicAddr,
		LocalAddr:     sess.LocalAddr,
		RemoteAddr:    sess.RemoteAddr,
		Status:        "active",
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
	}

	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("tunnel already registered: %w", err)
	}

	sess.CreatedAt = m.ConnectedAt
	sess.LastHeartbeat = m.LastHeartbeat
	return nil
}

func (r *PGSessionRepository) Deregister(ctx context.Context, tunnelID string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&TunnelSessionModel{}).
		Where("tunnel_id = ? AND status = ?", tunnelID, "active").
		Updates(map[string]interface{}{
			"status":          "closed",
			"disconnected_at": &now,
		})

	if result.RowsAffected == 0 {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}
	return result.Error
}

func (r *PGSessionRepository) Get(ctx context.Context, tunnelID string) (*domain.Session, error) {
	var m TunnelSessionModel
	if err := r.db.WithContext(ctx).Where("tunnel_id = ? AND status = ?", tunnelID, "active").First(&m).Error; err != nil {
		return nil, fmt.Errorf("tunnel %s not found", tunnelID)
	}

	return &domain.Session{
		TunnelID:      m.TunnelID,
		AgentID:       m.AgentID,
		PublicAddr:    m.PublicHost,
		LocalAddr:     m.LocalAddr,
		RemoteAddr:    m.RemoteAddr,
		CreatedAt:     m.ConnectedAt,
		LastHeartbeat: m.LastHeartbeat,
	}, nil
}

func (r *PGSessionRepository) List(ctx context.Context) ([]*domain.Session, error) {
	var models []TunnelSessionModel
	if err := r.db.WithContext(ctx).Where("status = ?", "active").Find(&models).Error; err != nil {
		return nil, err
	}

	result := make([]*domain.Session, 0, len(models))
	for i := range models {
		result = append(result, &domain.Session{
			TunnelID:      models[i].TunnelID,
			AgentID:       models[i].AgentID,
			PublicAddr:    models[i].PublicHost,
			LocalAddr:     models[i].LocalAddr,
			RemoteAddr:    models[i].RemoteAddr,
			CreatedAt:     models[i].ConnectedAt,
			LastHeartbeat: models[i].LastHeartbeat,
		})
	}
	return result, nil
}

func (r *PGSessionRepository) Heartbeat(ctx context.Context, tunnelID string) error {
	result := r.db.WithContext(ctx).Model(&TunnelSessionModel{}).
		Where("tunnel_id = ? AND status = ?", tunnelID, "active").
		Update("last_heartbeat", time.Now())

	if result.RowsAffected == 0 {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}
	return result.Error
}

func (r *PGSessionRepository) CleanupExpired(ctx context.Context, ttl time.Duration) (int, error) {
	cutoff := time.Now().Add(-ttl)
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&TunnelSessionModel{}).
		Where("status = ? AND last_heartbeat < ?", "active", cutoff).
		Updates(map[string]interface{}{
			"status":          "expired",
			"disconnected_at": &now,
		})

	return int(result.RowsAffected), result.Error
}

var _ domain.SessionRepository = (*PGSessionRepository)(nil)
