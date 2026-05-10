package domain

import (
	"fmt"
	"sync"
	"time"

	"tunneledge/pkg/errs"
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

type SessionRepository interface {
	Register(session *Session) error
	Deregister(tunnelID string) error
	Get(tunnelID string) (*Session, error)
	List() []*Session
	Heartbeat(tunnelID string) error
	CleanupExpired(ttl time.Duration) int
}

type MemorySessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewMemorySessionRepository() *MemorySessionRepository {
	return &MemorySessionRepository{
		sessions: make(map[string]*Session),
	}
}

func (r *MemorySessionRepository) Register(session *Session) error {
	if session.TunnelID == "" {
		return errs.New(errs.CodeInvalidArg, "tunnel_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[session.TunnelID]; exists {
		return errs.Wrap(errs.CodeAlreadyExists, "tunnel already registered", nil)
	}

	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	if session.LastHeartbeat.IsZero() {
		session.LastHeartbeat = time.Now()
	}
	r.sessions[session.TunnelID] = session
	return nil
}

func (r *MemorySessionRepository) Deregister(tunnelID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[tunnelID]; !exists {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}
	delete(r.sessions, tunnelID)
	return nil
}

func (r *MemorySessionRepository) Get(tunnelID string) (*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, exists := r.sessions[tunnelID]
	if !exists {
		return nil, errs.New(errs.CodeNotFound, fmt.Sprintf("tunnel %s not found", tunnelID))
	}
	return session, nil
}

func (r *MemorySessionRepository) List() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		result = append(result, session)
	}
	return result
}

func (r *MemorySessionRepository) Heartbeat(tunnelID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, exists := r.sessions[tunnelID]
	if !exists {
		return errs.New(errs.CodeNotFound, fmt.Sprintf("tunnel %s not found", tunnelID))
	}
	session.Touch()
	return nil
}

func (r *MemorySessionRepository) CleanupExpired(ttl time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	expired := 0
	for id, session := range r.sessions {
		if session.IsExpired(ttl) {
			delete(r.sessions, id)
			expired++
		}
	}
	return expired
}
