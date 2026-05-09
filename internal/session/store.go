package session

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

func (s *Session) IsExpired(ttl time.Duration) bool {
	return time.Since(s.LastHeartbeat) > ttl
}

type Store interface {
	Register(session *Session) error
	Deregister(tunnelID string) error
	Get(tunnelID string) (*Session, error)
	List() []*Session
	Heartbeat(tunnelID string) error
	CleanupExpired(ttl time.Duration) int
}

type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]*Session),
	}
}

func (s *MemoryStore) Register(session *Session) error {
	if session.TunnelID == "" {
		return errs.New(errs.CodeInvalidArg, "tunnel_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.TunnelID]; exists {
		return errs.Wrap(errs.CodeAlreadyExists, "tunnel already registered", nil)
	}

	session.CreatedAt = time.Now()
	session.LastHeartbeat = time.Now()
	s.sessions[session.TunnelID] = session

	return nil
}

func (s *MemoryStore) Deregister(tunnelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[tunnelID]; !exists {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}

	delete(s.sessions, tunnelID)
	return nil
}

func (s *MemoryStore) Get(tunnelID string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[tunnelID]
	if !exists {
		return nil, errs.New(errs.CodeNotFound, fmt.Sprintf("tunnel %s not found", tunnelID))
	}

	return session, nil
}

func (s *MemoryStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, session)
	}
	return result
}

func (s *MemoryStore) Heartbeat(tunnelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[tunnelID]
	if !exists {
		return errs.New(errs.CodeNotFound, fmt.Sprintf("tunnel %s not found", tunnelID))
	}

	session.LastHeartbeat = time.Now()
	return nil
}

func (s *MemoryStore) CleanupExpired(ttl time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := 0
	for id, session := range s.sessions {
		if session.IsExpired(ttl) {
			delete(s.sessions, id)
			expired++
		}
	}
	return expired
}
