package pgstore

import (
	"context"
	"fmt"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

type PGUserRepository struct {
	db *gorm.DB
}

func NewPGUserRepository(db *gorm.DB) *PGUserRepository {
	return &PGUserRepository{db: db}
}

func (r *PGUserRepository) Create(ctx context.Context, user *domain.User) error {
	m := &UserModel{
		Email:         user.Email,
		PasswordHash:  user.PasswordHash,
		Name:          user.Name,
		EmailVerified: user.EmailVerified,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	user.ID = m.ID
	user.CreatedAt = m.CreatedAt
	user.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *PGUserRepository) GetByID(ctx context.Context, id uint) (*domain.User, error) {
	var m UserModel
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("user %d not found: %w", id, err)
	}
	return modelToUser(&m), nil
}

func (r *PGUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var m UserModel
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&m).Error; err != nil {
		return nil, fmt.Errorf("user with email %s not found: %w", email, err)
	}
	return modelToUser(&m), nil
}

func (r *PGUserRepository) Update(ctx context.Context, user *domain.User) error {
	result := r.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", user.ID).
		Updates(map[string]interface{}{
			"email":          user.Email,
			"password_hash":  user.PasswordHash,
			"name":           user.Name,
			"email_verified": user.EmailVerified,
		})
	if result.RowsAffected == 0 {
		return fmt.Errorf("user %d not found", user.ID)
	}
	return result.Error
}

func (r *PGUserRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&UserModel{}, id)
	if result.RowsAffected == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return result.Error
}

func modelToUser(m *UserModel) *domain.User {
	return &domain.User{
		ID:            m.ID,
		Email:         m.Email,
		PasswordHash:  m.PasswordHash,
		Name:          m.Name,
		EmailVerified: m.EmailVerified,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}

var _ domain.UserRepository = (*PGUserRepository)(nil)
