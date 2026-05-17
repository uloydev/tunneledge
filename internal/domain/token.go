package domain

import (
	"context"
	"time"
)

// RefreshToken represents a long-lived refresh credential issued alongside a
// short-lived JWT access token. It must be stored in a durable backend (Postgres)
// so that revocation survives restarts.
type RefreshToken struct {
	JTI       string // UUID v4, primary key
	UserID    uint
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// RefreshTokenRepository manages the lifecycle of refresh tokens.
type RefreshTokenRepository interface {
	Create(ctx context.Context, token *RefreshToken) error
	GetByJTI(ctx context.Context, jti string) (*RefreshToken, error)
	Revoke(ctx context.Context, jti string) error
	RevokeAllForUser(ctx context.Context, userID uint) error
	CleanupExpired(ctx context.Context) error
}

// RevokedJTI is the revocation record for a short-lived JWT access token.
// It is keyed on the JTI claim and stored until the access token's natural
// expiry time so the table can be pruned safely without false negatives.
type RevokedJTI struct {
	JTI       string
	ExpiresAt time.Time
}

// RevokedJTIRepository checks whether an access token JTI has been explicitly
// revoked (e.g. via logout before the token expired).
type RevokedJTIRepository interface {
	Revoke(ctx context.Context, jti string, expiresAt time.Time) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
	CleanupExpired(ctx context.Context) error
}
