package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTunnelRouter_RegisterLookup(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")

	host := r.Register("t-agent-1")
	assert.Equal(t, "agent-1.tunneledge.dev", host)

	tunnelID, ok := r.Lookup("agent-1.tunneledge.dev")
	assert.True(t, ok)
	assert.Equal(t, "t-agent-1", tunnelID)
}

func TestTunnelRouter_Deregister(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")
	r.Register("t-agent-1")

	r.Deregister("t-agent-1")

	_, ok := r.Lookup("agent-1.tunneledge.dev")
	assert.False(t, ok)
}

func TestTunnelRouter_UnknownHost(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")

	_, ok := r.Lookup("unknown.tunneledge.dev")
	assert.False(t, ok)
}

func TestTunnelRouter_HasHostname(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")
	r.Register("t-agent-1")

	assert.True(t, r.HasHostname("agent-1.tunneledge.dev"))
	assert.False(t, r.HasHostname("other.tunneledge.dev"))
}

func TestTunnelRouter_HostnameForTunnel(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")

	assert.Equal(t, "agent-1.tunneledge.dev", r.HostnameForTunnel("t-agent-1"))
	assert.Equal(t, "myapp.tunneledge.dev", r.HostnameForTunnel("t-myapp"))
}

func TestTunnelRouter_List(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")
	r.Register("t-agent-1")
	r.Register("t-agent-2")

	list := r.List()
	assert.Len(t, list, 2)
	assert.Equal(t, "t-agent-1", list["agent-1.tunneledge.dev"])
	assert.Equal(t, "t-agent-2", list["agent-2.tunneledge.dev"])
}

func TestTunnelRouter_MultipleRegistersSameTunnel(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")

	r.Register("t-agent-1")
	r.Register("t-agent-1")

	_, ok := r.Lookup("agent-1.tunneledge.dev")
	assert.True(t, ok)

	list := r.List()
	assert.Len(t, list, 1)
}
