package memstore

import (
	"context"
	"fmt"
	"sync"

	"tunneledge/internal/domain"
)

type MemoryTokenRepository struct {
	mu     sync.RWMutex
	tokens map[string]string // agentID → tokenHash
}

func NewMemoryTokenRepository() *MemoryTokenRepository {
	return &MemoryTokenRepository{
		tokens: make(map[string]string),
	}
}

func (r *MemoryTokenRepository) Create(_ context.Context, agentID, tokenHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens[agentID] = tokenHash
	return nil
}

func (r *MemoryTokenRepository) GetByAgentID(_ context.Context, agentID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hash, ok := r.tokens[agentID]
	if !ok {
		return "", fmt.Errorf("token not found for agent %s", agentID)
	}
	return hash, nil
}

func (r *MemoryTokenRepository) List(_ context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string, len(r.tokens))
	for agentID, hash := range r.tokens {
		result[hash] = agentID
	}
	return result, nil
}

func (r *MemoryTokenRepository) Delete(_ context.Context, agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tokens, agentID)
	return nil
}

var _ domain.TokenRepository = (*MemoryTokenRepository)(nil)
