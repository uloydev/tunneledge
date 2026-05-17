package domain

import (
	"context"
	"time"
)

type User struct {
	ID            uint
	Email         string
	PasswordHash  string
	Name          string
	EmailVerified bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type EmailVerification struct {
	ID        uint
	UserID    uint
	Token     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type AgentProfile struct {
	ID        uint
	UserID    uint
	Name      string
	AgentID   string
	TokenHash string
	// Phase 3 security fields.
	Scopes          []string   // e.g. ["tunnel:connect"]
	TokenExpiresAt  *time.Time // nil = never expires
	LastUsedAt      *time.Time
	FailedAuthCount int
	LockedUntil     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TunnelACL defines an IP-based access control rule for a tunnel.
// Rules are evaluated in order: deny rules first, then allow rules.
// An empty rule set means allow-all.
type TunnelACL struct {
	ID        uint
	TunnelID  string // matches TunnelSession.TunnelID
	ACLType   string // "allow_cidr" or "deny_cidr"
	CIDR      string
	CreatedAt time.Time
}

// TunnelACLRepository manages IP ACLs associated with tunnels.
type TunnelACLRepository interface {
	List(ctx context.Context, tunnelID string) ([]TunnelACL, error)
	Create(ctx context.Context, acl *TunnelACL) error
	Delete(ctx context.Context, id uint) error
}

type TunnelConfig struct {
	ID             uint
	AgentProfileID uint
	Label          string
	LocalAddr      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
