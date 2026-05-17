package pgstore

import (
	"context"
	"fmt"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

// PGTunnelACLRepository implements domain.TunnelACLRepository.
type PGTunnelACLRepository struct{ db *gorm.DB }

func NewTunnelACLRepository(db *gorm.DB) *PGTunnelACLRepository {
	return &PGTunnelACLRepository{db: db}
}

func (r *PGTunnelACLRepository) List(ctx context.Context, tunnelID string) ([]domain.TunnelACL, error) {
	var models []TunnelACLModel
	if err := r.db.WithContext(ctx).Where("tunnel_id = ?", tunnelID).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("failed to list tunnel ACLs: %w", err)
	}
	out := make([]domain.TunnelACL, 0, len(models))
	for _, m := range models {
		out = append(out, domain.TunnelACL{
			ID:        m.ID,
			TunnelID:  m.TunnelID,
			ACLType:   m.ACLType,
			CIDR:      m.CIDR,
			CreatedAt: m.CreatedAt,
		})
	}
	return out, nil
}

func (r *PGTunnelACLRepository) Create(ctx context.Context, acl *domain.TunnelACL) error {
	m := &TunnelACLModel{
		TunnelID: acl.TunnelID,
		ACLType:  acl.ACLType,
		CIDR:     acl.CIDR,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("failed to create tunnel ACL: %w", err)
	}
	acl.ID = m.ID
	acl.CreatedAt = m.CreatedAt
	return nil
}

func (r *PGTunnelACLRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&TunnelACLModel{}, id)
	if result.RowsAffected == 0 {
		return fmt.Errorf("tunnel ACL %d not found", id)
	}
	return result.Error
}

var _ domain.TunnelACLRepository = (*PGTunnelACLRepository)(nil)
