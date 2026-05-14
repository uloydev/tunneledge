package domain

import "time"

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
	CreatedAt time.Time
	UpdatedAt time.Time
}

type TunnelDefinition struct {
	ID             uint
	AgentProfileID uint
	Label          string
	LocalAddr      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
