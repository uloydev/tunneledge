package domain

import (
	"context"
	"time"
)

type RegistryEventType string

const (
	RegistryEventUnknown            RegistryEventType = "unknown"
	RegistryEventTunnelUpserted     RegistryEventType = "tunnel_upserted"
	RegistryEventTunnelDeleted      RegistryEventType = "tunnel_deleted"
	RegistryEventLeaseAcquired      RegistryEventType = "lease_acquired"
	RegistryEventLeaseReleased      RegistryEventType = "lease_released"
	RegistryEventRelayUpserted      RegistryEventType = "relay_upserted"
	RegistryEventRelayHealthUpdated RegistryEventType = "relay_health_updated"
)

type WatchOptions struct {
	TunnelID        string
	RelayID         string
	IncludeExisting bool
}

type RelayInfo struct {
	RelayID       string
	AdvertiseAddr string
	State         string
	ActiveTunnels int32
	ActiveStreams int32
	LastSeen      time.Time
	Region        string // optional region tag (e.g. "us-east-1")
}

type Lease struct {
	LeaseID   string
	TunnelID  string
	RelayID   string
	ExpiresAt time.Time
	Version   int64
}

type LeaseRequest struct {
	TunnelID string
	RelayID  string
	TTL      time.Duration
}

type RelayHealth struct {
	AdvertiseAddr          string
	RTTMillis              int64
	HeartbeatLatencyMillis int64
	ActiveTunnels          int32
	ActiveStreams          int32
	BytesPerSecond         int64
	PacketLossPct          float64
	CPUUtilizationPct      float64
	MemoryUtilizationPct   float64
	RecordedAt             time.Time
	Region                 string // matches RelayInfo.Region
}

type RegistryEvent struct {
	Type   RegistryEventType
	Tunnel *Session
	Lease  *Lease
	Relay  *RelayInfo
	Health *RelayHealth
}

type RegistryClient interface {
	RegisterTunnel(tunnelID, agentID, publicAddr, localAddr string) error
	DeregisterTunnel(tunnelID string) error
	Heartbeat(tunnelID string) (bool, error)
	Watch(ctx context.Context, opts WatchOptions) (<-chan RegistryEvent, error)
	AcquireLease(ctx context.Context, req LeaseRequest) (*Lease, error)
	RenewLease(ctx context.Context, leaseID string, ttl time.Duration) (*Lease, error)
	ReleaseLease(ctx context.Context, leaseID string) error
	ReportRelayHealth(ctx context.Context, relayID string, health RelayHealth) error
	SubscribeRelayHealth(ctx context.Context, relayID string, includeCurrent bool) (<-chan RelayHealth, error)
}
