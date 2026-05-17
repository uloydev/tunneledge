package dashboard

import "time"

// Auth DTOs

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

type UserResponse struct {
	ID        uint      `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Agent DTOs

type CreateAgentRequest struct {
	Name    string `json:"name"`
	AgentID string `json:"agent_id"`
}

type UpdateAgentRequest struct {
	Name string `json:"name"`
}

type AgentResponse struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	AgentID   string    `json:"agent_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Phase 3 security fields (omitted when zero)
	TokenExpiresAt  *time.Time `json:"token_expires_at,omitempty"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	FailedAuthCount int        `json:"failed_auth_count,omitempty"`
	LockedUntil     *time.Time `json:"locked_until,omitempty"`
}

type AgentTokenResponse struct {
	AgentID string `json:"agent_id"`
	Token   string `json:"token"`
}

// Tunnel ACL DTOs

type CreateTunnelACLRequest struct {
	CIDR    string `json:"cidr"`
	ACLType string `json:"acl_type"` // "allow" or "deny"
}

type TunnelACLResponse struct {
	ID        uint      `json:"id"`
	TunnelID  string    `json:"tunnel_id"`
	ACLType   string    `json:"acl_type"`
	CIDR      string    `json:"cidr"`
	CreatedAt time.Time `json:"created_at"`
}

// Tunnel DTOs

type CreateTunnelRequest struct {
	Label     string `json:"label"`
	LocalAddr string `json:"local_addr"`
}

type UpdateTunnelRequest struct {
	Label     string `json:"label"`
	LocalAddr string `json:"local_addr"`
}

type TunnelResponse struct {
	ID        uint      `json:"id"`
	Label     string    `json:"label"`
	LocalAddr string    `json:"local_addr"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Status DTOs

type SessionResponse struct {
	TunnelID      string `json:"tunnel_id"`
	AgentID       string `json:"agent_id"`
	PublicAddr    string `json:"public_addr"`
	LocalAddr     string `json:"local_addr"`
	CreatedAt     int64  `json:"created_at"`
	LastHeartbeat int64  `json:"last_heartbeat"`
}
