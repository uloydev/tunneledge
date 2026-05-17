package gateway

import (
	"context"
	"sync"

	"tunneledge/internal/domain"

	"github.com/rs/zerolog/log"
)

// remoteRoute holds enough information to forward a public TLS connection to
// the gateway that owns a particular tunnel.
type remoteRoute struct {
	TunnelID     string
	Label        string
	OwnerRelayID string
	OwnerAddr    string // the advertise_addr of the owner gateway, e.g. "gateway-2:443"
}

// DistributedRouter maintains a watch-driven mirror of the global tunnel
// hostname → relay mapping. It is updated by registry Watch events and is
// consulted by the public connection handler when the local TunnelRouter has
// no match (the tunnel is owned by a peer gateway).
type DistributedRouter struct {
	mu     sync.RWMutex
	routes map[string]*remoteRoute // hostname → route
	relays map[string]string       // relayID → advertise addr

	// selfRelayID is used to skip routes that are owned locally.
	selfRelayID string
	baseDomain  string
	router      *TunnelRouter
}

func NewDistributedRouter(selfRelayID, baseDomain string, localRouter *TunnelRouter) *DistributedRouter {
	return &DistributedRouter{
		routes:      make(map[string]*remoteRoute),
		relays:      make(map[string]string),
		selfRelayID: selfRelayID,
		baseDomain:  baseDomain,
		router:      localRouter,
	}
}

// Lookup returns the remote route for a hostname, if it is owned by a peer
// gateway. Returns nil when the hostname is unknown or is owned locally.
func (d *DistributedRouter) Lookup(hostname string) *remoteRoute {
	d.mu.RLock()
	defer d.mu.RUnlock()
	r, ok := d.routes[hostname]
	if !ok {
		return nil
	}
	if r.OwnerRelayID == d.selfRelayID {
		return nil
	}
	return r
}

// Run consumes registry watch events and keeps the routing table current for
// as long as ctx is live. It blocks and returns when ctx is cancelled.
func (d *DistributedRouter) Run(ctx context.Context, client domain.RegistryClient) error {
	events, err := client.Watch(ctx, domain.WatchOptions{IncludeExisting: true})
	if err != nil {
		return err
	}

	log.Info().Str("relay_id", d.selfRelayID).Msg("distributed router watch started")

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			d.handleEvent(event)
		}
	}
}

func (d *DistributedRouter) handleEvent(event domain.RegistryEvent) {
	switch event.Type {
	case domain.RegistryEventRelayUpserted:
		if event.Relay != nil && event.Relay.AdvertiseAddr != "" {
			d.mu.Lock()
			d.relays[event.Relay.RelayID] = event.Relay.AdvertiseAddr
			d.mu.Unlock()
		}

	case domain.RegistryEventRelayHealthUpdated:
		if event.Relay != nil && event.Relay.AdvertiseAddr != "" {
			d.mu.Lock()
			d.relays[event.Relay.RelayID] = event.Relay.AdvertiseAddr
			d.mu.Unlock()
		}

	case domain.RegistryEventTunnelUpserted:
		if event.Tunnel == nil {
			return
		}
		ownerRelayID := event.Tunnel.OwnerRelayID
		if ownerRelayID == "" && event.Lease != nil {
			ownerRelayID = event.Lease.RelayID
		}
		if ownerRelayID == "" || ownerRelayID == d.selfRelayID {
			return
		}
		d.upsertTunnelRoutes(event.Tunnel.TunnelID, ownerRelayID)

	case domain.RegistryEventLeaseAcquired:
		if event.Lease == nil {
			return
		}
		if event.Lease.RelayID == "" || event.Lease.RelayID == d.selfRelayID {
			return
		}
		d.upsertTunnelRoutes(event.Lease.TunnelID, event.Lease.RelayID)

	case domain.RegistryEventLeaseReleased:
		if event.Lease == nil {
			return
		}
		d.removeTunnelRoutes(event.Lease.TunnelID)

	case domain.RegistryEventTunnelDeleted:
		if event.Tunnel == nil {
			return
		}
		d.removeTunnelRoutes(event.Tunnel.TunnelID)
	}
}

func (d *DistributedRouter) upsertTunnelRoutes(tunnelID, ownerRelayID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ownerAddr := d.relays[ownerRelayID]

	defaultHost := d.router.HostnameForTunnel(tunnelID)
	d.routes[defaultHost] = &remoteRoute{
		TunnelID:     tunnelID,
		Label:        "default",
		OwnerRelayID: ownerRelayID,
		OwnerAddr:    ownerAddr,
	}

	log.Debug().
		Str("tunnel_id", tunnelID).
		Str("owner_relay_id", ownerRelayID).
		Str("owner_addr", ownerAddr).
		Str("hostname", defaultHost).
		Msg("distributed route upserted")
}

func (d *DistributedRouter) removeTunnelRoutes(tunnelID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for host, route := range d.routes {
		if route.TunnelID == tunnelID {
			delete(d.routes, host)
			log.Debug().Str("tunnel_id", tunnelID).Str("hostname", host).Msg("distributed route removed")
		}
	}
}

// UpdateRelayAddr updates the advertise address for a known relay and
// refreshes all routes that reference it.
func (d *DistributedRouter) UpdateRelayAddr(relayID, addr string) {
	if relayID == "" || addr == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	d.relays[relayID] = addr
	for _, route := range d.routes {
		if route.OwnerRelayID == relayID {
			route.OwnerAddr = addr
		}
	}
}
