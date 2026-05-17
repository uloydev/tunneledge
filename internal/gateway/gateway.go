package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"tunneledge/internal/auth"
	"tunneledge/internal/domain"
	"tunneledge/internal/stream"
	"tunneledge/internal/transport"
	"tunneledge/pkg/metrics"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

type activeTunnel struct {
	tunnel   *domain.ActiveTunnel
	conn     *quic.Conn
	routeMap map[string]string
}

type Gateway struct {
	mu                sync.RWMutex
	tunnels           map[string]*activeTunnel
	router            *TunnelRouter
	streamManager     *stream.Manager
	authenticator     auth.Authenticator
	registryClient    domain.RegistryClient
	metrics           *metrics.Metrics
	quicListenAddr    string
	publicListenAddr  string
	baseDomain        string
	tlsCertFile       string
	tlsKeyFile        string
	maxStreams        int64
	shutdownTimeout   time.Duration
	streamIdleTimeout time.Duration
}

type Options struct {
	QUICListenAddr    string
	PublicListenAddr  string
	BaseDomain        string
	TLSCertFile       string
	TLSKeyFile        string
	MaxStreams        int64
	ShutdownTimeout   time.Duration
	StreamIdleTimeout time.Duration
	Authenticator     auth.Authenticator
	RegistryClient    domain.RegistryClient
	Metrics           *metrics.Metrics
}

func NewGateway(opts Options) (*Gateway, error) {
	if opts.Authenticator == nil {
		return nil, fmt.Errorf("authenticator is required")
	}
	if opts.RegistryClient == nil {
		return nil, fmt.Errorf("registry client is required")
	}

	return &Gateway{
		tunnels:           make(map[string]*activeTunnel),
		router:            NewTunnelRouter(opts.BaseDomain),
		streamManager:     stream.NewManager(),
		authenticator:     opts.Authenticator,
		registryClient:    opts.RegistryClient,
		metrics:           opts.Metrics,
		quicListenAddr:    opts.QUICListenAddr,
		publicListenAddr:  opts.PublicListenAddr,
		baseDomain:        opts.BaseDomain,
		tlsCertFile:       opts.TLSCertFile,
		tlsKeyFile:        opts.TLSKeyFile,
		maxStreams:        opts.MaxStreams,
		shutdownTimeout:   gatewayShutdownTimeout(opts.ShutdownTimeout),
		streamIdleTimeout: streamIdleTimeout(opts.StreamIdleTimeout),
	}, nil
}

func gatewayShutdownTimeout(v time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return 15 * time.Second
}

// streamIdleTimeout returns v when positive, otherwise the safe default of 30s.
func streamIdleTimeout(v time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return 30 * time.Second
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

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(runCtx)
	group.Go(func() error {
		return g.acceptAgents(groupCtx, quicListener)
	})
	group.Go(func() error {
		return g.acceptPublicConnections(groupCtx, publicLn)
	})

	<-ctx.Done()
	cancel()
	_ = quicListener.Close()
	_ = publicLn.Close()
	if err := group.Wait(); err != nil {
		return err
	}

	log.Info().Msg("gateway shutting down — draining connections")

	// Drain: wait for active streams to finish within ShutdownTimeout
	drainCtx, drainCancel := context.WithTimeout(context.Background(), g.shutdownTimeout)
	defer drainCancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-drainCtx.Done():
			log.Info().Msg("drain timeout reached, force closing")
			goto forceClose
		case <-ticker.C:
			if g.streamManager.Count() == 0 {
				log.Info().Msg("all streams drained")
				goto forceClose
			}
			log.Debug().Int("active_streams", g.streamManager.Count()).Msg("waiting for streams to drain")
		}
	}

forceClose:
	g.closeAll()

	return nil
}


func (g *Gateway) acceptAgents(ctx context.Context, listener *quic.Listener) error {
	workers, workerCtx := errgroup.WithContext(ctx)

	for {
		select {
		case <-ctx.Done():
			return workers.Wait()
		default:
		}

		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return workers.Wait()
			}
			return fmt.Errorf("failed to accept QUIC connection: %w", err)
		}

		agentConn := conn
		workers.Go(func() error {
			g.handleAgentConnection(workerCtx, agentConn)
			return nil
		})
	}
}

func (g *Gateway) acceptPublicConnections(ctx context.Context, ln net.Listener) error {
	workers, workerCtx := errgroup.WithContext(ctx)

	for {
		select {
		case <-ctx.Done():
			return workers.Wait()
		default:
		}

		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return workers.Wait()
			}
			return fmt.Errorf("failed to accept public connection: %w", err)
		}

		publicConn := conn
		workers.Go(func() error {
			g.handlePublicConnection(workerCtx, publicConn)
			return nil
		})
	}
}

func (g *Gateway) addTunnel(tunnel *domain.ActiveTunnel, conn *quic.Conn) {
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
		log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("failed to deregister from registry")
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

// publicPort extracts the port number from a listen address such as ":443"
// or "0.0.0.0:443" for use in building public URLs.
// If the address has no port or cannot be parsed, "443" is returned as a safe
// default (the standard HTTPS/QUIC port).
func publicPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "443"
	}
	return port
}
