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
	tunnel    *domain.ActiveTunnel
	conn      *quic.Conn
	routeMap  map[string]string
	leaseID   string
	suspended bool // true during session resume grace window
}

type Gateway struct {
	mu               sync.RWMutex
	tunnels          map[string]*activeTunnel
	router           *TunnelRouter
	distRouter       *DistributedRouter
	streamManager    *stream.Manager
	authenticator    auth.Authenticator
	registryClient   domain.RegistryClient
	metrics          *metrics.Metrics
	relayID          string
	advertiseAddr    string
	quicListenAddr   string
	publicListenAddr string
	baseDomain       string
	tlsCertFile      string
	tlsKeyFile       string
	// mTLS for agent QUIC connections
	mtlsEnabled          bool
	clientCAFile         string
	leaseTTL             time.Duration
	healthReportInterval time.Duration
	maxStreams           int64
	maxTunnelsPerAgent   int
	maxStreamsPerTunnel  int64
	authRateLimiter      *auth.IPRateLimiter
	shutdownTimeout      time.Duration
	streamIdleTimeout    time.Duration
	tunnelACLs           domain.TunnelACLRepository
	// Phase 4: region, session resume, UDP.
	region               string
	sessionResumeEnabled bool
	sessionResumeTTL     time.Duration
	sessionRepo          domain.SessionRepository
	udpListenAddr        string
	// resume timer map — protected by resumeMu, independent of g.mu.
	resumeMu            sync.Mutex
	pendingResumeTimers map[string]*time.Timer
}

type Options struct {
	QUICListenAddr       string
	PublicListenAddr     string
	BaseDomain           string
	TLSCertFile          string
	TLSKeyFile           string
	MaxStreams           int64
	ShutdownTimeout      time.Duration
	StreamIdleTimeout    time.Duration
	Authenticator        auth.Authenticator
	RegistryClient       domain.RegistryClient
	Metrics              *metrics.Metrics
	RelayID              string
	AdvertiseAddr        string
	LeaseTTL             time.Duration
	HealthReportInterval time.Duration
	// Phase 3: mTLS
	MTLSEnabled  bool
	ClientCAFile string
	// Phase 3: abuse prevention
	AuthRateLimitRPM    int
	MaxTunnelsPerAgent  int
	MaxStreamsPerTunnel int64
	// Phase 3: tunnel ACL enforcement
	TunnelACLs domain.TunnelACLRepository
	// Phase 4: region, session resume, UDP.
	Region               string
	SessionResumeEnabled bool
	SessionResumeTTL     time.Duration
	SessionRepo          domain.SessionRepository
	UDPListenAddr        string
}

func NewGateway(opts Options) (*Gateway, error) {
	if opts.Authenticator == nil {
		return nil, fmt.Errorf("authenticator is required")
	}
	if opts.RegistryClient == nil {
		return nil, fmt.Errorf("registry client is required")
	}

	locRouter := NewTunnelRouter(opts.BaseDomain)

	var authRL *auth.IPRateLimiter
	if opts.AuthRateLimitRPM > 0 {
		authRL = auth.NewIPRateLimiter(opts.AuthRateLimitRPM, opts.AuthRateLimitRPM)
	}

	return &Gateway{
		tunnels:              make(map[string]*activeTunnel),
		router:               locRouter,
		distRouter:           NewDistributedRouter(opts.RelayID, opts.BaseDomain, locRouter),
		streamManager:        stream.NewManager(),
		authenticator:        opts.Authenticator,
		registryClient:       opts.RegistryClient,
		metrics:              opts.Metrics,
		relayID:              opts.RelayID,
		advertiseAddr:        opts.AdvertiseAddr,
		quicListenAddr:       opts.QUICListenAddr,
		publicListenAddr:     opts.PublicListenAddr,
		baseDomain:           opts.BaseDomain,
		tlsCertFile:          opts.TLSCertFile,
		tlsKeyFile:           opts.TLSKeyFile,
		mtlsEnabled:          opts.MTLSEnabled,
		clientCAFile:         opts.ClientCAFile,
		leaseTTL:             gatewayLeaseTTL(opts.LeaseTTL),
		healthReportInterval: gatewayHealthReportInterval(opts.HealthReportInterval),
		maxStreams:           opts.MaxStreams,
		maxTunnelsPerAgent:   opts.MaxTunnelsPerAgent,
		maxStreamsPerTunnel:  opts.MaxStreamsPerTunnel,
		authRateLimiter:      authRL,
		shutdownTimeout:      gatewayShutdownTimeout(opts.ShutdownTimeout),
		streamIdleTimeout:    streamIdleTimeout(opts.StreamIdleTimeout),
		tunnelACLs:           opts.TunnelACLs,
		region:               opts.Region,
		sessionResumeEnabled: opts.SessionResumeEnabled,
		sessionResumeTTL:     sessionResumeTTL(opts.SessionResumeTTL),
		sessionRepo:          opts.SessionRepo,
		udpListenAddr:        opts.UDPListenAddr,
		pendingResumeTimers:  make(map[string]*time.Timer),
	}, nil
}

