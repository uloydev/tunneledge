package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewSession(t *testing.T) {
	sess := NewSession("t-1", "agent-1", "localhost:3000")
	assert.Equal(t, "t-1", sess.TunnelID)
	assert.Equal(t, "agent-1", sess.AgentID)
	assert.Equal(t, "localhost:3000", sess.LocalAddr)
	assert.False(t, sess.CreatedAt.IsZero())
	assert.False(t, sess.LastHeartbeat.IsZero())
}

func TestSession_IsExpired(t *testing.T) {
	sess := &Session{LastHeartbeat: time.Now().Add(-10 * time.Minute)}
	assert.True(t, sess.IsExpired(5*time.Minute))
	assert.False(t, sess.IsExpired(15*time.Minute))
}

func TestSession_Touch(t *testing.T) {
	sess := &Session{LastHeartbeat: time.Now().Add(-1 * time.Hour)}
	sess.Touch()
	assert.WithinDuration(t, time.Now(), sess.LastHeartbeat, 2*time.Second)
}

func TestTunnelID(t *testing.T) {
	id := NewTunnelID("agent-1")
	assert.Equal(t, TunnelID("t-agent-1"), id)
	assert.Equal(t, "t-agent-1", id.String())
	assert.Equal(t, "agent-1", id.AgentID())
}

func TestTunnel(t *testing.T) {
	routes := []TunnelRoute{
		{Label: "web", LocalAddr: "localhost:3000"},
		{Label: "api", LocalAddr: "localhost:8080"},
	}
	tunnel := NewActiveTunnel("agent-1", routes)

	assert.Equal(t, TunnelID("t-agent-1"), tunnel.ID)
	assert.Equal(t, "agent-1", tunnel.AgentID)
	assert.Len(t, tunnel.Routes, 2)

	routeMap := tunnel.RouteMap()
	assert.Equal(t, "localhost:3000", routeMap["web"])
	assert.Equal(t, "localhost:8080", routeMap["api"])
}
