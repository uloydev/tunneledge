package domain

import (
	"context"
	"time"
)

// AuditEventType identifies the security event category.
type AuditEventType string

const (
	AuditAuthSuccess      AuditEventType = "auth.success"
	AuditAuthFailure      AuditEventType = "auth.failure"
	AuditAuthLockout      AuditEventType = "auth.lockout"
	AuditTokenCreated     AuditEventType = "token.created"
	AuditTokenRotated     AuditEventType = "token.rotated"
	AuditTokenRevoked     AuditEventType = "token.revoked"
	AuditTunnelCreated    AuditEventType = "tunnel.created"
	AuditTunnelDeleted    AuditEventType = "tunnel.deleted"
	AuditOwnershipChanged AuditEventType = "ownership.changed"
	AuditAdminAction      AuditEventType = "admin.action"
	AuditRateLimitHit     AuditEventType = "rate_limit.hit"
)

// AuditEvent is a single immutable security event record.
type AuditEvent struct {
	ID         uint
	EventType  AuditEventType
	ActorType  string // "user", "agent", "system"
	ActorID    string
	TargetType string // "tunnel", "agent_profile", "user", ""
	TargetID   string
	IPAddress  string
	UserAgent  string
	Metadata   map[string]any
	CreatedAt  time.Time
}

// AuditFilter narrows the events returned by AuditRepository.List.
type AuditFilter struct {
	ActorID   string
	EventType AuditEventType
	Since     time.Time
	Until     time.Time
	Limit     int
	Offset    int
}

// AuditRepository is the write-ahead audit event store. It is append-only;
// there are no update or delete methods to preserve log integrity.
type AuditRepository interface {
	Append(ctx context.Context, event *AuditEvent) error
	List(ctx context.Context, filter AuditFilter) ([]AuditEvent, error)
}
