package gateway

import (
	"fmt"
	"strings"
	"sync"
)

type routeKey struct {
	TunnelID string
	Label    string
}

type TunnelRouter struct {
	mu      sync.RWMutex
	hostMap map[string]routeKey
	domain  string
}

func NewTunnelRouter(domain string) *TunnelRouter {
	return &TunnelRouter{
		hostMap: make(map[string]routeKey),
		domain:  domain,
	}
}

func (r *TunnelRouter) HostnameForTunnel(tunnelID string) string {
	return fmt.Sprintf("%s.%s", strings.TrimPrefix(tunnelID, "t-"), r.domain)
}

func (r *TunnelRouter) HostnameForLabel(tunnelID, label string) string {
	agentID := strings.TrimPrefix(tunnelID, "t-")
	return fmt.Sprintf("%s.%s.%s", label, agentID, r.domain)
}

func (r *TunnelRouter) Register(tunnelID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	hostname := r.HostnameForTunnel(tunnelID)
	r.hostMap[hostname] = routeKey{TunnelID: tunnelID, Label: "default"}
	return hostname
}

func (r *TunnelRouter) RegisterLabel(tunnelID, label string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	hostname := r.HostnameForLabel(tunnelID, label)
	r.hostMap[hostname] = routeKey{TunnelID: tunnelID, Label: label}
	return hostname
}

func (r *TunnelRouter) Deregister(tunnelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hostname := r.HostnameForTunnel(tunnelID)
	delete(r.hostMap, hostname)
}

func (r *TunnelRouter) DeregisterLabel(tunnelID, label string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hostname := r.HostnameForLabel(tunnelID, label)
	delete(r.hostMap, hostname)
}

func (r *TunnelRouter) DeregisterAll(tunnelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for host, key := range r.hostMap {
		if key.TunnelID == tunnelID {
			delete(r.hostMap, host)
		}
	}
}

func (r *TunnelRouter) Lookup(hostname string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key, ok := r.hostMap[hostname]
	if !ok {
		return "", false
	}
	return key.TunnelID, true
}

func (r *TunnelRouter) LookupWithLabel(hostname string) (tunnelID string, label string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key, ok := r.hostMap[hostname]
	if !ok {
		return "", "", false
	}
	return key.TunnelID, key.Label, true
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
		result[k] = v.TunnelID
	}
	return result
}
