package stream

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockReadWriteCloser struct {
	readErr  error
	writeErr error
	closed   bool
}

func (m *mockReadWriteCloser) Read(p []byte) (int, error)  { return 0, m.readErr }
func (m *mockReadWriteCloser) Write(p []byte) (int, error) { return len(p), m.writeErr }
func (m *mockReadWriteCloser) Close() error                { m.closed = true; return nil }

func TestManager_OpenClose(t *testing.T) {
	mgr := NewManager()

	s := mgr.Open("t-1", &mockReadWriteCloser{})
	assert.NotEmpty(t, s.ID)
	assert.Equal(t, "t-1", s.TunnelID)
	assert.Equal(t, StateOpen, s.State())
	assert.Equal(t, 1, mgr.Count())

	got, err := mgr.Get(s.ID)
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)

	require.NoError(t, mgr.Close(s.ID))
	assert.Equal(t, 0, mgr.Count())
}

func TestManager_CloseByTunnel(t *testing.T) {
	mgr := NewManager()
	s1 := mgr.Open("t-1", &mockReadWriteCloser{})
	s2 := mgr.Open("t-1", &mockReadWriteCloser{})
	s3 := mgr.Open("t-2", &mockReadWriteCloser{})

	assert.Equal(t, 3, mgr.Count())

	mgr.CloseByTunnel("t-1")
	assert.Equal(t, 1, mgr.Count())

	got, err := mgr.Get(s3.ID)
	require.NoError(t, err)
	assert.Equal(t, "t-2", got.TunnelID)

	_, err = mgr.Get(s1.ID)
	assert.Error(t, err)
	_, err = mgr.Get(s2.ID)
	assert.Error(t, err)
}

func TestManager_ListByTunnel(t *testing.T) {
	mgr := NewManager()
	mgr.Open("t-1", &mockReadWriteCloser{})
	mgr.Open("t-1", &mockReadWriteCloser{})
	mgr.Open("t-2", &mockReadWriteCloser{})

	list := mgr.ListByTunnel("t-1")
	assert.Len(t, list, 2)
}

func TestManager_CloseAll(t *testing.T) {
	mgr := NewManager()
	mgr.Open("t-1", &mockReadWriteCloser{})
	mgr.Open("t-2", &mockReadWriteCloser{})

	mgr.CloseAll()
	assert.Equal(t, 0, mgr.Count())
}

func TestManager_ConcurrentOpenClose(t *testing.T) {
	mgr := NewManager()
	var wg sync.WaitGroup

	for range 100 {
		wg.Go(func() {
			s := mgr.Open("t-1", &mockReadWriteCloser{})
			_ = mgr.Close(s.ID)
		})
	}

	wg.Wait()
	assert.Equal(t, 0, mgr.Count())
}
