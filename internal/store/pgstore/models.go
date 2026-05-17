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
	UserID    uint   `gorm:"index;not null"`
	Name      string `gorm:"size:128;not null"`
	AgentID   string `gorm:"uniqueIndex;size:128;not null"`
	TokenHash string `gorm:"size:256;not null"`
}

func (AgentProfileModel) TableName() string { return "agent_profiles" }

type TunnelDefinitionModel struct {
	gorm.Model
	AgentProfileID uint   `gorm:"index;not null"`
	Label          string `gorm:"size:64;not null"`
	LocalAddr      string `gorm:"size:256;not null"`
}

func (TunnelDefinitionModel) TableName() string { return "tunnel_definitions" }
