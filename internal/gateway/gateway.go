package gateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"tunneledge/internal/auth"
	"tunneledge/internal/relay"
	"tunneledge/internal/stream"
	"tunneledge/internal/transport"
	"tunneledge/pkg/metrics"
	pb "tunneledge/proto/registry/v1"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type activeTunnel struct {
	tunnelID   string
	agentID    string
	localAddr  string
	conn       *quic.Conn
	publicHost string
	cancelFunc context.CancelFunc
}

type Gateway struct {
	mu               sync.RWMutex
	tunnels          map[string]*activeTunnel
	router           *TunnelRouter
	streamManager    *stream.Manager
	authenticator    auth.Authenticator
	registryClient   pb.RegistryServiceClient
	registryConn     *grpc.ClientConn
	metrics          *metrics.Metrics
	tlsCertFile      string
	tlsKeyFile       string
	quicListenAddr   string
	publicListenAddr string
	baseDomain       string
	maxStreams       int64
}

type Options struct {
	QUICListenAddr   string
	PublicListenAddr string
	BaseDomain       string
	RegistryAddr     string
	TLSCertFile      string
	TLSKeyFile       string
	MaxStreams       int64
	Authenticator    auth.Authenticator
	Metrics          *metrics.Metrics
}

func NewGateway(opts Options) (*Gateway, error) {
	conn, err := grpc.NewClient(opts.RegistryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to registry: %w", err)
	}

	return &Gateway{
		tunnels:          make(map[string]*activeTunnel),
		router:           NewTunnelRouter(opts.BaseDomain),
		streamManager:    stream.NewManager(),
		authenticator:    opts.Authenticator,
		registryClient:   pb.NewRegistryServiceClient(conn),
		registryConn:     conn,
		metrics:          opts.Metrics,
		tlsCertFile:      opts.TLSCertFile,
		tlsKeyFile:       opts.TLSKeyFile,
		quicListenAddr:   opts.QUICListenAddr,
		publicListenAddr: opts.PublicListenAddr,
		baseDomain:       opts.BaseDomain,
		maxStreams:       opts.MaxStreams,
	}, nil
}

func (g *Gateway) Run(ctx context.Context) error {
	defer g.registryConn.Close()

	quicTLS, err := g.getQUICTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to get QUIC TLS config: %w", err)
	}

	quicListener, err := transport.QUICListen(g.quicListenAddr, quicTLS)
	if err != nil {
		return fmt.Errorf("failed to listen QUIC: %w", err)
	}
	defer quicListener.Close()

	publicTLS, err := g.getPublicTLSConfig()
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

