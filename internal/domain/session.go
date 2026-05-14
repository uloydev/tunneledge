package domain

import (
	"time"
)

type Session struct {
	TunnelID      string
	AgentID       string
	PublicAddr    string
	LocalAddr     string
	RemoteAddr    string
	CreatedAt     time.Time
	LastHeartbeat time.Time
}

func NewSession(tunnelID, agentID, localAddr string) *Session {
	now := time.Now()
	return &Session{
		TunnelID:      tunnelID,
		AgentID:       agentID,
		LocalAddr:     localAddr,
		CreatedAt:     now,
		LastHeartbeat: now,
	}
}

func (s *Session) IsExpired(ttl time.Duration) bool {
	return time.Since(s.LastHeartbeat) > ttl
}

func (s *Session) Touch() {
	s.LastHeartbeat = time.Now()
}
