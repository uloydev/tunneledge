package auth

import (
	"crypto/subtle"
	"fmt"
	"sync"

	"tunneledge/pkg/errs"

	"golang.org/x/crypto/bcrypt"
)

type Authenticator interface {
	Authenticate(token string) (agentID string, err error)
}

type TokenAuthenticator struct {
	mu     sync.RWMutex
	tokens map[string]string // plaintext token → agentID (legacy)
	hashes map[string]string // bcrypt hash → agentID
}

func NewTokenAuthenticator(tokens map[string]string) *TokenAuthenticator {
	if tokens == nil {
		tokens = make(map[string]string)
	}
	return &TokenAuthenticator{
		tokens: tokens,
		hashes: make(map[string]string),
	}
}

func NewHashedTokenAuthenticator(hashes map[string]string) *TokenAuthenticator {
	if hashes == nil {
		hashes = make(map[string]string)
	}
	return &TokenAuthenticator{
		tokens: make(map[string]string),
		hashes: hashes,
	}
}

func (a *TokenAuthenticator) Authenticate(token string) (string, error) {
	if token == "" {
		return "", errs.New(errs.CodeUnauthorized, "empty token")
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	// Check bcrypt hashes first
	for hash, agentID := range a.hashes {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil {
			return agentID, nil
		}
	}

	// Fallback to constant-time plaintext comparison (legacy/dev)
	for storedToken, agentID := range a.tokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(storedToken)) == 1 {
			return agentID, nil
		}
	}

	return "", errs.New(errs.CodeUnauthorized, "invalid token")
}

func (a *TokenAuthenticator) AddToken(token, agentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokens[token] = agentID
}

func (a *TokenAuthenticator) AddHashedToken(hash, agentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hashes[hash] = agentID
}

func (a *TokenAuthenticator) RemoveToken(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.tokens, token)
}

func LoadTokensFromSlice(pairs []string) (map[string]string, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("token pairs must be even (token, agentID): got %d items", len(pairs))
	}
	tokens := make(map[string]string, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		tokens[pairs[i]] = pairs[i+1]
	}
	return tokens, nil
}

func HashToken(token string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash token: %w", err)
	}
	return string(hash), nil
}
