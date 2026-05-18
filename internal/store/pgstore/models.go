package pgstore

import (
	"time"

	"gorm.io/gorm"
)

type TokenModel struct {
	gorm.Model
	AgentID   string `gorm:"uniqueIndex;size:128;not null"`
	TokenHash string `gorm:"size:256;not null"`
}

func (TokenModel) TableName() string { return "agent_tokens" }

type TunnelSessionModel struct {
	gorm.Model
	TunnelID       string `gorm:"index;size:128;not null"`
	AgentID        string `gorm:"size:128;not null"`
	OwnerRelayID   string `gorm:"index;size:128"`
	LeaseID        string `gorm:"index;size:128"`
	PublicHost     string `gorm:"size:256"`
	LocalAddr      string `gorm:"size:256"`
	RemoteAddr     string `gorm:"size:256"`
	Status         string `gorm:"size:32;index"`
	ConnectedAt    time.Time
	DisconnectedAt *time.Time
	LastHeartbeat  time.Time
	// Phase 4 session resume.
	ResumeToken    string     `gorm:"index;size:512"`
	ResumeDeadline *time.Time `gorm:"index"`
}

func (TunnelSessionModel) TableName() string { return "tunnel_sessions" }

type RelayModel struct {
	gorm.Model
	RelayID       string `gorm:"uniqueIndex;size:128;not null"`
	AdvertiseAddr string `gorm:"size:256"`
	State         string `gorm:"size:32;index"`
	ActiveTunnels int32
	ActiveStreams int32
	LastSeen      time.Time `gorm:"index"`
	Region        string    `gorm:"size:64;index"`
}

func (RelayModel) TableName() string { return "relay_registry" }

type LeaseModel struct {
	gorm.Model
	LeaseID    string    `gorm:"uniqueIndex;size:128;not null"`
	TunnelID   string    `gorm:"index;size:128;not null"`
	RelayID    string    `gorm:"index;size:128;not null"`
	State      string    `gorm:"size:32;index;not null"`
	ExpiresAt  time.Time `gorm:"index;not null"`
	ReleasedAt *time.Time
	Version    int64 `gorm:"not null;default:1"`
}

func (LeaseModel) TableName() string { return "relay_leases" }

type RelayHealthModel struct {
	gorm.Model
	RelayID                string `gorm:"uniqueIndex;size:128;not null"`
	RTTMillis              int64
	HeartbeatLatencyMillis int64
	ActiveTunnels          int32
	ActiveStreams          int32
	BytesPerSecond         int64
	PacketLossPct          float64
	CPUUtilizationPct      float64
	MemoryUtilizationPct   float64
	RecordedAt             time.Time `gorm:"index;not null"`
	Region                 string    `gorm:"size:64;index"`
}

func (RelayHealthModel) TableName() string { return "relay_health_snapshots" }

type UserModel struct {
	gorm.Model
	Email         string `gorm:"uniqueIndex;size:256;not null"`
	PasswordHash  string `gorm:"size:256;not null"`
	Name          string `gorm:"size:128;not null"`
	EmailVerified bool   `gorm:"default:false;not null"`
}

func (UserModel) TableName() string { return "users" }

type EmailVerificationModel struct {
	gorm.Model
	UserID    uint      `gorm:"index;not null"`
	Token     string    `gorm:"uniqueIndex;size:128;not null"`
	ExpiresAt time.Time `gorm:"not null"`
}

func (EmailVerificationModel) TableName() string { return "email_verifications" }

type AgentProfileModel struct {
	gorm.Model
	UserID          uint   `gorm:"index;not null"`
	Name            string `gorm:"size:128;not null"`
	AgentID         string `gorm:"uniqueIndex;size:128;not null"`
	TokenHash       string `gorm:"size:256;not null"`
	Scopes          string `gorm:"size:512;default:''"`
	TokenExpiresAt  *time.Time
	LastUsedAt      *time.Time
	FailedAuthCount int `gorm:"default:0;not null"`
	LockedUntil     *time.Time
}

func (AgentProfileModel) TableName() string { return "agent_profiles" }

// AuditEventModel persists immutable security audit events.
type AuditEventModel struct {
	ID         uint      `gorm:"primaryKey;autoIncrement"`
	EventType  string    `gorm:"size:64;index;not null"`
	ActorType  string    `gorm:"size:32"`
	ActorID    string    `gorm:"size:128;index"`
	TargetType string    `gorm:"size:32"`
	TargetID   string    `gorm:"size:128"`
	IPAddress  string    `gorm:"size:64"`
	UserAgent  string    `gorm:"size:512"`
	Metadata   string    `gorm:"type:text"` // JSON-encoded map
	CreatedAt  time.Time `gorm:"index;not null"`
}

func (AuditEventModel) TableName() string { return "audit_events" }

// RefreshTokenModel persists long-lived refresh tokens linked to a user.
type RefreshTokenModel struct {
	JTI       string    `gorm:"primaryKey;size:128;not null"`
	UserID    uint      `gorm:"index;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	RevokedAt *time.Time
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (RefreshTokenModel) TableName() string { return "refresh_tokens" }

// RevokedJTIModel records JWT access token JTIs that have been explicitly
// revoked (e.g. via logout) before their natural expiry.
type RevokedJTIModel struct {
	JTI       string    `gorm:"primaryKey;size:128;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
}

func (RevokedJTIModel) TableName() string { return "revoked_jtis" }

// TunnelACLModel defines an IP-based access control rule for a tunnel.
type TunnelACLModel struct {
	gorm.Model
	TunnelID string `gorm:"index;size:128;not null"`
	ACLType  string `gorm:"size:32;not null"` // "allow_cidr" or "deny_cidr"
	CIDR     string `gorm:"size:64;not null"`
}

func (TunnelACLModel) TableName() string { return "tunnel_acls" }

type TunnelDefinitionModel struct {
	gorm.Model
	AgentProfileID uint   `gorm:"index;not null"`
	Label          string `gorm:"size:64;not null"`
	LocalAddr      string `gorm:"size:256;not null"`
	TunnelType     string `gorm:"size:16;default:'tcp'"`
}

func (TunnelDefinitionModel) TableName() string { return "tunnel_definitions" }