func (g *Gateway) handleAgentConnection(ctx context.Context, conn *quic.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	logger := log.With().Str("remote_addr", remoteAddr).Logger()

	acceptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	qstream, err := conn.AcceptStream(acceptCtx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to accept initial stream")
		conn.CloseWithError(1, "failed to accept auth stream")
		return
	}

	authMsg, err := transport.DecodeAuth(qstream)
	if err != nil {
		logger.Error().Err(err).Msg("failed to decode auth message")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		conn.CloseWithError(1, "auth failed")
		return
	}

	agentID, err := g.authenticator.Authenticate(authMsg.Token)
	if err != nil {
		logger.Warn().Err(err).Msg("authentication failed")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		conn.CloseWithError(1, "auth failed")
		return
	}

	tunnelID := fmt.Sprintf("t-%s", agentID)

	_, err = g.registryClient.RegisterTunnel(ctx, &pb.RegisterTunnelRequest{
		TunnelId: tunnelID,
		AgentId:  agentID,
		Token:    authMsg.Token,
	})
	if err != nil {
		logger.Error().Err(err).Msg("failed to register tunnel with registry")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		conn.CloseWithError(1, "registry error")
		return
	}

	publicHost := g.router.Register(tunnelID)
	publicURL := fmt.Sprintf("%s:%s", publicHost, stripPort(g.publicListenAddr))

	if err := transport.EncodeAuthResponse(qstream, transport.AuthStatusOK, tunnelID, publicURL); err != nil {
		logger.Error().Err(err).Msg("failed to send auth response")
		g.router.Deregister(tunnelID)
		conn.CloseWithError(1, "auth response failed")
		return
	}

	tunnelCtx, tunnelCancel := context.WithCancel(ctx)

	at := &activeTunnel{
		tunnelID:   tunnelID,
		agentID:    agentID,
		conn:       conn,
		publicHost: publicHost,
		cancelFunc: tunnelCancel,
	}

	g.mu.Lock()
	g.tunnels[tunnelID] = at
	g.mu.Unlock()

	if g.metrics != nil {
		g.metrics.ActiveTunnels.Inc()
		g.metrics.TunnelCreated.WithLabelValues("success").Inc()
	}

	logger = logger.With().
		Str("tunnel_id", tunnelID).
		Str("public_host", publicHost).
		Logger()
	logger.Info().Msg("tunnel established")

	go g.heartbeatLoop(tunnelCtx, tunnelID, conn)

	go func() {
		<-tunnelCtx.Done()
		g.removeTunnel(tunnelID, "connection_lost")
	}()

	<-tunnelCtx.Done()
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

func (g *Gateway) handlePublicConnection(ctx context.Context, conn net.Conn) {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		log.Error().Msg("expected TLS connection")
		conn.Close()
		return
	}

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		log.Debug().Err(err).Str("client_addr", conn.RemoteAddr().String()).Msg("TLS handshake failed")
		conn.Close()
		return
	}

	state := tlsConn.ConnectionState()
	hostname := state.ServerName
	if hostname == "" {
		log.Warn().Str("client_addr", conn.RemoteAddr().String()).Msg("no SNI hostname provided")
		conn.Close()
		return
	}

	tunnelID, ok := g.router.Lookup(hostname)
	if !ok {
		log.Warn().Str("hostname", hostname).Msg("unknown tunnel hostname")
		conn.Close()
		return
	}

	g.mu.RLock()
	at, ok := g.tunnels[tunnelID]
	g.mu.RUnlock()
	if !ok {
		log.Warn().Str("tunnel_id", tunnelID).Msg("tunnel not found for hostname")
		conn.Close()
		return
	}

	logger := log.With().
		Str("tunnel_id", tunnelID).
		Str("hostname", hostname).
		Str("client_addr", conn.RemoteAddr().String()).
		Logger()

	qstream, err := at.conn.OpenStreamSync(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to open QUIC stream")
		conn.Close()
		return
	}

	s := g.streamManager.Open(tunnelID, qstream)
	defer g.streamManager.Close(s.ID)

	if g.metrics != nil {
		g.metrics.ActiveStreams.Inc()
	}

	logger = logger.With().Str("stream_id", s.ID).Logger()
	logger.Info().Msg("stream opened")

	result, err := relay.Bidirectional(ctx, conn, qstream)
	if err != nil {
		logger.Debug().Err(err).Msg("relay ended with error")
	}

	if g.metrics != nil {
		g.metrics.ActiveStreams.Dec()
		g.metrics.StreamDuration.Observe(time.Since(s.CreatedAt).Seconds())
		g.metrics.BytesForwarded.WithLabelValues("sent", tunnelID).Add(float64(result.Stats.GetSent()))
		g.metrics.BytesForwarded.WithLabelValues("received", tunnelID).Add(float64(result.Stats.GetReceived()))
	}

	logger.Info().Int64("sent", result.Stats.GetSent()).Int64("received", result.Stats.GetReceived()).Msg("stream closed")
}

func (g *Gateway) heartbeatLoop(ctx context.Context, tunnelID string, conn *quic.Conn) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := g.registryClient.Heartbeat(ctx, &pb.HeartbeatRequest{TunnelId: tunnelID})
			if err != nil {
				log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("heartbeat failed")
			}
		}
	}
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

	at.cancelFunc()
	g.router.Deregister(tunnelID)
	g.streamManager.CloseByTunnel(tunnelID)
	at.conn.CloseWithError(0, reason)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = g.registryClient.DeregisterTunnel(ctx, &pb.DeregisterTunnelRequest{TunnelId: tunnelID})

	if g.metrics != nil {
		g.metrics.ActiveTunnels.Dec()
		g.metrics.TunnelDestroyed.WithLabelValues(reason).Inc()
	}

	log.Info().Str("tunnel_id", tunnelID).Str("reason", reason).Msg("tunnel removed")
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

func (g *Gateway) getQUICTLSConfig() (*tls.Config, error) {
	if g.tlsCertFile != "" && g.tlsKeyFile != "" {
		return transport.LoadTLSConfig(g.tlsCertFile, g.tlsKeyFile)
	}
	return transport.GenerateSelfSignedTLSConfig()
}

func (g *Gateway) getPublicTLSConfig() (*tls.Config, error) {
	if g.tlsCertFile != "" && g.tlsKeyFile != "" {
		return transport.PublicTLSConfigWithSNI(g.tlsCertFile, g.tlsKeyFile, g.router.HasHostname)
	}
	return transport.PublicTLSConfigWithSNISelfSigned(g.baseDomain, g.router.HasHostname)
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
