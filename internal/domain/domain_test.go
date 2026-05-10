package domain

import (
	"sync"
	"testing"
	"time"

	"tunneledge/pkg/errs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemorySessionRepository_RegisterGet(t *testing.T) {
	repo := NewMemorySessionRepository()

	sess := &Session{
		TunnelID: "t-1",
		AgentID:  "agent-1",
	}

	err := repo.Register(sess)
	require.NoError(t, err)

	got, err := repo.Get("t-1")
	require.NoError(t, err)
	assert.Equal(t, "t-1", got.TunnelID)
	assert.Equal(t, "agent-1", got.AgentID)
	assert.False(t, got.CreatedAt.IsZero())
}

func TestMemorySessionRepository_RegisterDuplicate(t *testing.T) {
	repo := NewMemorySessionRepository()

	sess := &Session{TunnelID: "t-1"}
	require.NoError(t, repo.Register(sess))

	err := repo.Register(&Session{TunnelID: "t-1"})
	assert.Error(t, err)
	assert.Equal(t, errs.CodeAlreadyExists, errs.GetCode(err))
}

func TestMemorySessionRepository_Deregister(t *testing.T) {
	repo := NewMemorySessionRepository()
	require.NoError(t, repo.Register(&Session{TunnelID: "t-1"}))

	require.NoError(t, repo.Deregister("t-1"))

	_, err := repo.Get("t-1")
	assert.Error(t, err)
	assert.Equal(t, errs.CodeNotFound, errs.GetCode(err))
}

func TestMemorySessionRepository_List(t *testing.T) {
	repo := NewMemorySessionRepository()
	require.NoError(t, repo.Register(&Session{TunnelID: "t-1"}))
	require.NoError(t, repo.Register(&Session{TunnelID: "t-2"}))

	list := repo.List()
	assert.Len(t, list, 2)
}

func TestMemorySessionRepository_Heartbeat(t *testing.T) {
	repo := NewMemorySessionRepository()
	require.NoError(t, repo.Register(&Session{TunnelID: "t-1"}))

	sess, _ := repo.Get("t-1")
	sess.LastHeartbeat = time.Now().Add(-1 * time.Hour)

	require.NoError(t, repo.Heartbeat("t-1"))

	after, _ := repo.Get("t-1")
	assert.WithinDuration(t, time.Now(), after.LastHeartbeat, 2*time.Second)
}

func TestMemorySessionRepository_CleanupExpired(t *testing.T) {
	repo := NewMemorySessionRepository()
	require.NoError(t, repo.Register(&Session{TunnelID: "t-1"}))

	sess, _ := repo.Get("t-1")
	sess.LastHeartbeat = time.Now().Add(-10 * time.Minute)

	expired := repo.CleanupExpired(5 * time.Minute)
	assert.Equal(t, 1, expired)
	assert.Empty(t, repo.List())
}

func TestMemorySessionRepository_ConcurrentAccess(t *testing.T) {
	repo := NewMemorySessionRepository()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = repo.Register(&Session{TunnelID: string(rune(i))})
		}(i)
	}

	wg.Wait()
	assert.Len(t, repo.List(), 100)
}

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
	tunnel := NewTunnel("agent-1", routes)

	assert.Equal(t, TunnelID("t-agent-1"), tunnel.ID)
	assert.Equal(t, "agent-1", tunnel.AgentID)
	assert.Len(t, tunnel.Routes, 2)

	routeMap := tunnel.RouteMap()
	assert.Equal(t, "localhost:3000", routeMap["web"])
	assert.Equal(t, "localhost:8080", routeMap["api"])
}