func gatewayLeaseTTL(v time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return 45 * time.Second
}

func gatewayHealthReportInterval(v time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return 15 * time.Second
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

// sessionResumeTTL returns v when positive, otherwise the safe default of 30s.
func sessionResumeTTL(v time.Duration) time.Duration {
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
	group.Go(func() error {
		return g.reportRelayHealthLoop(groupCtx)
	})
	group.Go(func() error {
		return g.distRouter.Run(groupCtx, g.registryClient)
	})

	// Phase 4D: optional UDP listener.
	if g.udpListenAddr != "" {
		udpAddr, udpAddrErr := net.ResolveUDPAddr("udp", g.udpListenAddr)
		if udpAddrErr != nil {
			return fmt.Errorf("invalid udp_listen_addr %q: %w", g.udpListenAddr, udpAddrErr)
		}
		udpConn, udpErr := net.ListenUDP("udp", udpAddr)
		if udpErr != nil {
			return fmt.Errorf("failed to listen UDP on %s: %w", g.udpListenAddr, udpErr)
		}
		defer udpConn.Close()
		log.Info().Str("udp_addr", g.udpListenAddr).Msg("UDP listener started")
		group.Go(func() error {
			g.udpDatagramListener(groupCtx, udpConn)
			return nil
		})
	}

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

func (g *Gateway) addTunnel(tunnel *domain.ActiveTunnel, conn *quic.Conn, leaseID string) {
	g.mu.Lock()
	routeMap := tunnel.RouteMap()
	g.tunnels[tunnel.ID.String()] = &activeTunnel{
		tunnel:   tunnel,
		conn:     conn,
		routeMap: routeMap,
		leaseID:  leaseID,
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

	if at.leaseID != "" {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := g.registryClient.ReleaseLease(releaseCtx, at.leaseID); err != nil {
			log.Warn().Err(err).Str("tunnel_id", tunnelID).Str("lease_id", at.leaseID).Msg("failed to release tunnel lease")
		}
		cancel()
	}

	if err := g.registryClient.DeregisterTunnel(tunnelID); err != nil {
		log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("failed to deregister from registry")
	}
}

// suspendTunnel closes the current QUIC connection and streams for tunnelID
// but keeps the router registration and tunnel entry alive for the session
// resume grace window. A timer fires after sessionResumeTTL to fully clean up
// if the agent does not reconnect.
//
// Only called when sessionResumeEnabled && sessionRepo != nil.
func (g *Gateway) suspendTunnel(tunnelID, reason string) {
	g.mu.Lock()
	at, ok := g.tunnels[tunnelID]
	if !ok {
		g.mu.Unlock()
		return
	}

	// Generate and store the resume token.
	token, err := generateResumeToken()
	if err != nil {
		g.mu.Unlock()
		// Fall back to full removal on crypto error.
		log.Error().Err(err).Str("tunnel_id", tunnelID).Msg("failed to generate resume token; doing full removal")
		g.removeTunnel(tunnelID, reason)
		return
	}

	// Mark as suspended (nil out conn so public handler won't try to use it).
	at.conn.CloseWithError(0, reason)
	at.conn = nil
	at.suspended = true
	g.mu.Unlock()

	// Close existing streams — they cannot survive a QUIC reconnect.
	g.streamManager.CloseByTunnel(tunnelID)

	deadline := time.Now().Add(g.sessionResumeTTL)
	storeCtx, storeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if storeErr := g.sessionRepo.SetResumable(storeCtx, tunnelID, token, deadline); storeErr != nil {
		log.Warn().Err(storeErr).Str("tunnel_id", tunnelID).Msg("failed to store resume token; doing full removal")
		storeCancel()
		g.removeTunnel(tunnelID, reason)
		return
	}
	storeCancel()

	if g.metrics != nil {
		// Don't count as destroyed — tunnel is suspended, not gone.
	}

	log.Info().
		Str("tunnel_id", tunnelID).
		Str("reason", reason).
		Dur("resume_ttl", g.sessionResumeTTL).
		Msg("tunnel suspended; entering resume window")

	// Start the finalization timer.
	t := time.AfterFunc(g.sessionResumeTTL, func() {
		g.finalizeTunnelRemoval(tunnelID, "resume_timeout")
	})
	g.resumeMu.Lock()
	g.pendingResumeTimers[tunnelID] = t
	g.resumeMu.Unlock()
}

// finalizeTunnelRemoval performs a full cleanup of a suspended or active tunnel.
// It is idempotent — safe to call even if the tunnel has already been removed.
func (g *Gateway) finalizeTunnelRemoval(tunnelID, reason string) {
	// Cancel any pending timer.
	g.resumeMu.Lock()
	if t, ok := g.pendingResumeTimers[tunnelID]; ok {
		t.Stop()
		delete(g.pendingResumeTimers, tunnelID)
	}
	g.resumeMu.Unlock()

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

	log.Info().Str("tunnel_id", tunnelID).Str("reason", reason).Msg("tunnel finalized")

	if at.leaseID != "" {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := g.registryClient.ReleaseLease(releaseCtx, at.leaseID); err != nil {
			log.Warn().Err(err).Str("tunnel_id", tunnelID).Str("lease_id", at.leaseID).Msg("failed to release lease")
		}
		cancel()
	}

	if err := g.registryClient.DeregisterTunnel(tunnelID); err != nil {
		log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("failed to deregister from registry")
	}
}

func (g *Gateway) acquireTunnelLease(ctx context.Context, tunnelID string) (*domain.Lease, error) {
	return g.registryClient.AcquireLease(ctx, domain.LeaseRequest{
		TunnelID: tunnelID,
		RelayID:  g.relayID,
		TTL:      g.leaseTTL,
	})
}

func (g *Gateway) reportRelayHealthLoop(ctx context.Context) error {
	report := func() {
		health := domain.RelayHealth{
			AdvertiseAddr: g.advertiseAddr,
			ActiveTunnels: int32(g.ActiveTunnelCount()),
			ActiveStreams: int32(g.ActiveStreamCount()),
			RecordedAt:    time.Now(),
			Region:        g.region,
		}
		if err := g.registryClient.ReportRelayHealth(ctx, g.relayID, health); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Str("relay_id", g.relayID).Msg("failed to report relay health")
		}
		if g.advertiseAddr != "" {
			g.distRouter.UpdateRelayAddr(g.relayID, g.advertiseAddr)
		}
	}

	report()
	ticker := time.NewTicker(g.healthReportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			report()
		}
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
	if g.mtlsEnabled && g.tlsCertFile != "" && g.tlsKeyFile != "" && g.clientCAFile != "" {
		return transport.LoadMTLSServerConfig(g.tlsCertFile, g.tlsKeyFile, g.clientCAFile)
	}
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
