package auth

import (
	"context"
	"fmt"

	"tunneledge/pkg/errs"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
)

// TokenLookup is the minimal interface the DBTokenAuthenticator needs.
// pgstore.PGAgentProfileRepository satisfies this via ListTokenHashes,
// which reads from agent_profiles — the single authoritative token store.
type TokenLookup interface {
	// ListTokenHashes returns all (bcryptHash → agentID) pairs.
	ListTokenHashes(ctx context.Context) (map[string]string, error)
}

// DBTokenAuthenticator queries the token store on every authentication
// request so that tokens created after gateway startup are immediately valid
// without requiring a restart.
//
// bcrypt comparisons are intentionally sequential to avoid amplifying CPU
// work in case of many tokens; for fleets with hundreds of agents consider
// adding an HMAC pre-filter index.
type DBTokenAuthenticator struct {
	store TokenLookup
}

// NewDBTokenAuthenticator creates an Authenticator backed by a live token store.
func NewDBTokenAuthenticator(store TokenLookup) *DBTokenAuthenticator {
	return &DBTokenAuthenticator{store: store}
}

// Authenticate looks up all token hashes from the store and returns the
// agentID for the first hash that matches token.
func (a *DBTokenAuthenticator) Authenticate(token string) (string, error) {
	if token == "" {
		return "", errs.New(errs.CodeUnauthorized, "empty token")
	}

	hashes, err := a.store.ListTokenHashes(context.Background())
	if err != nil {
		log.Error().Err(err).Msg("failed to load token hashes for authentication")
		return "", fmt.Errorf("failed to load tokens: %w", err)
	}

	for hash, agentID := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil {
			return agentID, nil
		}
	}

	log.Warn().Msg("authentication failed: no matching token hash")
	return "", errs.New(errs.CodeUnauthorized, "invalid token")
}
