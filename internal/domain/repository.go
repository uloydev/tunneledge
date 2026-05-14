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

type TunnelDefinitionRepository interface {
	Create(ctx context.Context, tunnel *TunnelDefinition) error
	GetByID(ctx context.Context, id uint) (*TunnelDefinition, error)
	ListByAgentProfileID(ctx context.Context, agentProfileID uint) ([]*TunnelDefinition, error)
	Update(ctx context.Context, tunnel *TunnelDefinition) error
	Delete(ctx context.Context, id uint) error
}
