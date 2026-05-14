package memstore

import (
	"context"
	"sync"
	"testing"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/pkg/errs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemorySessionRepository_RegisterGet(t *testing.T) {
	ctx := context.Background()
	repo := NewMemorySessionRepository()

	sess := &domain.Session{
		TunnelID: "t-1",
		AgentID:  "agent-1",
	}

	err := repo.Register(ctx, sess)
	require.NoError(t, err)

	got, err := repo.Get(ctx, "t-1")
	require.NoError(t, err)
	assert.Equal(t, "t-1", got.TunnelID)
	assert.Equal(t, "agent-1", got.AgentID)
	assert.False(t, got.CreatedAt.IsZero())
}

func TestMemorySessionRepository_RegisterDuplicate(t *testing.T) {
	ctx := context.Background()
	repo := NewMemorySessionRepository()

	sess := &domain.Session{TunnelID: "t-1"}
	require.NoError(t, repo.Register(ctx, sess))

	err := repo.Register(ctx, &domain.Session{TunnelID: "t-1"})
	assert.Error(t, err)
	assert.Equal(t, errs.CodeAlreadyExists, errs.GetCode(err))
}

func TestMemorySessionRepository_Deregister(t *testing.T) {
	ctx := context.Background()
	repo := NewMemorySessionRepository()
	require.NoError(t, repo.Register(ctx, &domain.Session{TunnelID: "t-1"}))

	require.NoError(t, repo.Deregister(ctx, "t-1"))

	_, err := repo.Get(ctx, "t-1")
	assert.Error(t, err)
	assert.Equal(t, errs.CodeNotFound, errs.GetCode(err))
}

func TestMemorySessionRepository_List(t *testing.T) {
	ctx := context.Background()
	repo := NewMemorySessionRepository()
	require.NoError(t, repo.Register(ctx, &domain.Session{TunnelID: "t-1"}))
	require.NoError(t, repo.Register(ctx, &domain.Session{TunnelID: "t-2"}))

	list, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestMemorySessionRepository_Heartbeat(t *testing.T) {
	ctx := context.Background()
	repo := NewMemorySessionRepository()
	require.NoError(t, repo.Register(ctx, &domain.Session{TunnelID: "t-1"}))

	sess, _ := repo.Get(ctx, "t-1")
	sess.LastHeartbeat = time.Now().Add(-1 * time.Hour)

	require.NoError(t, repo.Heartbeat(ctx, "t-1"))

	after, _ := repo.Get(ctx, "t-1")
	assert.WithinDuration(t, time.Now(), after.LastHeartbeat, 2*time.Second)
}

func TestMemorySessionRepository_CleanupExpired(t *testing.T) {
	ctx := context.Background()
	repo := NewMemorySessionRepository()
	require.NoError(t, repo.Register(ctx, &domain.Session{TunnelID: "t-1"}))

	sess, _ := repo.Get(ctx, "t-1")
	sess.LastHeartbeat = time.Now().Add(-10 * time.Minute)

	expired, err := repo.CleanupExpired(ctx, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, expired)
	assert.Empty(t, mustList(t, repo, ctx))
}

func TestMemorySessionRepository_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	repo := NewMemorySessionRepository()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = repo.Register(ctx, &domain.Session{TunnelID: string(rune(i))})
		}(i)
	}

	wg.Wait()
	list, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 100)
}

func mustList(t *testing.T, repo *MemorySessionRepository, ctx context.Context) []*domain.Session {
	t.Helper()
	list, err := repo.List(ctx)
	require.NoError(t, err)
	return list
}
