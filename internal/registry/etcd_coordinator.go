package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tunneledge/internal/domain"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Compile-time assertion.
var _ Coordinator = (*EtcdCoordinator)(nil)

const (
	etcdKeyPrefixLeases = "/tunneledge/leases/" // +tunnelID → JSON leaseMeta
	etcdKeyPrefixRelays = "/tunneledge/relays/" // +relayID  → JSON relayMeta
	etcdKeyPrefixHealth = "/tunneledge/health/" // +relayID  → JSON RelayHealth
	etcdKeyPrefix       = "/tunneledge/"
)

// leaseMeta is the JSON payload stored in etcd for a tunnel lease.
type leaseMeta struct {
	LeaseID   string    `json:"lease_id"`
	TunnelID  string    `json:"tunnel_id"`
	RelayID   string    `json:"relay_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Version   int64     `json:"version"`
	EtcdLease int64     `json:"etcd_lease,omitempty"` // etcd lease ID backing the TTL
}

// relayMeta is the JSON payload stored in etcd for relay info.
type relayMeta struct {
	RelayID       string    `json:"relay_id"`
	AdvertiseAddr string    `json:"advertise_addr"`
	State         string    `json:"state"`
	ActiveTunnels int32     `json:"active_tunnels"`
	ActiveStreams int32     `json:"active_streams"`
	LastSeen      time.Time `json:"last_seen"`
}

// EtcdCoordinator is a Coordinator backed by etcd, enabling multiple
// registry instances to share coordination state for HA deployments.
type EtcdCoordinator struct {
	client *clientv3.Client

	// etcd leases keyed by tunnel lease ID → etcd lease ID (for keepalive/revoke)
	mu          sync.Mutex
	etcdLeaseOf map[string]clientv3.LeaseID // tunnelLeaseID → etcdLeaseID

	// in-process fan-out for the etcd watch stream
	subMu      sync.RWMutex
	eventSubs  map[int64]chan domain.RegistryEvent
	healthSubs map[int64]*healthEventSub
	nextID     atomic.Int64

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewEtcdCoordinator creates an EtcdCoordinator connected to the given etcd
// endpoints and immediately starts the background watch loop.
func NewEtcdCoordinator(endpoints []string, dialTimeout time.Duration) (*EtcdCoordinator, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: dialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("etcd dial: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &EtcdCoordinator{
		client:      cli,
		etcdLeaseOf: make(map[string]clientv3.LeaseID),
		eventSubs:   make(map[int64]chan domain.RegistryEvent),
		healthSubs:  make(map[int64]*healthEventSub),
		cancel:      cancel,
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.watchLoop(ctx)
	}()

	return c, nil
}

// AcquireLease atomically acquires an exclusive tunnel lease using an etcd
// transaction. The lease key is backed by an etcd lease (TTL).
func (c *EtcdCoordinator) AcquireLease(ctx context.Context, req domain.LeaseRequest) (*domain.Lease, error) {
	if req.TunnelID == "" || req.RelayID == "" {
		return nil, status.Error(codes.InvalidArgument, "tunnel_id and relay_id are required")
	}

	ttlSec := max(int64(req.TTL.Seconds()), 1)

	// Create an etcd lease that controls TTL-based expiry.
	leaseResp, err := c.client.Grant(ctx, ttlSec)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "etcd grant: %v", err)
	}

	key := etcdKeyPrefixLeases + req.TunnelID
	leaseID := uuid.NewString()
	meta := leaseMeta{
		LeaseID:   leaseID,
		TunnelID:  req.TunnelID,
		RelayID:   req.RelayID,
		ExpiresAt: time.Now().Add(req.TTL),
		Version:   1,
		EtcdLease: int64(leaseResp.ID),
	}

	payload, err := json.Marshal(meta)
	if err != nil {
		_, _ = c.client.Revoke(ctx, leaseResp.ID)
		return nil, status.Errorf(codes.Internal, "marshal: %v", err)
	}

	// Transaction: create the key only if it does NOT exist, OR if the
	// existing owner is the same relay (re-acquisition / recovery).
	txnResp, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Version(key), "=", 0)).
		Then(clientv3.OpPut(key, string(payload), clientv3.WithLease(leaseResp.ID))).
		Else(clientv3.OpGet(key)).
		Commit()
	if err != nil {
		_, _ = c.client.Revoke(ctx, leaseResp.ID)
		return nil, status.Errorf(codes.Unavailable, "etcd txn: %v", err)
	}

	if !txnResp.Succeeded {
		// Key already exists — check if it belongs to the same relay.
		_, _ = c.client.Revoke(ctx, leaseResp.ID)
		if len(txnResp.Responses) > 0 && len(txnResp.Responses[0].GetResponseRange().Kvs) > 0 {
			var existing leaseMeta
			if err := json.Unmarshal(txnResp.Responses[0].GetResponseRange().Kvs[0].Value, &existing); err == nil {
				if existing.RelayID == req.RelayID {
					// Same relay re-acquiring — allow by doing a plain PUT.
					existing.Version++
					existing.ExpiresAt = time.Now().Add(req.TTL)
					// Create a new etcd lease for the extended TTL.
					newEtcdLease, err := c.client.Grant(ctx, ttlSec)
					if err != nil {
						return nil, status.Errorf(codes.Unavailable, "etcd grant: %v", err)
					}
					existing.EtcdLease = int64(newEtcdLease.ID)
					payload, _ = json.Marshal(existing)
					if _, err = c.client.Put(ctx, key, string(payload), clientv3.WithLease(newEtcdLease.ID)); err != nil {
						_, _ = c.client.Revoke(ctx, newEtcdLease.ID)
						return nil, status.Errorf(codes.Unavailable, "etcd put: %v", err)
					}
					result := &domain.Lease{
						LeaseID:   existing.LeaseID,
						TunnelID:  existing.TunnelID,
						RelayID:   existing.RelayID,
						ExpiresAt: existing.ExpiresAt,
						Version:   existing.Version,
					}
					c.mu.Lock()
					c.etcdLeaseOf[existing.LeaseID] = newEtcdLease.ID
					c.mu.Unlock()
					return result, nil
				}
				return nil, status.Errorf(codes.AlreadyExists, "tunnel %s already leased by relay %s", req.TunnelID, existing.RelayID)
			}
		}
		return nil, status.Errorf(codes.AlreadyExists, "tunnel %s already leased", req.TunnelID)
	}

	result := &domain.Lease{
		LeaseID:   leaseID,
		TunnelID:  req.TunnelID,
		RelayID:   req.RelayID,
		ExpiresAt: meta.ExpiresAt,
		Version:   1,
	}
	c.mu.Lock()
	c.etcdLeaseOf[leaseID] = leaseResp.ID
	c.mu.Unlock()
	return result, nil
}

// RenewLease extends the TTL of an existing tunnel lease by updating the
// underlying etcd lease via KeepAliveOnce.
func (c *EtcdCoordinator) RenewLease(ctx context.Context, leaseID string, ttl time.Duration) (*domain.Lease, error) {
	if leaseID == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id is required")
	}

	c.mu.Lock()
	etcdLID, ok := c.etcdLeaseOf[leaseID]
	c.mu.Unlock()
	if !ok {
		return nil, status.Errorf(codes.NotFound, "lease %s not found", leaseID)
	}

	if _, err := c.client.KeepAliveOnce(ctx, etcdLID); err != nil {
		return nil, status.Errorf(codes.Unavailable, "keepalive: %v", err)
	}

	// Read the current value to return up-to-date lease info.
	getResp, err := c.client.Get(ctx, etcdKeyPrefixLeases)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "etcd get: %v", err)
	}
	// Find the right key.
	for _, kv := range getResp.Kvs {
		var meta leaseMeta
		if err := json.Unmarshal(kv.Value, &meta); err == nil && meta.LeaseID == leaseID {
			meta.ExpiresAt = time.Now().Add(ttl)
			meta.Version++
			payload, _ := json.Marshal(meta)
			_, _ = c.client.Put(ctx, string(kv.Key), string(payload), clientv3.WithLease(etcdLID))
			return &domain.Lease{
				LeaseID:   meta.LeaseID,
				TunnelID:  meta.TunnelID,
				RelayID:   meta.RelayID,
				ExpiresAt: meta.ExpiresAt,
				Version:   meta.Version,
			}, nil
		}
	}

	return nil, status.Errorf(codes.NotFound, "lease %s not found after keepalive", leaseID)
}

// ReleaseLease revokes the etcd lease, which automatically deletes the key.
func (c *EtcdCoordinator) ReleaseLease(ctx context.Context, leaseID string) (*domain.Lease, error) {
	if leaseID == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id is required")
	}

	c.mu.Lock()
	etcdLID, ok := c.etcdLeaseOf[leaseID]
	if ok {
		delete(c.etcdLeaseOf, leaseID)
	}
	c.mu.Unlock()

	if !ok {
		return nil, status.Errorf(codes.NotFound, "lease %s not found", leaseID)
	}

	if _, err := c.client.Revoke(ctx, etcdLID); err != nil {
		log.Warn().Err(err).Str("lease_id", leaseID).Msg("etcd revoke failed; key will expire naturally")
	}

	return &domain.Lease{LeaseID: leaseID}, nil
}

// CurrentLease returns the active lease for tunnelID by reading etcd.
func (c *EtcdCoordinator) CurrentLease(tunnelID string) *domain.Lease {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := etcdKeyPrefixLeases + tunnelID
	resp, err := c.client.Get(ctx, key)
	if err != nil || len(resp.Kvs) == 0 {
		return nil
	}
	var meta leaseMeta
	if err := json.Unmarshal(resp.Kvs[0].Value, &meta); err != nil {
		return nil
	}
	if meta.ExpiresAt.Before(time.Now()) {
		return nil
	}
	return &domain.Lease{
		LeaseID:   meta.LeaseID,
		TunnelID:  meta.TunnelID,
		RelayID:   meta.RelayID,
		ExpiresAt: meta.ExpiresAt,
		Version:   meta.Version,
	}
}

// ReportRelayHealth stores relay health and relay info in etcd.
func (c *EtcdCoordinator) ReportRelayHealth(ctx context.Context, relayID, advertiseAddr string, health domain.RelayHealth) (*domain.RelayInfo, *domain.RelayHealth, error) {
	if relayID == "" {
		return nil, nil, status.Error(codes.InvalidArgument, "relay_id is required")
	}

	// Upsert relay info.
	rInfo := &domain.RelayInfo{
		RelayID:       relayID,
		AdvertiseAddr: advertiseAddr,
		State:         "active",
		ActiveTunnels: health.ActiveTunnels,
		ActiveStreams: health.ActiveStreams,
		LastSeen:      health.RecordedAt,
	}
	// Load existing to preserve fields not in the current report.
	existingRelay := c.getRelayInfo(ctx, relayID)
	if existingRelay != nil && advertiseAddr == "" {
		rInfo.AdvertiseAddr = existingRelay.AdvertiseAddr
	}

	rMeta := relayMeta{
		RelayID:       rInfo.RelayID,
		AdvertiseAddr: rInfo.AdvertiseAddr,
		State:         rInfo.State,
		ActiveTunnels: rInfo.ActiveTunnels,
		ActiveStreams: rInfo.ActiveStreams,
		LastSeen:      rInfo.LastSeen,
	}
	relayPayload, err := json.Marshal(rMeta)
	if err == nil {
		_, _ = c.client.Put(ctx, etcdKeyPrefixRelays+relayID, string(relayPayload))
	}

	// Store health with a short TTL.
	healthPayload, err := json.Marshal(health)
	if err == nil {
		// 3× health report interval ~= 45s default, use a fixed 60s TTL.
		etcdLease, err := c.client.Grant(ctx, 60)
		if err == nil {
			_, _ = c.client.Put(ctx, etcdKeyPrefixHealth+relayID, string(healthPayload), clientv3.WithLease(etcdLease.ID))
		} else {
			_, _ = c.client.Put(ctx, etcdKeyPrefixHealth+relayID, string(healthPayload))
		}
	}

	healthCopy := health
	c.Publish(domain.RegistryEvent{
		Type:   domain.RegistryEventRelayHealthUpdated,
		Relay:  rInfo,
		Health: &healthCopy,
	})
	c.publishHealth(relayID, &healthCopy)

	return rInfo, &healthCopy, nil
}

// GetRelayHealth reads the last reported health for relayID from etcd.
func (c *EtcdCoordinator) GetRelayHealth(relayID string) *domain.RelayHealth {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := c.client.Get(ctx, etcdKeyPrefixHealth+relayID)
	if err != nil || len(resp.Kvs) == 0 {
		return nil
	}
	var h domain.RelayHealth
	if err := json.Unmarshal(resp.Kvs[0].Value, &h); err != nil {
		return nil
	}
	return &h
}

func (c *EtcdCoordinator) getRelayInfo(ctx context.Context, relayID string) *domain.RelayInfo {
	resp, err := c.client.Get(ctx, etcdKeyPrefixRelays+relayID)
	if err != nil || len(resp.Kvs) == 0 {
		return nil
	}
	var meta relayMeta
	if err := json.Unmarshal(resp.Kvs[0].Value, &meta); err != nil {
		return nil
	}
	return &domain.RelayInfo{
		RelayID:       meta.RelayID,
		AdvertiseAddr: meta.AdvertiseAddr,
		State:         meta.State,
		ActiveTunnels: meta.ActiveTunnels,
		ActiveStreams: meta.ActiveStreams,
		LastSeen:      meta.LastSeen,
	}
}

// Publish fans out a RegistryEvent to all Subscribe consumers.
func (c *EtcdCoordinator) Publish(event domain.RegistryEvent) {
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	for _, ch := range c.eventSubs {
		select {
		case ch <- event:
		default:
		}
	}
}

// Subscribe returns a per-caller buffered channel of all events.
func (c *EtcdCoordinator) Subscribe(ctx context.Context) <-chan domain.RegistryEvent {
	ch := make(chan domain.RegistryEvent, 64)
	id := c.nextID.Add(1)
	c.subMu.Lock()
	c.eventSubs[id] = ch
	c.subMu.Unlock()

	go func() {
		<-ctx.Done()
		c.subMu.Lock()
		delete(c.eventSubs, id)
		c.subMu.Unlock()
		close(ch)
	}()

	return ch
}

// SubscribeHealth returns a per-caller channel of RelayHealth updates.
func (c *EtcdCoordinator) SubscribeHealth(ctx context.Context, relayID string) (<-chan domain.RelayHealth, *domain.RelayHealth) {
	ch := make(chan domain.RelayHealth, 32)
	id := c.nextID.Add(1)

	c.subMu.Lock()
	c.healthSubs[id] = &healthEventSub{relayID: relayID, ch: ch}
	c.subMu.Unlock()

	current := c.GetRelayHealth(relayID)

	go func() {
		<-ctx.Done()
		c.subMu.Lock()
		delete(c.healthSubs, id)
		c.subMu.Unlock()
		close(ch)
	}()

	return ch, current
}

func (c *EtcdCoordinator) publishHealth(relayID string, health *domain.RelayHealth) {
	if health == nil {
		return
	}
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	for _, sub := range c.healthSubs {
		if sub.relayID != "" && sub.relayID != relayID {
			continue
		}
		select {
		case sub.ch <- *health:
		default:
		}
	}
}

// Snapshot returns the current etcd coordination state as RegistryEvents.
func (c *EtcdCoordinator) Snapshot(tunnelID, relayID string) []domain.RegistryEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var events []domain.RegistryEvent

	// Leases.
	leaseResp, err := c.client.Get(ctx, etcdKeyPrefixLeases, clientv3.WithPrefix())
	if err == nil {
		for _, kv := range leaseResp.Kvs {
			var meta leaseMeta
			if err := json.Unmarshal(kv.Value, &meta); err != nil {
				continue
			}
			if tunnelID != "" && meta.TunnelID != tunnelID {
				continue
			}
			if relayID != "" && meta.RelayID != relayID {
				continue
			}
			if meta.ExpiresAt.Before(time.Now()) {
				continue
			}
			events = append(events, domain.RegistryEvent{
				Type: domain.RegistryEventLeaseAcquired,
				Lease: &domain.Lease{
					LeaseID:   meta.LeaseID,
					TunnelID:  meta.TunnelID,
					RelayID:   meta.RelayID,
					ExpiresAt: meta.ExpiresAt,
					Version:   meta.Version,
				},
			})
		}
	}

	// Relay info.
	relayResp, err := c.client.Get(ctx, etcdKeyPrefixRelays, clientv3.WithPrefix())
	if err == nil {
		for _, kv := range relayResp.Kvs {
			var meta relayMeta
			if err := json.Unmarshal(kv.Value, &meta); err != nil {
				continue
			}
			if relayID != "" && meta.RelayID != relayID {
				continue
			}
			events = append(events, domain.RegistryEvent{
				Type: domain.RegistryEventRelayUpserted,
				Relay: &domain.RelayInfo{
					RelayID:       meta.RelayID,
					AdvertiseAddr: meta.AdvertiseAddr,
					State:         meta.State,
					ActiveTunnels: meta.ActiveTunnels,
					ActiveStreams: meta.ActiveStreams,
					LastSeen:      meta.LastSeen,
				},
			})
		}
	}

	// Relay health.
	healthResp, err := c.client.Get(ctx, etcdKeyPrefixHealth, clientv3.WithPrefix())
	if err == nil {
		for _, kv := range healthResp.Kvs {
			var h domain.RelayHealth
			if err := json.Unmarshal(kv.Value, &h); err != nil {
				continue
			}
			rID := strings.TrimPrefix(string(kv.Key), etcdKeyPrefixHealth)
			if relayID != "" && rID != relayID {
				continue
			}
			var rInfo *domain.RelayInfo
			if ri := c.getRelayInfo(ctx, rID); ri != nil {
				rInfo = ri
			}
			events = append(events, domain.RegistryEvent{
				Type:   domain.RegistryEventRelayHealthUpdated,
				Relay:  rInfo,
				Health: &h,
			})
		}
	}

	return events
}

// ReleaseExpiredLeases is a no-op for EtcdCoordinator: etcd TTLs handle expiry.
func (c *EtcdCoordinator) ReleaseExpiredLeases() []*domain.Lease {
	return nil
}

// Close cancels the background watch and closes the etcd client.
func (c *EtcdCoordinator) Close() error {
	c.cancel()
	c.wg.Wait()
	return c.client.Close()
}

// watchLoop watches all tunneledge keys in etcd and converts events to
// RegistryEvents that are published to all subscribers.
func (c *EtcdCoordinator) watchLoop(ctx context.Context) {
	wch := c.client.Watch(ctx, etcdKeyPrefix, clientv3.WithPrefix())
	for {
		select {
		case <-ctx.Done():
			return
		case resp, ok := <-wch:
			if !ok {
				return
			}
			for _, ev := range resp.Events {
				c.handleEtcdEvent(ev)
			}
		}
	}
}

func (c *EtcdCoordinator) handleEtcdEvent(ev *clientv3.Event) {
	key := string(ev.Kv.Key)

	switch {
	case strings.HasPrefix(key, etcdKeyPrefixLeases):
		if ev.Type == clientv3.EventTypeDelete {
			tunnelID := strings.TrimPrefix(key, etcdKeyPrefixLeases)
			c.Publish(domain.RegistryEvent{
				Type:  domain.RegistryEventLeaseReleased,
				Lease: &domain.Lease{TunnelID: tunnelID},
			})
			return
		}
		var meta leaseMeta
		if err := json.Unmarshal(ev.Kv.Value, &meta); err != nil {
			return
		}
		c.mu.Lock()
		c.etcdLeaseOf[meta.LeaseID] = clientv3.LeaseID(meta.EtcdLease)
		c.mu.Unlock()
		c.Publish(domain.RegistryEvent{
			Type: domain.RegistryEventLeaseAcquired,
			Lease: &domain.Lease{
				LeaseID:   meta.LeaseID,
				TunnelID:  meta.TunnelID,
				RelayID:   meta.RelayID,
				ExpiresAt: meta.ExpiresAt,
				Version:   meta.Version,
			},
		})

	case strings.HasPrefix(key, etcdKeyPrefixRelays):
		if ev.Type == clientv3.EventTypeDelete {
			relayID := strings.TrimPrefix(key, etcdKeyPrefixRelays)
			c.Publish(domain.RegistryEvent{
				Type:  domain.RegistryEventRelayUpserted,
				Relay: &domain.RelayInfo{RelayID: relayID, State: "gone"},
			})
			return
		}
		var meta relayMeta
		if err := json.Unmarshal(ev.Kv.Value, &meta); err != nil {
			return
		}
		c.Publish(domain.RegistryEvent{
			Type: domain.RegistryEventRelayUpserted,
			Relay: &domain.RelayInfo{
				RelayID:       meta.RelayID,
				AdvertiseAddr: meta.AdvertiseAddr,
				State:         meta.State,
				ActiveTunnels: meta.ActiveTunnels,
				ActiveStreams: meta.ActiveStreams,
				LastSeen:      meta.LastSeen,
			},
		})

	case strings.HasPrefix(key, etcdKeyPrefixHealth):
		if ev.Type == clientv3.EventTypeDelete {
			return
		}
		var h domain.RelayHealth
		if err := json.Unmarshal(ev.Kv.Value, &h); err != nil {
			return
		}
		relayID := strings.TrimPrefix(key, etcdKeyPrefixHealth)
		rInfo := c.getRelayInfo(context.Background(), relayID)
		c.Publish(domain.RegistryEvent{
			Type:   domain.RegistryEventRelayHealthUpdated,
			Relay:  rInfo,
			Health: &h,
		})
		c.publishHealth(relayID, &h)
	}
}
