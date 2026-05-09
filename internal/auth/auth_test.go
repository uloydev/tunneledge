package auth

import (
	"testing"

	"tunneledge/pkg/errs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenAuthenticator_Authenticate(t *testing.T) {
	tokens := map[string]string{
		"valid-token-1": "agent-1",
		"valid-token-2": "agent-2",
	}
	auth := NewTokenAuthenticator(tokens)

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
			id, err := auth.Authenticate(tt.token)
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
	auth := NewTokenAuthenticator(map[string]string{})

	_, err := auth.Authenticate("my-token")
	assert.Error(t, err)

	auth.AddToken("my-token", "agent-new")
	id, err := auth.Authenticate("my-token")
	assert.NoError(t, err)
	assert.Equal(t, "agent-new", id)

	auth.RemoveToken("my-token")
	_, err = auth.Authenticate("my-token")
	assert.Error(t, err)
}

func TestLoadTokensFromSlice(t *testing.T) {
	tests := []struct {
		name    string
		pairs   []string
		wantLen int
		wantErr bool
	}{
		{"valid pairs", []string{"t1", "a1", "t2", "a2"}, 2, false},
		{"empty", []string{}, 0, false},
		{"odd count", []string{"t1"}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := LoadTokensFromSlice(tt.pairs)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, tokens, tt.wantLen)
			}
		})
	}
}
