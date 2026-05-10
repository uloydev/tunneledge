package gateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"tunneledge/internal/auth"
	"tunneledge/internal/domain"
	"tunneledge/internal/stream"
	"tunneledge/internal/transport"
	"tunneledge/pkg/metrics"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
)

type activeTunnel struct {
	tunnel   *domain.Tunnel
	conn     *quic.Conn
	routeMap map[string]string
}

type Gateway struct {
	mu               sync.RWMutex
	tunnels          map[string]*activeTunnel
	router           *TunnelRouter
	streamManager    *stream.Manager
	authenticator    auth.Authenticator
	registryClient   domain.RegistryClient
	metrics          *metrics.Metrics
	quicListenAddr   string
	publicListenAddr string
	baseDomain       string
	tlsCertFile      string
	tlsKeyFile       string
	maxStreams       int64
}

type Options struct {
	QUICListenAddr   string
	PublicListenAddr string
	BaseDomain       string
	TLSCertFile      string
	TLSKeyFile       string
	MaxStreams       int64
	Authenticator    auth.Authenticator
	RegistryClient   domain.RegistryClient
	Metrics          *metrics.Metrics
}

func NewGateway(opts Options) (*Gateway, error) {
	if opts.Authenticator == nil {
		return nil, fmt.Errorf("authenticator is required")
	}
	if opts.RegistryClient == nil {
		return nil, fmt.Errorf("registry client is required")
	}

	return &Gateway{
		tunnels:          make(map[string]*activeTunnel),
		router:           NewTunnelRouter(opts.BaseDomain),
		streamManager:    stream.NewManager(),
		authenticator:    opts.Authenticator,
		registryClient:   opts.RegistryClient,
		metrics:          opts.Metrics,
		quicListenAddr:   opts.QUICListenAddr,
		publicListenAddr: opts.PublicListenAddr,
		baseDomain:       opts.BaseDomain,
		tlsCertFile:      opts.TLSCertFile,
		tlsKeyFile:       opts.TLSKeyFile,
		maxStreams:       opts.MaxStreams,
	}, nil
}

func (g *Gateway) Run(ctx context.Context) error {
	quicTLS, err := g.buildQUICTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to get QUIC TLS config: %w", err)
	}

	quicListener, err := transport.QUICListen(g.quicListenAddr, quicTLS)
	if err != nil {
		return fmt.Errorf("failed to listen QUIC: %w", err)
	}
	defer quicListener.Close()

	publicTLS, err := g.buildPublicTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to get public TLS config: %w", err)
	}

	publicLn, err := tls.Listen("tcp", g.publicListenAddr, publicTLS)
	if err != nil {
		return fmt.Errorf("failed to listen public TLS on %s: %w", g.publicListenAddr, err)
	}
	defer publicLn.Close()

	log.Info().
		Str("quic_addr", g.quicListenAddr).
		Str("public_addr", g.publicListenAddr).
		Str("base_domain", g.baseDomain).
		Msg("gateway listeners started")

	go g.acceptAgents(ctx, quicListener)
	go g.acceptPublicConnections(ctx, publicLn)

	<-ctx.Done()
	log.Info().Msg("gateway shutting down")
	g.closeAll()

	return nil
}

func (g *Gateway) acceptAgents(ctx context.Context, listener *quic.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error().Err(err).Msg("failed to accept QUIC connection")
			continue
		}

		go g.handleAgentConnection(ctx, conn)
	}
}

func (g *Gateway) acceptPublicConnections(ctx context.Context, ln net.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error().Err(err).Msg("failed to accept public connection")
			continue
		}

		go g.handlePublicConnection(ctx, conn)
	}
}

func (g *Gateway) addTunnel(tunnel *domain.Tunnel, conn *quic.Conn) {
	g.mu.Lock()
	routeMap := tunnel.RouteMap()
	g.tunnels[tunnel.ID.String()] = &activeTunnel{
		tunnel:   tunnel,
		conn:     conn,
		routeMap: routeMap,
	}
	g.mu.Unlock()
}

func (g *Gateway) getTunnel(tunnelID string) (*activeTunnel, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	at, ok := g.tunnels[tunnelID]
	return at, ok
}

func (g *Gateway) removeTunnel(tunnelID, reason string) {
	g.mu.Lock()
	at, ok := g.tunnels[tunnelID]
	if !ok {
		g.mu.Unlock()
		return
	}
	delete(g.tunnels, tunnelID)
	g.mu.Unlock()

	g.router.DeregisterAll(tunnelID)
	g.streamManager.CloseByTunnel(tunnelID)
	if at.conn != nil {
		at.conn.CloseWithError(0, reason)
	}

	if g.metrics != nil {
		g.metrics.ActiveTunnels.Dec()
		g.metrics.TunnelDestroyed.WithLabelValues(reason).Inc()
	}

	log.Info().Str("tunnel_id", tunnelID).Str("reason", reason).Msg("tunnel removed")

	if err := g.registryClient.DeregisterTunnel(tunnelID); err != nil {
		log.Debug().Err(err).Str("tunnel_id", tunnelID).Msg("failed to deregister from registry")
	}
}

func (g *Gateway) closeAll() {
	g.mu.RLock()
	ids := make([]string, 0, len(g.tunnels))
	for id := range g.tunnels {
		ids = append(ids, id)
	}
	g.mu.RUnlock()

	for _, id := range ids {
		g.removeTunnel(id, "shutdown")
	}

	g.streamManager.CloseAll()
}

func (g *Gateway) buildQUICTLSConfig() (*tls.Config, error) {
	if g.tlsCertFile != "" && g.tlsKeyFile != "" {
		return transport.LoadQUICTLSConfig(g.tlsCertFile, g.tlsKeyFile)
	}
	return transport.GenerateSelfSignedQUICTLSConfig()
}

func (g *Gateway) buildPublicTLSConfig() (*tls.Config, error) {
	if g.tlsCertFile != "" && g.tlsKeyFile != "" {
		return transport.LoadPublicTLSConfig(g.tlsCertFile, g.tlsKeyFile)
	}
	return transport.GenerateWildcardSelfSignedTLSConfig(g.baseDomain)
}

func (g *Gateway) ActiveTunnelCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.tunnels)
}

func (g *Gateway) ActiveStreamCount() int {
	return g.streamManager.Count()
}

func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "localhost"
	}
	return host
}
