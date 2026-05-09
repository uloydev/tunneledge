package session

import (
	"sync"
	"testing"
	"time"

	"tunneledge/pkg/errs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_RegisterGet(t *testing.T) {
	store := NewMemoryStore()

	sess := &Session{
		TunnelID: "t-1",
		AgentID:  "agent-1",
	}

	err := store.Register(sess)
	require.NoError(t, err)

	got, err := store.Get("t-1")
	require.NoError(t, err)
	assert.Equal(t, "t-1", got.TunnelID)
	assert.Equal(t, "agent-1", got.AgentID)
	assert.False(t, got.CreatedAt.IsZero())
}

func TestMemoryStore_RegisterDuplicate(t *testing.T) {
	store := NewMemoryStore()

	sess := &Session{TunnelID: "t-1"}
	require.NoError(t, store.Register(sess))

	err := store.Register(&Session{TunnelID: "t-1"})
	assert.Error(t, err)
	assert.Equal(t, errs.CodeAlreadyExists, errs.GetCode(err))
}

func TestMemoryStore_Deregister(t *testing.T) {
	store := NewMemoryStore()
	require.NoError(t, store.Register(&Session{TunnelID: "t-1"}))

	require.NoError(t, store.Deregister("t-1"))

	_, err := store.Get("t-1")
	assert.Error(t, err)
	assert.Equal(t, errs.CodeNotFound, errs.GetCode(err))
}

func TestMemoryStore_List(t *testing.T) {
	store := NewMemoryStore()
	require.NoError(t, store.Register(&Session{TunnelID: "t-1"}))
	require.NoError(t, store.Register(&Session{TunnelID: "t-2"}))

	list := store.List()
	assert.Len(t, list, 2)
}

func TestMemoryStore_Heartbeat(t *testing.T) {
	store := NewMemoryStore()
	require.NoError(t, store.Register(&Session{TunnelID: "t-1"}))

	sess, _ := store.Get("t-1")
	sess.LastHeartbeat = time.Now().Add(-1 * time.Hour)

	require.NoError(t, store.Heartbeat("t-1"))

	after, _ := store.Get("t-1")
	assert.WithinDuration(t, time.Now(), after.LastHeartbeat, 2*time.Second)
}

func TestMemoryStore_CleanupExpired(t *testing.T) {
	store := NewMemoryStore()
	require.NoError(t, store.Register(&Session{TunnelID: "t-1"}))

	sess, _ := store.Get("t-1")
	sess.LastHeartbeat = time.Now().Add(-10 * time.Minute)

	expired := store.CleanupExpired(5 * time.Minute)
	assert.Equal(t, 1, expired)
	assert.Empty(t, store.List())
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = store.Register(&Session{TunnelID: string(rune(i))})
		}(i)
	}

	wg.Wait()
	assert.Len(t, store.List(), 100)
}
