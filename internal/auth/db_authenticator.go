package auth

import (
	"context"
	"fmt"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/pkg/errs"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
)

// TokenLookup is the minimal interface the DBTokenAuthenticator needs.
// pgstore.PGAgentProfileRepository satisfies this interface.
type TokenLookup interface {
	// ListTokenHashes returns all (bcryptHash → agentID) pairs.
	ListTokenHashes(ctx context.Context) (map[string]string, error)
}

// ProfileLookup extends TokenLookup to support per-profile security checks
// (token expiry, lockout, and last-used tracking). Implementing this interface
// is optional; if the store does not implement it the extra checks are skipped.
type ProfileLookup interface {
	GetByAgentID(ctx context.Context, agentID string) (*domain.AgentProfile, error)
	UpdateSecurityFields(ctx context.Context, profile *domain.AgentProfile) error
}

// LockoutConfig controls failed-attempt tracking and temporary lockout.
type LockoutConfig struct {
	MaxFailedAttempts int
	LockoutDuration   time.Duration
}

// DefaultLockoutConfig is used when no LockoutConfig is provided.
var DefaultLockoutConfig = LockoutConfig{
	MaxFailedAttempts: 10,
	LockoutDuration:   15 * time.Minute,
}

// DBTokenAuthenticator queries the token store on every authentication
// request so that tokens created after gateway startup are immediately valid
// without requiring a restart.
type DBTokenAuthenticator struct {
	store   TokenLookup
	profile ProfileLookup // optional; nil = no expiry/lockout checks
	lockout LockoutConfig
}

// NewDBTokenAuthenticator creates an Authenticator backed by a live token store.
func NewDBTokenAuthenticator(store TokenLookup) *DBTokenAuthenticator {
	a := &DBTokenAuthenticator{store: store, lockout: DefaultLockoutConfig}
	if pl, ok := store.(ProfileLookup); ok {
		a.profile = pl
	}
	return a
}

// WithLockoutConfig sets a custom lockout policy.
func (a *DBTokenAuthenticator) WithLockoutConfig(cfg LockoutConfig) *DBTokenAuthenticator {
	a.lockout = cfg
	return a
}

// Authenticate looks up all token hashes from the store and returns the
// agentID for the first hash that matches token.
func (a *DBTokenAuthenticator) Authenticate(token string) (string, error) {
	if token == "" {
		return "", errs.New(errs.CodeUnauthorized, "empty token")
	}

	ctx := context.Background()

	hashes, err := a.store.ListTokenHashes(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to load token hashes for authentication")
		return "", fmt.Errorf("failed to load tokens: %w", err)
	}

	for hash, agentID := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) != nil {
			continue
		}

		// Bcrypt match — apply security checks if a profile store is available.
		if a.profile != nil {
			profile, pErr := a.profile.GetByAgentID(ctx, agentID)
			if pErr != nil {
				log.Error().Err(pErr).Str("agent_id", agentID).Msg("failed to load agent profile for security check")
				return "", errs.New(errs.CodeUnauthorized, "invalid token")
			}

			// Lockout check.
			if profile.LockedUntil != nil && profile.LockedUntil.After(time.Now()) {
				log.Warn().Str("agent_id", agentID).Time("locked_until", *profile.LockedUntil).Msg("agent token is locked out")
				return "", errs.New(errs.CodeUnauthorized, "account temporarily locked")
			}

			// Token expiry check.
			if profile.TokenExpiresAt != nil && profile.TokenExpiresAt.Before(time.Now()) {
				log.Warn().Str("agent_id", agentID).Time("expired_at", *profile.TokenExpiresAt).Msg("agent token expired")
				return "", errs.New(errs.CodeUnauthorized, "token expired")
			}

			// Update last-used timestamp and reset failure counter (fire-and-forget).
			now := time.Now()
			profile.LastUsedAt = &now
			profile.FailedAuthCount = 0
			profile.LockedUntil = nil
			if uErr := a.profile.UpdateSecurityFields(ctx, profile); uErr != nil {
				log.Warn().Err(uErr).Str("agent_id", agentID).Msg("failed to update last_used_at")
			}
		}

		return agentID, nil
	}

	// No match — increment failure counter if profile store is available.
	log.Warn().Msg("authentication failed: no matching token hash")

	if a.profile != nil && a.lockout.MaxFailedAttempts > 0 {
		a.recordAuthFailure(ctx, hashes)
	}

	return "", errs.New(errs.CodeUnauthorized, "invalid token")
}

// recordAuthFailure increments the failed-auth counter on all profiles whose
// token didn't match (we cannot identify which profile the bad token targeted,
// so we look up by the raw token structure — but since we can't know, we skip
// per-profile failure tracking here; gateway-level per-IP rate limiting handles
// brute-force mitigation instead).
//
// This method exists as an extension point for future per-profile tracking
// when a separate "token → profile" lookup is available.
func (a *DBTokenAuthenticator) recordAuthFailure(_ context.Context, _ map[string]string) {
	// Per-profile failure tracking requires knowing which profile was targeted.
	// Without a dedicated "get profile by token prefix" index, we defer this to
	// the IP-level rate limiter in the gateway (Phase D). No-op here.
}
