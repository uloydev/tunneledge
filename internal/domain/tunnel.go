package domain

import (
	"fmt"
	"time"
)

type TunnelID string

func NewTunnelID(agentID string) TunnelID {
	return TunnelID(fmt.Sprintf("t-%s", agentID))
}

func (id TunnelID) String() string {
	return string(id)
}

func (id TunnelID) AgentID() string {
	if len(id) > 2 && id[:2] == "t-" {
		return string(id[2:])
	}
	return string(id)
}

type TunnelRoute struct {
	Label      string
	LocalAddr  string
	TunnelType string // "tcp" or "udp"
}

type ActiveTunnel struct {
	ID          TunnelID
	AgentID     string
	Routes      []TunnelRoute
	PublicHosts map[string]string
	CreatedAt   time.Time
}

func NewActiveTunnel(agentID string, routes []TunnelRoute) *ActiveTunnel {
	return &ActiveTunnel{
		ID:          NewTunnelID(agentID),
		AgentID:     agentID,
		Routes:      routes,
		PublicHosts: make(map[string]string),
		CreatedAt:   time.Now(),
	}
}

func (t *ActiveTunnel) RouteMap() map[string]string {
	m := make(map[string]string, len(t.Routes))
	for _, r := range t.Routes {
		m[r.Label] = r.LocalAddr
	}
	return m
}
