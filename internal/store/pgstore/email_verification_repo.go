package pgstore

import (
	"context"
	"fmt"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

type PGEmailVerificationRepository struct {
	db *gorm.DB
}

func NewPGEmailVerificationRepository(db *gorm.DB) *PGEmailVerificationRepository {
	return &PGEmailVerificationRepository{db: db}
}

func (r *PGEmailVerificationRepository) Create(ctx context.Context, v *domain.EmailVerification) error {
	m := &EmailVerificationModel{
		UserID:    v.UserID,
		Token:     v.Token,
		ExpiresAt: v.ExpiresAt,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("failed to create email verification: %w", err)
	}
	v.ID = m.ID
	v.CreatedAt = m.CreatedAt
	return nil
}

func (r *PGEmailVerificationRepository) GetByToken(ctx context.Context, token string) (*domain.EmailVerification, error) {
	var m EmailVerificationModel
	if err := r.db.WithContext(ctx).Where("token = ?", token).First(&m).Error; err != nil {
		return nil, fmt.Errorf("verification token not found: %w", err)
	}
	return &domain.EmailVerification{
		ID:        m.ID,
		UserID:    m.UserID,
		Token:     m.Token,
		ExpiresAt: m.ExpiresAt,
		CreatedAt: m.CreatedAt,
	}, nil
}

func (r *PGEmailVerificationRepository) DeleteByUserID(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&EmailVerificationModel{}).Error
}

var _ domain.EmailVerificationRepository = (*PGEmailVerificationRepository)(nil)
