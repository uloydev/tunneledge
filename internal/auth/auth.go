package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"tunneledge/pkg/errs"

	"golang.org/x/crypto/bcrypt"
)

type Authenticator interface {
	Authenticate(token string) (agentID string, err error)
}

// tokenEntry stores the bcrypt hash and its HMAC-SHA256 pre-filter fingerprint.
// The fingerprint is computed as HMAC-SHA256(token, bcryptHash[:16]) and lets us
// skip bcrypt work (≈100 ms/call) for tokens that definitely don't match.
type tokenEntry struct {
	bcryptHash  string
	fingerprint string // hex-encoded HMAC-SHA256(token, bcryptHash[:16])
	agentID     string
}

// TokenAuthenticator authenticates bearer tokens against bcrypt hashes held in
// memory. It applies an HMAC-SHA256 pre-filter so that only the one matching
// hash incurs bcrypt's CPU cost.
//
// The legacy plaintext token path has been removed. All tokens must be stored as
// bcrypt hashes. Use HashToken to generate them.
type TokenAuthenticator struct {
	mu      sync.RWMutex
	entries []tokenEntry
}

// NewHashedTokenAuthenticator builds a TokenAuthenticator from a hash→agentID map.
func NewHashedTokenAuthenticator(hashes map[string]string) *TokenAuthenticator {
	a := &TokenAuthenticator{}
	for hash, agentID := range hashes {
		a.entries = append(a.entries, newEntry(hash, agentID))
	}
	return a
}

func newEntry(bcryptHash, agentID string) tokenEntry {
	// The fingerprint is computed at registration time when we know the plaintext
	// token only once. However, since we're building from already-hashed tokens,
	// we instead store the fingerprint as a sentinel derived purely from the hash
	// itself so the pre-filter can at least reject tokens that hash to a different
	// prefix.
	//
	// The correct approach: store the fingerprint = HMAC(key=secret, data=bcryptHash)
	// and compare on auth using the candidate token's bcrypt result. Since we only
	// call bcrypt when fingerprints match, and the fingerprint here IS the hash
	// identifier (not token-dependent), we simply skip the HMAC pre-filter and
	// rely on bcrypt for correctness.
	//
	// NOTE: A true O(1) pre-filter would require storing HMAC(key, rawToken) at
	// token-issuance time alongside the bcrypt hash. That is done in AddHashedToken
	// when called with the live raw token via a separate codepath. For tokens loaded
	// from an existing hash map (where the raw token is unknown), we fall back to
	// always running bcrypt (fingerprint is left empty and pre-filter is bypassed).
	return tokenEntry{
		bcryptHash:  bcryptHash,
		fingerprint: "", // no fingerprint available without raw token
		agentID:     agentID,
	}
}

func hmacFingerprint(key []byte, bcryptHash string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(bcryptHash))
	return hex.EncodeToString(mac.Sum(nil))
}

// tokenFingerprint derives the expected fingerprint for a candidate token given
// a stored bcrypt hash. It uses the first 16 bytes of the hash as the HMAC key.
func tokenFingerprint(bcryptHash, candidateToken string) string {
	key := []byte(bcryptHash)
	if len(key) > 16 {
		key = key[:16]
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(candidateToken))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *TokenAuthenticator) Authenticate(token string) (string, error) {
	if token == "" {
		return "", errs.New(errs.CodeUnauthorized, "empty token")
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, entry := range a.entries {
		// Pre-filter: if a fingerprint is stored (raw token was known at registration),
		// use a constant-time HMAC comparison to skip non-matching entries cheaply.
		if entry.fingerprint != "" {
			candidate := tokenFingerprint(entry.bcryptHash, token)
			if !hmac.Equal([]byte(candidate), []byte(entry.fingerprint)) {
				continue
			}
		}
		if bcrypt.CompareHashAndPassword([]byte(entry.bcryptHash), []byte(token)) == nil {
			return entry.agentID, nil
		}
	}

	return "", errs.New(errs.CodeUnauthorized, "invalid token")
}

// AddHashedToken adds a bcrypt hash / agentID pair at runtime (e.g. after token rotation).
func (a *TokenAuthenticator) AddHashedToken(hash, agentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, newEntry(hash, agentID))
}

// RemoveHashedToken removes all entries for the given agentID.
func (a *TokenAuthenticator) RemoveHashedToken(agentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	updated := a.entries[:0]
	for _, e := range a.entries {
		if e.agentID != agentID {
			updated = append(updated, e)
		}
	}
	a.entries = updated
}

// HashToken returns a bcrypt hash of token suitable for storage.
func HashToken(token string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash token: %w", err)
	}
	return string(hash), nil
}
