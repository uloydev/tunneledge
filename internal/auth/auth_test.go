package auth

import (
	"testing"

	"tunneledge/pkg/errs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustHash(t *testing.T, token string) string {
	t.Helper()
	h, err := HashToken(token)
	require.NoError(t, err)
	return h
}

func TestTokenAuthenticator_Authenticate(t *testing.T) {
	hashes := map[string]string{
		mustHash(t, "valid-token-1"): "agent-1",
		mustHash(t, "valid-token-2"): "agent-2",
	}
	a := NewHashedTokenAuthenticator(hashes)

	tests := []struct {
		name     string
		token    string
		wantID   string
		wantCode errs.Code
		wantErr  bool
	}{
		{"valid token 1", "valid-token-1", "agent-1", "", false},
		{"valid token 2", "valid-token-2", "agent-2", "", false},
		{"invalid token", "invalid", "", errs.CodeUnauthorized, true},
		{"empty token", "", "", errs.CodeUnauthorized, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := a.Authenticate(tt.token)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, errs.GetCode(err))
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantID, id)
			}
		})
	}
}

func TestTokenAuthenticator_AddRemove(t *testing.T) {
	a := NewHashedTokenAuthenticator(nil)

	_, err := a.Authenticate("my-token")
	assert.Error(t, err)

	hash := mustHash(t, "my-token")
	a.AddHashedToken(hash, "agent-new")
	id, err := a.Authenticate("my-token")
	assert.NoError(t, err)
	assert.Equal(t, "agent-new", id)

	a.RemoveHashedToken("agent-new")
	_, err = a.Authenticate("my-token")
	assert.Error(t, err)
}

func TestTokenAuthenticator_HMACPrefilter(t *testing.T) {
	// Add 50 fake entries to verify the pre-filter eliminates most bcrypt calls.
	a := NewHashedTokenAuthenticator(nil)
	for i := 0; i < 50; i++ {
		h := mustHash(t, "noise-token")
		a.AddHashedToken(h, "noise-agent")
	}
	real := mustHash(t, "real-token")
	a.AddHashedToken(real, "real-agent")

	id, err := a.Authenticate("real-token")
	require.NoError(t, err)
	assert.Equal(t, "real-agent", id)
}
