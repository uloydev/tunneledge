package registry

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"tunneledge/internal/domain"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Compile-time assertion that MemCoordinator satisfies the Coordinator interface.
var _ Coordinator = (*MemCoordinator)(nil)

type healthEventSub struct {
	relayID string
	ch      chan domain.RelayHealth
}

// MemCoordinator is a single-process, in-memory Coordinator. It is the default
// backend for development and single-node production deployments.
type MemCoordinator struct {
	mu            sync.RWMutex
	leasesByID    map[string]*domain.Lease
	leaseByTunnel map[string]string
	relays        map[string]*domain.RelayInfo
	relayHealth   map[string]*domain.RelayHealth

	subMu      sync.RWMutex
	eventSubs  map[int64]chan domain.RegistryEvent
	healthSubs map[int64]*healthEventSub
	nextID     atomic.Int64
}

func NewMemCoordinator() *MemCoordinator {
	return &MemCoordinator{
		leasesByID:    make(map[string]*domain.Lease),
		leaseByTunnel: make(map[string]string),
		relays:        make(map[string]*domain.RelayInfo),
		relayHealth:   make(map[string]*domain.RelayHealth),
		eventSubs:     make(map[int64]chan domain.RegistryEvent),
		healthSubs:    make(map[int64]*healthEventSub),
	}
}

func (m *MemCoordinator) AcquireLease(ctx context.Context, req domain.LeaseRequest) (*domain.Lease, error) {
	if req.TunnelID == "" || req.RelayID == "" {
		return nil, status.Error(codes.InvalidArgument, "tunnel_id and relay_id are required")
	}
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.currentLeaseLocked(req.TunnelID, now)
	if existing != nil && existing.RelayID != req.RelayID {
		return nil, status.Errorf(codes.AlreadyExists, "tunnel %s already leased by relay %s", req.TunnelID, existing.RelayID)
	}

	lease := existing
	if lease == nil {
		lease = &domain.Lease{
			LeaseID:  uuid.NewString(),
			TunnelID: req.TunnelID,
			RelayID:  req.RelayID,
			Version:  1,
		}
	} else {
		lease.RelayID = req.RelayID
		lease.Version++
	}
	lease.ExpiresAt = now.Add(req.TTL)
	m.leasesByID[lease.LeaseID] = cloneLease(lease)
	m.leaseByTunnel[lease.TunnelID] = lease.LeaseID

	if relay, ok := m.relays[req.RelayID]; ok {
		relay.LastSeen = now
	}

	result := cloneLease(lease)
	go m.Publish(domain.RegistryEvent{Type: domain.RegistryEventLeaseAcquired, Lease: result})
	return result, nil
}

func (m *MemCoordinator) RenewLease(ctx context.Context, leaseID string, ttl time.Duration) (*domain.Lease, error) {
	if leaseID == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id is required")
	}
	now := time.Now()

	m.mu.Lock()
	lease, ok := m.leasesByID[leaseID]
	if !ok || lease.ExpiresAt.Before(now) {
		m.mu.Unlock()
		return nil, status.Errorf(codes.NotFound, "lease %s not found", leaseID)
	}
	lease.ExpiresAt = now.Add(ttl)
	lease.Version++
	result := cloneLease(lease)
	m.mu.Unlock()

	go m.Publish(domain.RegistryEvent{Type: domain.RegistryEventLeaseAcquired, Lease: result})
	return result, nil
}

func (m *MemCoordinator) ReleaseLease(ctx context.Context, leaseID string) (*domain.Lease, error) {
	if leaseID == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id is required")
	}

	m.mu.Lock()
	lease, ok := m.leasesByID[leaseID]
	if !ok {
		m.mu.Unlock()
		return nil, status.Errorf(codes.NotFound, "lease %s not found", leaseID)
	}
	result := cloneLease(lease)
	delete(m.leasesByID, leaseID)
	if m.leaseByTunnel[lease.TunnelID] == leaseID {
		delete(m.leaseByTunnel, lease.TunnelID)
	}
	m.mu.Unlock()

	go m.Publish(domain.RegistryEvent{Type: domain.RegistryEventLeaseReleased, Lease: result})
	return result, nil
}

func (m *MemCoordinator) CurrentLease(tunnelID string) *domain.Lease {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneLease(m.currentLeaseLocked(tunnelID, time.Now()))
}

func (m *MemCoordinator) currentLeaseLocked(tunnelID string, now time.Time) *domain.Lease {
	leaseID, ok := m.leaseByTunnel[tunnelID]
	if !ok {
		return nil
	}
	lease, ok := m.leasesByID[leaseID]
	if !ok {
		delete(m.leaseByTunnel, tunnelID)
		return nil
	}
	if lease.ExpiresAt.Before(now) {
		delete(m.leasesByID, leaseID)
		delete(m.leaseByTunnel, tunnelID)
		return nil
	}
	return lease
}

