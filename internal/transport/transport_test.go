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

func TestGenerateSelfSignedQUICTLSConfig(t *testing.T) {
	tlsCfg, err := GenerateSelfSignedQUICTLSConfig()
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

func TestEncodeDecodeAuthV2(t *testing.T) {
	var buf bytes.Buffer
	tunnels := []TunnelEntry{
		{Label: "web", LocalAddr: "localhost:3000"},
		{Label: "api", LocalAddr: "localhost:8080"},
	}

	require.NoError(t, EncodeAuthV2(&buf, "test-token", tunnels))

	msg, err := DecodeAuthV2(&buf)
	require.NoError(t, err)
	assert.Equal(t, "test-token", msg.Token)
	assert.Len(t, msg.Tunnels, 2)
	assert.Equal(t, "web", msg.Tunnels[0].Label)
	assert.Equal(t, "localhost:3000", msg.Tunnels[0].LocalAddr)
	assert.Equal(t, "api", msg.Tunnels[1].Label)
	assert.Equal(t, "localhost:8080", msg.Tunnels[1].LocalAddr)
}

func TestDecodeAuthV2_WrongType(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0x01, 0x00, 0x04, 't', 'e', 's', 't'})

	_, err := DecodeAuthV2(buf)
	require.Error(t, err)
}

func TestEncodeDecodeAuthV2Response(t *testing.T) {
	var buf bytes.Buffer
	tunnels := []TunnelHostEntry{
		{Label: "web", Hostname: "web-agent-1.tunneledge.dev:443"},
		{Label: "api", Hostname: "api-agent-1.tunneledge.dev:443"},
	}

	require.NoError(t, EncodeAuthV2Response(&buf, AuthStatusOK, "t-agent-1", tunnels))

	resp, err := DecodeAuthV2Response(&buf)
	require.NoError(t, err)
	assert.Equal(t, AuthStatusOK, resp.Status)
	assert.Equal(t, "t-agent-1", resp.TunnelID)
	assert.Len(t, resp.Tunnels, 2)
	assert.Equal(t, "web", resp.Tunnels[0].Label)
	assert.Equal(t, "web-agent-1.tunneledge.dev:443", resp.Tunnels[0].Hostname)
	assert.Equal(t, "api", resp.Tunnels[1].Label)
	assert.Equal(t, "api-agent-1.tunneledge.dev:443", resp.Tunnels[1].Hostname)
}

func TestEncodeDecodeAuthV2Response_Empty(t *testing.T) {
	var buf bytes.Buffer

	require.NoError(t, EncodeAuthV2Response(&buf, AuthStatusError, "", nil))

	resp, err := DecodeAuthV2Response(&buf)
	require.NoError(t, err)
	assert.Equal(t, AuthStatusError, resp.Status)
	assert.Equal(t, "", resp.TunnelID)
	assert.Empty(t, resp.Tunnels)
}

func TestEncodeDecodeStreamLabel(t *testing.T) {
	var buf bytes.Buffer

	require.NoError(t, EncodeStreamLabel(&buf, "web"))

	label, err := DecodeStreamLabel(&buf)
	require.NoError(t, err)
	assert.Equal(t, "web", label)
}

func TestDecodeStreamLabel_WrongType(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0x01, 0x03, 'w', 'e', 'b'})

	_, err := DecodeStreamLabel(buf)
	require.Error(t, err)
}

func TestEncodeDecodeAuthV2_EmptyTunnels(t *testing.T) {
	var buf bytes.Buffer

	require.NoError(t, EncodeAuthV2(&buf, "test-token", nil))

	msg, err := DecodeAuthV2(&buf)
	require.NoError(t, err)
	assert.Equal(t, "test-token", msg.Token)
	assert.Empty(t, msg.Tunnels)
}

func TestClientTLSConfig(t *testing.T) {
	cfg := ClientTLSConfig()
	assert.NotNil(t, cfg)
	assert.True(t, cfg.InsecureSkipVerify)
	assert.Equal(t, []string{"tunneledge"}, cfg.NextProtos)
}

func TestEncodeDecodeHello(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EncodeHello(&buf, "tunneledge/agent"))

	// Read the first byte (MsgHello) like the gateway does.
	msgType, err := ReadMessageType(&buf)
	require.NoError(t, err)
	assert.Equal(t, MsgHello, msgType)

	hello, err := DecodeHello(&buf)
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, hello.Version)
	assert.Equal(t, "tunneledge/agent", hello.ClientVersion)
}

func TestDecodeHello_VersionMismatch(t *testing.T) {
	// Build a frame with an incompatible version.
	var buf bytes.Buffer
	require.NoError(t, EncodeHello(&buf, "tunneledge/agent"))

	// Patch the version bytes (offset 1 and 2 in the raw frame).
	data := buf.Bytes()
	data[1] = 0xFF
	data[2] = 0xFF

	patchedBuf := bytes.NewBuffer(data)
	// Skip the MsgHello byte.
	_, _ = ReadMessageType(patchedBuf)

	hello, err := DecodeHello(patchedBuf)
	require.NoError(t, err)
	// Version should be 0xFFFF, which is != ProtocolVersion.
	assert.NotEqual(t, ProtocolVersion, hello.Version)
}
