package memstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/pkg/errs"
)

type MemorySessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]*domain.Session
}

func NewMemorySessionRepository() *MemorySessionRepository {
	return &MemorySessionRepository{
		sessions: make(map[string]*domain.Session),
	}
}

func (r *MemorySessionRepository) Register(_ context.Context, session *domain.Session) error {
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

func (r *MemorySessionRepository) Deregister(_ context.Context, tunnelID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[tunnelID]; !exists {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}
	delete(r.sessions, tunnelID)
	return nil
}

func (r *MemorySessionRepository) Get(_ context.Context, tunnelID string) (*domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, exists := r.sessions[tunnelID]
	if !exists {
		return nil, errs.New(errs.CodeNotFound, fmt.Sprintf("tunnel %s not found", tunnelID))
	}
	return session, nil
}

func (r *MemorySessionRepository) List(_ context.Context) ([]*domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*domain.Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		result = append(result, session)
	}
	return result, nil
}

func (r *MemorySessionRepository) Heartbeat(_ context.Context, tunnelID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, exists := r.sessions[tunnelID]
	if !exists {
		return errs.New(errs.CodeNotFound, fmt.Sprintf("tunnel %s not found", tunnelID))
	}
	session.Touch()
	return nil
}

func (r *MemorySessionRepository) CleanupExpired(_ context.Context, ttl time.Duration) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	expired := 0
	for id, session := range r.sessions {
		if session.IsExpired(ttl) {
			delete(r.sessions, id)
			expired++
		}
	}
	return expired, nil
}

var _ domain.SessionRepository = (*MemorySessionRepository)(nil)
