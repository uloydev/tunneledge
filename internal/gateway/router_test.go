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

func TestTunnelRouter_RegisterLabel(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")

	host := r.RegisterLabel("t-agent-1", "web")
	assert.Equal(t, "web-agent-1.tunneledge.dev", host)

	host2 := r.RegisterLabel("t-agent-1", "api")
	assert.Equal(t, "api-agent-1.tunneledge.dev", host2)

	tunnelID, label, ok := r.LookupWithLabel("web-agent-1.tunneledge.dev")
	assert.True(t, ok)
	assert.Equal(t, "t-agent-1", tunnelID)
	assert.Equal(t, "web", label)

	tunnelID2, label2, ok2 := r.LookupWithLabel("api-agent-1.tunneledge.dev")
	assert.True(t, ok2)
	assert.Equal(t, "t-agent-1", tunnelID2)
	assert.Equal(t, "api", label2)
}

func TestTunnelRouter_DeregisterLabel(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")
	r.RegisterLabel("t-agent-1", "web")
	r.RegisterLabel("t-agent-1", "api")

	r.DeregisterLabel("t-agent-1", "web")

	_, _, ok := r.LookupWithLabel("web-agent-1.tunneledge.dev")
	assert.False(t, ok)

	_, _, ok2 := r.LookupWithLabel("api-agent-1.tunneledge.dev")
	assert.True(t, ok2)
}

func TestTunnelRouter_DeregisterAll(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")
	r.RegisterLabel("t-agent-1", "web")
	r.RegisterLabel("t-agent-1", "api")
	r.Register("t-agent-2")

	r.DeregisterAll("t-agent-1")

	_, _, ok1 := r.LookupWithLabel("web-agent-1.tunneledge.dev")
	assert.False(t, ok1)

	_, _, ok2 := r.LookupWithLabel("api-agent-1.tunneledge.dev")
	assert.False(t, ok2)

	_, ok3 := r.Lookup("agent-2.tunneledge.dev")
	assert.True(t, ok3)
}

func TestTunnelRouter_LookupWithLabel_Unknown(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")

	_, _, ok := r.LookupWithLabel("unknown.tunneledge.dev")
	assert.False(t, ok)
}

func TestTunnelRouter_HostnameForLabel(t *testing.T) {
	r := NewTunnelRouter("tunneledge.dev")

	assert.Equal(t, "web-agent-1.tunneledge.dev", r.HostnameForLabel("t-agent-1", "web"))
	assert.Equal(t, "api-myapp.tunneledge.dev", r.HostnameForLabel("t-myapp", "api"))
}
