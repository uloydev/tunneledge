package pgstore

import (
	"fmt"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/internal/store"

	"gorm.io/gorm"
)

type PGSessionRepository struct {
	db *gorm.DB
}

func NewPGSessionRepository(db *gorm.DB) *PGSessionRepository {
	return &PGSessionRepository{db: db}
}

func (r *PGSessionRepository) Register(sess *domain.Session) error {
	if sess.TunnelID == "" {
		return fmt.Errorf("tunnel_id is required")
	}

	ts := &store.TunnelSession{
		TunnelID:      sess.TunnelID,
		AgentID:       sess.AgentID,
		PublicHost:    sess.PublicAddr,
		LocalAddr:     sess.LocalAddr,
		RemoteAddr:    sess.RemoteAddr,
		Status:        "active",
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
	}

	if err := r.db.Create(ts).Error; err != nil {
		return fmt.Errorf("tunnel already registered: %w", err)
	}

	sess.CreatedAt = ts.ConnectedAt
	sess.LastHeartbeat = ts.LastHeartbeat
	return nil
}

func (r *PGSessionRepository) Deregister(tunnelID string) error {
	now := time.Now()
	result := r.db.Model(&store.TunnelSession{}).
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

func (r *PGSessionRepository) Get(tunnelID string) (*domain.Session, error) {
	var ts store.TunnelSession
	if err := r.db.Where("tunnel_id = ? AND status = ?", tunnelID, "active").First(&ts).Error; err != nil {
		return nil, fmt.Errorf("tunnel %s not found", tunnelID)
	}

	return &domain.Session{
		TunnelID:      ts.TunnelID,
		AgentID:       ts.AgentID,
		PublicAddr:    ts.PublicHost,
		LocalAddr:     ts.LocalAddr,
		RemoteAddr:    ts.RemoteAddr,
		CreatedAt:     ts.ConnectedAt,
		LastHeartbeat: ts.LastHeartbeat,
	}, nil
}

func (r *PGSessionRepository) List() []*domain.Session {
	var tunnels []store.TunnelSession
	r.db.Where("status = ?", "active").Find(&tunnels)

	result := make([]*domain.Session, 0, len(tunnels))
	for i := range tunnels {
		result = append(result, &domain.Session{
			TunnelID:      tunnels[i].TunnelID,
			AgentID:       tunnels[i].AgentID,
			PublicAddr:    tunnels[i].PublicHost,
			LocalAddr:     tunnels[i].LocalAddr,
			RemoteAddr:    tunnels[i].RemoteAddr,
			CreatedAt:     tunnels[i].ConnectedAt,
			LastHeartbeat: tunnels[i].LastHeartbeat,
		})
	}
	return result
}

func (r *PGSessionRepository) Heartbeat(tunnelID string) error {
	result := r.db.Model(&store.TunnelSession{}).
		Where("tunnel_id = ? AND status = ?", tunnelID, "active").
		Update("last_heartbeat", time.Now())

	if result.RowsAffected == 0 {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}
	return result.Error
}

func (r *PGSessionRepository) CleanupExpired(ttl time.Duration) int {
	cutoff := time.Now().Add(-ttl)
	now := time.Now()
	result := r.db.Model(&store.TunnelSession{}).
		Where("status = ? AND last_heartbeat < ?", "active", cutoff).
		Updates(map[string]interface{}{
			"status":          "expired",
			"disconnected_at": &now,
		})

	return int(result.RowsAffected)
}
