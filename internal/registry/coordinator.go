package registry

import (
	"context"
	"time"

	"tunneledge/internal/domain"
)

// Coordinator abstracts the distributed coordination state used by the
// registry: tunnel leases, relay info, relay health, and event fanout.
//
// Two implementations ship:
//   - MemCoordinator  — single-process in-memory state (dev / single-node)
//   - EtcdCoordinator — etcd-backed for multi-instance HA deployments
type Coordinator interface {
	// AcquireLease atomically acquires an exclusive tunnel lease.
	// Returns codes.AlreadyExists if the tunnel is already leased by a
	// different relay.
	AcquireLease(ctx context.Context, req domain.LeaseRequest) (*domain.Lease, error)

	// RenewLease extends the TTL of an existing lease.
	RenewLease(ctx context.Context, leaseID string, ttl time.Duration) (*domain.Lease, error)

	// ReleaseLease releases a lease and returns a copy for event emission.
	ReleaseLease(ctx context.Context, leaseID string) (*domain.Lease, error)

	// CurrentLease returns the active (non-expired) lease for tunnelID, or nil.
	CurrentLease(tunnelID string) *domain.Lease

	// ReportRelayHealth stores relay health and updates relay info.
	// Returns the updated RelayInfo and health snapshot.
	ReportRelayHealth(ctx context.Context, relayID, advertiseAddr string, health domain.RelayHealth) (*domain.RelayInfo, *domain.RelayHealth, error)

	// GetRelayHealth returns the last known health for relayID, or nil.
	GetRelayHealth(relayID string) *domain.RelayHealth

	// Publish fans out a RegistryEvent to all active Subscribe consumers.
	// Used by the Server to broadcast tunnel register/deregister events.
	Publish(event domain.RegistryEvent)

	// Subscribe returns a per-caller channel of all RegistryEvents.
	// The channel is closed when ctx is cancelled.
	// Slow consumers receive dropped events (non-blocking send).
	Subscribe(ctx context.Context) <-chan domain.RegistryEvent

	// SubscribeHealth returns a channel of health updates for relayID
	// (empty string = all relays). Also returns the current health
	// snapshot for the relay if available.
	SubscribeHealth(ctx context.Context, relayID string) (<-chan domain.RelayHealth, *domain.RelayHealth)

	// Snapshot returns the current coordination state as a list of events.
	// Used to fulfil Watch requests with IncludeExisting=true.
	// tunnelID and relayID act as optional filters (empty = all).
	Snapshot(tunnelID, relayID string) []domain.RegistryEvent

	// ReleaseExpiredLeases removes leases past their expiry.
	// Returns all released leases so the caller can emit watch events.
	ReleaseExpiredLeases() []*domain.Lease

	// Close releases coordinator resources.
	Close() error
}
