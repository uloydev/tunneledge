package tui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"tunneledge/internal/agent"
	"tunneledge/pkg/config"

	"github.com/rs/zerolog/log"
)

type AgentStatus string

const (
	StatusDisconnected AgentStatus = "disconnected"
	StatusConnecting   AgentStatus = "connecting"
	StatusConnected    AgentStatus = "connected"
	StatusReconnecting AgentStatus = "reconnecting"
)

type StatusUpdate struct {
	Status       AgentStatus
	TunnelID     string
	PublicURLs   map[string]string
	ActiveStreams int
	Sent         int64
	Received     int64
	Uptime       time.Duration
	Error        string
}

type TunnelEvent struct {
	Label    string
	LocalAddr string
	PublicURL string
	Active   bool
}

type TunnelStatus struct {
	Label       string
	LocalAddr   string
	PublicURL   string
	Active      bool
	StreamCount int
	BytesSent   int64
	BytesRecv   int64
}

type AgentBridge struct {
	agent *agent.Agent
	cfg   *config.Config

	mu              sync.RWMutex
	status          AgentStatus
	tunnelID        string
	publicURLs      map[string]string
	activeStreams   atomic.Int64
	totalSent       atomic.Int64
	totalReceived   atomic.Int64
	connectedAt     time.Time
	tunnelStatuses  map[string]*TunnelStatus

	statusCh chan StatusUpdate
	logCh    chan LogEntry
	tunnelCh chan TunnelEvent

	logWriter *LogWriter
	startTime time.Time

	cancel context.CancelFunc
}

func NewAgentBridge(cfg *config.Config) *AgentBridge {
	b := &AgentBridge{
		cfg:             cfg,
		status:          StatusDisconnected,
		publicURLs:      make(map[string]string),
		tunnelStatuses:  make(map[string]*TunnelStatus),
		statusCh:        make(chan StatusUpdate, 64),
		logCh:           make(chan LogEntry, 256),
		tunnelCh:        make(chan TunnelEvent, 64),
		startTime:       time.Now(),
	}

	b.logWriter = NewLogWriter(b.logCh)

	for _, t := range cfg.Agent.Tunnels {
		b.tunnelStatuses[t.Label] = &TunnelStatus{
			Label:     t.Label,
			LocalAddr: t.LocalAddr,
			Active:    false,
		}
	}

	return b
}

func (b *AgentBridge) LogWriter() *LogWriter {
	return b.logWriter
}

func (b *AgentBridge) StatusCh() <-chan StatusUpdate {
	return b.statusCh
}

func (b *AgentBridge) LogCh() <-chan LogEntry {
	return b.logCh
}

func (b *AgentBridge) TunnelCh() <-chan TunnelEvent {
	return b.tunnelCh
}

func (b *AgentBridge) GetStatus() StatusUpdate {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var uptime time.Duration
	if b.status == StatusConnected && !b.connectedAt.IsZero() {
		uptime = time.Since(b.connectedAt)
	}

	urls := make(map[string]string, len(b.publicURLs))
	for k, v := range b.publicURLs {
		urls[k] = v
	}

	return StatusUpdate{
		Status:       b.status,
		TunnelID:     b.tunnelID,
		PublicURLs:   urls,
		ActiveStreams: int(b.activeStreams.Load()),
		Sent:         b.totalSent.Load(),
		Received:     b.totalReceived.Load(),
		Uptime:       uptime,
	}
}

func (b *AgentBridge) GetTunnelStatuses() []TunnelStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()

	statuses := make([]TunnelStatus, 0, len(b.tunnelStatuses))
	for _, ts := range b.tunnelStatuses {
		statuses = append(statuses, *ts)
	}
	return statuses
}

func (b *AgentBridge) GetConfig() *config.Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cfg
}

func (b *AgentBridge) UpdateToken(token string) {
	b.mu.Lock()
	b.cfg.Agent.Token = token
	b.mu.Unlock()
}

func (b *AgentBridge) UpdateConfig(cfg *config.Config) {
	b.mu.Lock()
	b.cfg = cfg
	b.mu.Unlock()
}

func (b *AgentBridge) AddTunnel(label, localAddr string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.tunnelStatuses[label]; exists {
		return fmt.Errorf("tunnel %q already exists", label)
	}

	b.cfg.Agent.Tunnels = append(b.cfg.Agent.Tunnels, config.TunnelConfig{
		Label:     label,
		LocalAddr: localAddr,
	})
	b.tunnelStatuses[label] = &TunnelStatus{
		Label:     label,
		LocalAddr: localAddr,
		Active:    false,
	}

	b.tunnelCh <- TunnelEvent{Label: label, LocalAddr: localAddr}
	return nil
}

func (b *AgentBridge) RemoveTunnel(label string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.tunnelStatuses[label]; !exists {
		return fmt.Errorf("tunnel %q not found", label)
	}

	tunnels := make([]config.TunnelConfig, 0, len(b.cfg.Agent.Tunnels))
	for _, t := range b.cfg.Agent.Tunnels {
		if t.Label != label {
			tunnels = append(tunnels, t)
		}
	}
	b.cfg.Agent.Tunnels = tunnels
	delete(b.tunnelStatuses, label)

	return nil
}

func (b *AgentBridge) Start(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)

	b.mu.Lock()
	b.status = StatusConnecting
	b.mu.Unlock()

	b.statusCh <- StatusUpdate{Status: StatusConnecting}

	tunnels := b.resolveTunnels()

	a := agent.NewAgent(agent.Options{
		GatewayAddr:       b.cfg.Agent.GatewayAddr,
		Token:             b.cfg.Agent.Token,
		Tunnels:           tunnels,
		ReconnectDelay:    b.cfg.Agent.ReconnectDelay,
		MaxReconnect:      b.cfg.Agent.MaxReconnect,
		HeartbeatInterval: b.cfg.Agent.HeartbeatInterval,
	})

	b.agent = a

	go func() {
		b.mu.Lock()
		b.status = StatusConnected
		b.connectedAt = time.Now()
		b.mu.Unlock()

		urls := a.PublicURLs()
		b.mu.Lock()
		b.publicURLs = urls
		for label, url := range urls {
			if ts, ok := b.tunnelStatuses[label]; ok {
				ts.PublicURL = url
				ts.Active = true
			}
		}
		b.mu.Unlock()

		b.statusCh <- StatusUpdate{
			Status:     StatusConnected,
			PublicURLs: urls,
		}

		for label, url := range urls {
			b.tunnelCh <- TunnelEvent{
				Label:     label,
				PublicURL: url,
				Active:    true,
			}
		}

		if err := a.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("agent stopped with error")

			b.mu.Lock()
			b.status = StatusReconnecting
			b.mu.Unlock()

			b.statusCh <- StatusUpdate{
				Status: StatusReconnecting,
				Error:  err.Error(),
			}
		}

		b.mu.Lock()
		b.status = StatusDisconnected
		b.mu.Unlock()

		b.statusCh <- StatusUpdate{Status: StatusDisconnected}
	}()
}

func (b *AgentBridge) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.logWriter.Close()
}

func (b *AgentBridge) resolveTunnels() []config.TunnelConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.cfg.Agent.Tunnels) > 0 {
		return b.cfg.Agent.Tunnels
	}
	if b.cfg.Agent.LocalAddr != "" {
		return []config.TunnelConfig{{Label: "default", LocalAddr: b.cfg.Agent.LocalAddr}}
	}
	return nil
}
