package transport

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeAuth(t *testing.T) {
	var buf bytes.Buffer
	token := "test-token-123"

	require.NoError(t, EncodeAuth(&buf, token))

	msg, err := DecodeAuth(&buf)
	require.NoError(t, err)
	assert.Equal(t, token, msg.Token)
}

func TestDecodeAuth_WrongType(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0x99, 0x00, 0x04, 't', 'e', 's', 't'})

	_, err := DecodeAuth(buf)
	require.Error(t, err)
}

func TestEncodeDecodeAuthResponse(t *testing.T) {
	tests := []struct {
		name      string
		status    byte
		tunnelID  string
		publicURL string
	}{
		{"success with all fields", AuthStatusOK, "t-abc123", "abc123.tunneledge.dev:443"},
		{"error empty fields", AuthStatusError, "", ""},
		{"success with tunnel only", AuthStatusOK, "t-xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, EncodeAuthResponse(&buf, tt.status, tt.tunnelID, tt.publicURL))

			resp, err := DecodeAuthResponse(&buf)
			require.NoError(t, err)
			assert.Equal(t, tt.status, resp.Status)
			assert.Equal(t, tt.tunnelID, resp.TunnelID)
			assert.Equal(t, tt.publicURL, resp.PublicURL)
		})
	}
}

func TestEncodeHeartbeat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EncodeHeartbeat(&buf))

	assert.Equal(t, []byte{MsgHeartbeat}, buf.Bytes())
}

func TestReadMessageType(t *testing.T) {
	buf := bytes.NewBuffer([]byte{MsgHeartbeat})
	mt, err := ReadMessageType(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(MsgHeartbeat), mt)

	_, err = ReadMessageType(bytes.NewBuffer(nil))
	assert.Equal(t, io.EOF, err)
}

func TestGenerateSelfSignedTLSConfig(t *testing.T) {
	tlsCfg, err := GenerateSelfSignedTLSConfig()
	require.NoError(t, err)
	assert.NotNil(t, tlsCfg)
	assert.NotEmpty(t, tlsCfg.Certificates)
}

func TestGenerateWildcardSelfSignedTLSConfig(t *testing.T) {
	tlsCfg, err := GenerateWildcardSelfSignedTLSConfig("tunneledge.dev")
	require.NoError(t, err)
	assert.NotNil(t, tlsCfg)
	assert.NotEmpty(t, tlsCfg.Certificates)
}
