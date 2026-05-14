package stream

import (
	"fmt"
	"io"
	"sync"
	"time"

	"tunneledge/pkg/errs"

	"github.com/google/uuid"
)

type State int

const (
	StateOpen State = iota
	StateClosed
)

func (s State) String() string {
	switch s {
	case StateOpen:
		return "OPEN"
	case StateClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

type Stream struct {
	ID        string
	TunnelID  string
	CreatedAt time.Time

	mu     sync.Mutex
	state  State
	stream io.ReadWriteCloser
}

// State returns the current state of the stream, safe for concurrent access.
func (s *Stream) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Stream) Read(p []byte) (int, error) {
	return s.stream.Read(p)
}

func (s *Stream) Write(p []byte) (int, error) {
	return s.stream.Write(p)
}

func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == StateClosed {
		return nil
	}
	s.state = StateClosed
	return s.stream.Close()
}

type Manager struct {
	mu      sync.RWMutex
	streams map[string]*Stream
}

func NewManager() *Manager {
	return &Manager{
		streams: make(map[string]*Stream),
	}
}

func (m *Manager) Open(tunnelID string, raw io.ReadWriteCloser) *Stream {
	s := &Stream{
		ID:        generateStreamID(),
		TunnelID:  tunnelID,
		CreatedAt: time.Now(),
		state:     StateOpen,
		stream:    raw,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.streams[s.ID] = s
	return s
}

func (m *Manager) Get(id string) (*Stream, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.streams[id]
	if !ok {
		return nil, errs.New(errs.CodeNotFound, fmt.Sprintf("stream %s not found", id))
	}
	return s, nil
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.streams[id]
	if !ok {
		return errs.New(errs.CodeNotFound, fmt.Sprintf("stream %s not found", id))
	}

	if err := s.Close(); err != nil {
		return fmt.Errorf("failed to close stream %s: %w", id, err)
	}

	delete(m.streams, id)
	return nil
}

func (m *Manager) List() []*Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Stream, 0, len(m.streams))
	for _, s := range m.streams {
		result = append(result, s)
	}
	return result
}

func (m *Manager) ListByTunnel(tunnelID string) []*Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Stream
	for _, s := range m.streams {
		if s.TunnelID == tunnelID {
			result = append(result, s)
		}
	}
	return result
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, s := range m.streams {
		_ = s.Close()
		delete(m.streams, id)
	}
}

func (m *Manager) CloseByTunnel(tunnelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, s := range m.streams {
		if s.TunnelID == tunnelID {
			_ = s.Close()
			delete(m.streams, id)
		}
	}
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.streams)
}

func (m *Manager) CountByTunnel(tunnelID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, s := range m.streams {
		if s.TunnelID == tunnelID {
			count++
		}
	}
	return count
}

func generateStreamID() string {
	return uuid.New().String()
}
