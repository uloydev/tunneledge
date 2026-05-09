package gateway

import (
	"fmt"
	"strings"
	"sync"
)

type TunnelRouter struct {
	mu      sync.RWMutex
	hostMap map[string]string
	domain  string
}

func NewTunnelRouter(domain string) *TunnelRouter {
	return &TunnelRouter{
		hostMap: make(map[string]string),
		domain:  domain,
	}
}

func (r *TunnelRouter) HostnameForTunnel(tunnelID string) string {
	return fmt.Sprintf("%s.%s", strings.TrimPrefix(tunnelID, "t-"), r.domain)
}

func (r *TunnelRouter) Register(tunnelID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	hostname := r.HostnameForTunnel(tunnelID)
	r.hostMap[hostname] = tunnelID
	return hostname
}

func (r *TunnelRouter) Deregister(tunnelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hostname := r.HostnameForTunnel(tunnelID)
	delete(r.hostMap, hostname)
}

func (r *TunnelRouter) Lookup(hostname string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tunnelID, ok := r.hostMap[hostname]
	return tunnelID, ok
}

func (r *TunnelRouter) HasHostname(hostname string) bool {
	_, ok := r.Lookup(hostname)
	return ok
}

func (r *TunnelRouter) TunnelIDForHost(hostname string) (string, bool) {
	return r.Lookup(hostname)
}

func (r *TunnelRouter) List() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string, len(r.hostMap))
	for k, v := range r.hostMap {
		result[k] = v
	}
	return result
}
