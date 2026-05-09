package agent

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"time"

	"tunneledge/internal/relay"
	"tunneledge/internal/transport"
	"tunneledge/pkg/metrics"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
)

type Agent struct {
	gatewayAddr       string
	token             string
	localAddr         string
	tlsConfig         *tlsConfig
	conn              *quic.Conn
	tunnelID          string
	metrics           *metrics.Metrics
	reconnectDelay    time.Duration
	maxReconnect      int
	heartbeatInterval time.Duration
}

type tlsConfig struct {
	skipVerify bool
}

type Options struct {
	GatewayAddr       string
	Token             string
	LocalAddr         string
	ReconnectDelay    time.Duration
	MaxReconnect      int
	HeartbeatInterval time.Duration
	Metrics           *metrics.Metrics
}

func NewAgent(opts Options) *Agent {
	return &Agent{
		gatewayAddr:       opts.GatewayAddr,
		token:             opts.Token,
		localAddr:         opts.LocalAddr,
		reconnectDelay:    opts.ReconnectDelay,
		maxReconnect:      opts.MaxReconnect,
		heartbeatInterval: opts.HeartbeatInterval,
		metrics:           opts.Metrics,
		tlsConfig:         &tlsConfig{skipVerify: true},
	}
}

func (a *Agent) Run(ctx context.Context) error {
	attempts := 0
	delay := a.reconnectDelay

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := a.connect(ctx)
		if err == nil {
			attempts = 0
			delay = a.reconnectDelay
			continue
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		a.maxReconnect--
		if a.maxReconnect == 0 {
			return fmt.Errorf("max reconnect attempts reached: %w", err)
		}

		attempts++
		jitter := time.Duration(rand.Int64N(int64(delay) / 2))
		waitTime := delay + jitter

		log.Warn().
			Err(err).
			Int("attempt", attempts).
			Dur("retry_after", waitTime).
			Msg("connection lost, reconnecting")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}

		delay = time.Duration(math.Min(float64(delay*2), float64(30*time.Second)))
	}
}

func (a *Agent) connect(ctx context.Context) error {
	tlsCfg := transport.ClientTLSConfig()
	if !a.tlsConfig.skipVerify {
		tlsCfg.InsecureSkipVerify = false
	}

	conn, err := transport.QUICDial(ctx, a.gatewayAddr, tlsCfg)
	if err != nil {
		return fmt.Errorf("failed to dial gateway: %w", err)
	}
	a.conn = conn

	log.Info().Str("gateway_addr", a.gatewayAddr).Msg("QUIC connection established")
	defer func() {
		conn.CloseWithError(0, "agent shutting down")
		log.Info().Msg("QUIC connection closed")
	}()

	authStream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("failed to open auth stream: %w", err)
	}
	defer authStream.Close()

	if err := transport.EncodeAuth(authStream, a.token); err != nil {
		return fmt.Errorf("failed to send auth: %w", err)
	}

	authResp, err := transport.DecodeAuthResponse(authStream)
	if err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	if authResp.Status != transport.AuthStatusOK {
		return fmt.Errorf("authentication rejected by gateway")
	}

	a.tunnelID = authResp.TunnelID
	log.Info().
		Str("tunnel_id", a.tunnelID).
		Str("public_url", authResp.PublicURL).
		Msg("authenticated with gateway")

	if a.metrics != nil {
		a.metrics.ActiveTunnels.Inc()
		a.metrics.TunnelCreated.WithLabelValues("success").Inc()
	}

	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	go a.heartbeatLoop(heartbeatCtx)
	defer heartbeatCancel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		qstream, err := conn.AcceptStream(ctx)
		if err != nil {
			return fmt.Errorf("failed to accept stream: %w", err)
		}

		go a.handleStream(ctx, qstream)
	}
}

func (a *Agent) handleStream(ctx context.Context, qstream *quic.Stream) {
	streamID := fmt.Sprintf("s-%d", qstream.StreamID())
	logger := log.With().Str("tunnel_id", a.tunnelID).Str("stream_id", streamID).Logger()

	logger.Info().Msg("incoming stream from gateway")

	tcpConn, err := net.DialTimeout("tcp", a.localAddr, 10*time.Second)
	if err != nil {
		logger.Error().Err(err).Str("local_addr", a.localAddr).Msg("failed to connect to local service")
		qstream.Close()
		return
	}
	defer tcpConn.Close()

	if a.metrics != nil {
		a.metrics.ActiveStreams.Inc()
	}

	result, _ := relay.Bidirectional(ctx, qstream, tcpConn)

	if a.metrics != nil {
		a.metrics.ActiveStreams.Dec()
		a.metrics.BytesForwarded.WithLabelValues("sent", a.tunnelID).Add(float64(result.Stats.GetSent()))
		a.metrics.BytesForwarded.WithLabelValues("received", a.tunnelID).Add(float64(result.Stats.GetReceived()))
	}

	logger.Info().Int64("sent", result.Stats.GetSent()).Int64("received", result.Stats.GetReceived()).Msg("stream closed")
}

func (a *Agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s, err := a.conn.OpenStreamSync(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("failed to open heartbeat stream")
				return
			}
			if err := transport.EncodeHeartbeat(s); err != nil {
				log.Warn().Err(err).Msg("failed to send heartbeat")
				s.Close()
				return
			}
			s.Close()
		}
	}
}

func (a *Agent) TunnelID() string {
	return a.tunnelID
}
