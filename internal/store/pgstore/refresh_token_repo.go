package pgstore

import (
	"context"
	"time"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

// PGRefreshTokenRepository implements domain.RefreshTokenRepository.
type PGRefreshTokenRepository struct{ db *gorm.DB }

func NewRefreshTokenRepository(db *gorm.DB) *PGRefreshTokenRepository {
	return &PGRefreshTokenRepository{db: db}
}

func (r *PGRefreshTokenRepository) Create(ctx context.Context, token *domain.RefreshToken) error {
	m := &RefreshTokenModel{
		JTI:       token.JTI,
		UserID:    token.UserID,
		ExpiresAt: token.ExpiresAt,
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *PGRefreshTokenRepository) GetByJTI(ctx context.Context, jti string) (*domain.RefreshToken, error) {
	var m RefreshTokenModel
	if err := r.db.WithContext(ctx).Where("jti = ?", jti).First(&m).Error; err != nil {
		return nil, err
	}
	return &domain.RefreshToken{
		JTI:       m.JTI,
		UserID:    m.UserID,
		ExpiresAt: m.ExpiresAt,
		RevokedAt: m.RevokedAt,
		CreatedAt: m.CreatedAt,
	}, nil
}

func (r *PGRefreshTokenRepository) Revoke(ctx context.Context, jti string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&RefreshTokenModel{}).
		Where("jti = ?", jti).
		Update("revoked_at", now).Error
}

func (r *PGRefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&RefreshTokenModel{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", now).Error
}

func (r *PGRefreshTokenRepository) CleanupExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).
		Where("expires_at < ?", time.Now()).
		Delete(&RefreshTokenModel{}).Error
}

var _ domain.RefreshTokenRepository = (*PGRefreshTokenRepository)(nil)

// PGRevokedJTIRepository implements domain.RevokedJTIRepository.
type PGRevokedJTIRepository struct{ db *gorm.DB }

func NewRevokedJTIRepository(db *gorm.DB) *PGRevokedJTIRepository {
	return &PGRevokedJTIRepository{db: db}
}

func (r *PGRevokedJTIRepository) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	m := &RevokedJTIModel{JTI: jti, ExpiresAt: expiresAt}
	// Tolerate duplicate revocations (e.g. double-logout).
	return r.db.WithContext(ctx).
		Where(RevokedJTIModel{JTI: jti}).
		FirstOrCreate(m).Error
}

func (r *PGRevokedJTIRepository) IsRevoked(ctx context.Context, jti string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&RevokedJTIModel{}).
		Where("jti = ?", jti).Count(&count).Error
	return count > 0, err
}

func (r *PGRevokedJTIRepository) CleanupExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).
		Where("expires_at < ?", time.Now()).
		Delete(&RevokedJTIModel{}).Error
}

var _ domain.RevokedJTIRepository = (*PGRevokedJTIRepository)(nil)
