package domain

import (
	"context"
	"time"
)

type TokenRepository interface {
	Create(ctx context.Context, agentID, tokenHash string) error
	GetByAgentID(ctx context.Context, agentID string) (tokenHash string, err error)
	List(ctx context.Context) (map[string]string, error) // hash → agentID
	Delete(ctx context.Context, agentID string) error
}

type SessionRepository interface {
	Register(ctx context.Context, session *Session) error
	Deregister(ctx context.Context, tunnelID string) error
	Get(ctx context.Context, tunnelID string) (*Session, error)
	List(ctx context.Context) ([]*Session, error)
	Heartbeat(ctx context.Context, tunnelID string) error
	CleanupExpired(ctx context.Context, ttl time.Duration) (int, error)
	// Phase 4 session resume.
	GetResumable(ctx context.Context, token string) (*Session, error)
	SetResumable(ctx context.Context, tunnelID, token string, deadline time.Time) error
}

type RelayRepository interface {
	Upsert(ctx context.Context, relay *RelayInfo) error
	Get(ctx context.Context, relayID string) (*RelayInfo, error)
	List(ctx context.Context) ([]*RelayInfo, error)
	Delete(ctx context.Context, relayID string) error
}

type LeaseRepository interface {
	Upsert(ctx context.Context, lease *Lease) error
	Get(ctx context.Context, leaseID string) (*Lease, error)
	GetByTunnelID(ctx context.Context, tunnelID string) (*Lease, error)
	ListByRelay(ctx context.Context, relayID string) ([]*Lease, error)
	Delete(ctx context.Context, leaseID string) error
	DeleteExpired(ctx context.Context, cutoff time.Time) (int, error)
}

type RelayHealthRepository interface {
	Upsert(ctx context.Context, relayID string, health *RelayHealth) error
	Get(ctx context.Context, relayID string) (*RelayHealth, error)
	List(ctx context.Context) (map[string]*RelayHealth, error)
	Delete(ctx context.Context, relayID string) error
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uint) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id uint) error
}

type EmailVerificationRepository interface {
	Create(ctx context.Context, v *EmailVerification) error
	GetByToken(ctx context.Context, token string) (*EmailVerification, error)
	DeleteByUserID(ctx context.Context, userID uint) error
}

type AgentProfileRepository interface {
	Create(ctx context.Context, agent *AgentProfile) error
	GetByID(ctx context.Context, id uint) (*AgentProfile, error)
	GetByAgentID(ctx context.Context, agentID string) (*AgentProfile, error)
	ListByUserID(ctx context.Context, userID uint) ([]*AgentProfile, error)
	Update(ctx context.Context, agent *AgentProfile) error
	Delete(ctx context.Context, id uint) error
}

type TunnelConfigRepository interface {
	Create(ctx context.Context, tunnel *TunnelConfig) error
	GetByID(ctx context.Context, id uint) (*TunnelConfig, error)
	ListByAgentProfileID(ctx context.Context, agentProfileID uint) ([]*TunnelConfig, error)
	Update(ctx context.Context, tunnel *TunnelConfig) error
	Delete(ctx context.Context, id uint) error
}