func (m *MemCoordinator) ReportRelayHealth(ctx context.Context, relayID, advertiseAddr string, health domain.RelayHealth) (*domain.RelayInfo, *domain.RelayHealth, error) {
	if relayID == "" {
		return nil, nil, status.Error(codes.InvalidArgument, "relay_id is required")
	}

	m.mu.Lock()
	relay := m.ensureRelayLocked(relayID)
	relay.ActiveTunnels = health.ActiveTunnels
	relay.ActiveStreams = health.ActiveStreams
	relay.LastSeen = health.RecordedAt
	if advertiseAddr != "" {
		relay.AdvertiseAddr = advertiseAddr
	}
	m.relayHealth[relayID] = &health
	relayCopy := cloneRelayInfo(relay)
	healthCopy := cloneRelayHealth(&health)
	m.mu.Unlock()

	m.Publish(domain.RegistryEvent{
		Type:   domain.RegistryEventRelayHealthUpdated,
		Relay:  relayCopy,
		Health: healthCopy,
	})
	m.publishHealth(relayID, healthCopy)

	return relayCopy, healthCopy, nil
}

func (m *MemCoordinator) GetRelayHealth(relayID string) *domain.RelayHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneRelayHealth(m.relayHealth[relayID])
}

func (m *MemCoordinator) ensureRelayLocked(relayID string) *domain.RelayInfo {
	if relay, ok := m.relays[relayID]; ok {
		return relay
	}
	relay := &domain.RelayInfo{RelayID: relayID, State: "active", LastSeen: time.Now()}
	m.relays[relayID] = relay
	return relay
}

// Publish fans out a RegistryEvent to all Subscribe consumers. Non-blocking.
func (m *MemCoordinator) Publish(event domain.RegistryEvent) {
	m.subMu.RLock()
	defer m.subMu.RUnlock()
	for _, ch := range m.eventSubs {
		select {
		case ch <- event:
		default:
		}
	}
}

// Subscribe returns a per-caller buffered channel of all events.
func (m *MemCoordinator) Subscribe(ctx context.Context) <-chan domain.RegistryEvent {
	ch := make(chan domain.RegistryEvent, 64)
	id := m.nextID.Add(1)
	m.subMu.Lock()
	m.eventSubs[id] = ch
	m.subMu.Unlock()

	go func() {
		<-ctx.Done()
		m.subMu.Lock()
		delete(m.eventSubs, id)
		m.subMu.Unlock()
		close(ch)
	}()

	return ch
}

// SubscribeHealth returns a per-caller channel of RelayHealth updates.
func (m *MemCoordinator) SubscribeHealth(ctx context.Context, relayID string) (<-chan domain.RelayHealth, *domain.RelayHealth) {
	ch := make(chan domain.RelayHealth, 32)
	id := m.nextID.Add(1)

	m.subMu.Lock()
	m.healthSubs[id] = &healthEventSub{relayID: relayID, ch: ch}
	m.subMu.Unlock()

	m.mu.RLock()
	current := cloneRelayHealth(m.relayHealth[relayID])
	m.mu.RUnlock()

	go func() {
		<-ctx.Done()
		m.subMu.Lock()
		delete(m.healthSubs, id)
		m.subMu.Unlock()
		close(ch)
	}()

	return ch, current
}

func (m *MemCoordinator) publishHealth(relayID string, health *domain.RelayHealth) {
	if health == nil {
		return
	}
	m.subMu.RLock()
	defer m.subMu.RUnlock()
	for _, sub := range m.healthSubs {
		if sub.relayID != "" && sub.relayID != relayID {
			continue
		}
		select {
		case sub.ch <- *health:
		default:
		}
	}
}

// Snapshot returns coordination state events for the given filters.
func (m *MemCoordinator) Snapshot(tunnelID, relayID string) []domain.RegistryEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var events []domain.RegistryEvent
	now := time.Now()

	for _, lease := range m.leasesByID {
		if lease.ExpiresAt.Before(now) {
			continue
		}
		if tunnelID != "" && lease.TunnelID != tunnelID {
			continue
		}
		if relayID != "" && lease.RelayID != relayID {
			continue
		}
		events = append(events, domain.RegistryEvent{
			Type:  domain.RegistryEventLeaseAcquired,
			Lease: cloneLease(lease),
		})
	}

	for _, relay := range m.relays {
		if relayID != "" && relay.RelayID != relayID {
			continue
		}
		events = append(events, domain.RegistryEvent{
			Type:  domain.RegistryEventRelayUpserted,
			Relay: cloneRelayInfo(relay),
		})
	}

	for rID, health := range m.relayHealth {
		if relayID != "" && rID != relayID {
			continue
		}
		var relayInfo *domain.RelayInfo
		if r, ok := m.relays[rID]; ok {
			relayInfo = cloneRelayInfo(r)
		}
		events = append(events, domain.RegistryEvent{
			Type:   domain.RegistryEventRelayHealthUpdated,
			Relay:  relayInfo,
			Health: cloneRelayHealth(health),
		})
	}

	return events
}

// ReleaseExpiredLeases removes expired leases and returns them.
func (m *MemCoordinator) ReleaseExpiredLeases() []*domain.Lease {
	now := time.Now()
	var expired []*domain.Lease

	m.mu.Lock()
	for leaseID, lease := range m.leasesByID {
		if lease.ExpiresAt.Before(now) {
			expired = append(expired, cloneLease(lease))
			delete(m.leasesByID, leaseID)
			if m.leaseByTunnel[lease.TunnelID] == leaseID {
				delete(m.leaseByTunnel, lease.TunnelID)
			}
		}
	}
	m.mu.Unlock()

	for _, lease := range expired {
		m.Publish(domain.RegistryEvent{Type: domain.RegistryEventLeaseReleased, Lease: lease})
	}

	return expired
}

func (m *MemCoordinator) Close() error {
	return nil
}
