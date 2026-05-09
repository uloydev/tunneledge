package auth

import (
	"crypto/subtle"
	"fmt"
	"sync"

	"tunneledge/pkg/errs"
)

type Authenticator interface {
	Authenticate(token string) (agentID string, err error)
}

type TokenAuthenticator struct {
	mu     sync.RWMutex
	tokens map[string]string
}

func NewTokenAuthenticator(tokens map[string]string) *TokenAuthenticator {
	if tokens == nil {
		tokens = make(map[string]string)
	}
	return &TokenAuthenticator{tokens: tokens}
}

func (a *TokenAuthenticator) Authenticate(token string) (string, error) {
	if token == "" {
		return "", errs.New(errs.CodeUnauthorized, "empty token")
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

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
