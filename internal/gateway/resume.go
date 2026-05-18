package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// generateResumeToken returns a cryptographically random 64-character hex string
// (32 bytes of entropy) suitable for use as a session resume token.
func generateResumeToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate resume token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
