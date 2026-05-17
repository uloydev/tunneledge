package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"tunneledge/internal/relay"
	"tunneledge/internal/transport"
	"tunneledge/pkg/config"
	"tunneledge/pkg/metrics"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

type Agent struct {
	gatewayAddr       string
	token             string
	tunnels           []config.TunnelConfig
	tunnelMap         map[string]string
	metrics           *metrics.Metrics
	reconnectDelay    time.Duration
	maxReconnect      int
	heartbeatInterval time.Duration
	streamIdleTimeout time.Duration
	tlsCAFile         string
	tlsInsecure       bool

	mu          sync.RWMutex
	conn        *quic.Conn
	tunnelID    string
	publicURLs  map[string]string
	reconnectCh chan struct{}

	// Event emission — nil when running headless.
	eventCh chan<- AgentEvent

	// Atomic bandwidth counters updated from relay goroutines.
	rxTotal atomic.Int64
	txTotal atomic.Int64
}

type Options struct {
	GatewayAddr       string
	Token             string
	Tunnels           []config.TunnelConfig
	ReconnectDelay    time.Duration
	MaxReconnect      int
	HeartbeatInterval time.Duration
	StreamIdleTimeout time.Duration
	Metrics           *metrics.Metrics
	TLSCAFile         string
	TLSInsecure       bool

	// EventCh is an optional buffered channel for broadcasting AgentEvents to the TUI.
	// If nil, all event emission is skipped (headless mode).
	EventCh chan<- AgentEvent
}

func NewAgent(opts Options) *Agent {
	tunnelMap := make(map[string]string, len(opts.Tunnels))
	for _, t := range opts.Tunnels {
		tunnelMap[t.Label] = t.LocalAddr
	}

	return &Agent{
		gatewayAddr:       opts.GatewayAddr,
		token:             opts.Token,
		tunnels:           opts.Tunnels,
		tunnelMap:         tunnelMap,
		reconnectDelay:    opts.ReconnectDelay,
		maxReconnect:      opts.MaxReconnect,
		heartbeatInterval: opts.HeartbeatInterval,
		streamIdleTimeout: agentStreamIdleTimeout(opts.StreamIdleTimeout),
		metrics:           opts.Metrics,
		publicURLs:        make(map[string]string),
		tlsCAFile:         opts.TLSCAFile,
		tlsInsecure:       opts.TLSInsecure,
		reconnectCh:       make(chan struct{}, 1),
		eventCh:           opts.EventCh,
	}
}

// agentStreamIdleTimeout returns v when positive, otherwise the safe default of 30s.
func agentStreamIdleTimeout(v time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return 30 * time.Second
}

// emitEvent sends ev to the TUI event channel without blocking.
// If the channel buffer is full the event is silently dropped so that
// the transport layer is never stalled by a slow UI render.
func (a *Agent) emitEvent(ev AgentEvent) {
	if a.eventCh == nil {
		return
	}
	select {
	case a.eventCh <- ev:
	default:
	}
}

// telemetryLoop emits TelemetryTickEvents at 500 ms intervals by computing
// incremental deltas from the atomic byte counters.
func (a *Agent) telemetryLoop(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastRx, lastTx int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			curRx := a.rxTotal.Load()
			curTx := a.txTotal.Load()
			rxDelta := curRx - lastRx
			txDelta := curTx - lastTx
			lastRx = curRx
			lastTx = curTx
			a.emitEvent(TelemetryTickEvent{
				RxDelta: uint64(rxDelta),
				TxDelta: uint64(txDelta),
			})
		}
	}
}

func (a *Agent) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(runCtx)
	if a.eventCh != nil {
		group.Go(func() error {
			a.telemetryLoop(groupCtx)
			return nil
		})
	}

	err := a.runConnectLoop(groupCtx)
	cancel()
	_ = group.Wait()
	return err
}


func (a *Agent) runConnectLoop(ctx context.Context) error {
	attempts := 0
	delay := a.reconnectDelay
	maxRetries := a.maxReconnect

	for {
		select {
		case <-ctx.Done():
			a.emitEvent(StatusUpdateEvent{Status: "Disconnected"})
			return ctx.Err()
		default:
		}

		a.emitEvent(StatusUpdateEvent{Status: "Connecting"})
		err := a.connect(ctx)
		if err == nil {
			attempts = 0
			delay = a.reconnectDelay
			continue
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if maxRetries > 0 {
			maxRetries--
			if maxRetries == 0 {
				return fmt.Errorf("max reconnect attempts reached: %w", err)
			}
		}

		attempts++
		if a.metrics != nil {
			a.metrics.ReconnectTotal.Inc()
		}
		jitter := time.Duration(rand.Int64N(int64(delay) / 2))
		waitTime := delay + jitter

		log.Warn().
			Err(err).
			Int("attempt", attempts).
			Dur("retry_after", waitTime).
			Msg("connection lost, reconnecting")

		a.emitEvent(StatusUpdateEvent{Status: "Reconnecting"})

		select {
		case <-ctx.Done():
			a.emitEvent(StatusUpdateEvent{Status: "Disconnected"})
			return ctx.Err()
		case <-time.After(waitTime):
		}

		delay = time.Duration(math.Min(float64(delay*2), float64(30*time.Second)))
	}
}

func (a *Agent) connect(ctx context.Context) (err error) {
	ctx, span := otel.Tracer("tunneledge/agent").Start(ctx, "agent.connect",
		trace.WithAttributes(attribute.String("gateway.addr", a.gatewayAddr)),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()

	var tlsCfg *tls.Config

	if a.tlsCAFile != "" {
		var err error
		tlsCfg, err = transport.ClientTLSConfigWithCA(a.tlsCAFile)
		if err != nil {
			return fmt.Errorf("load TLS CA: %w", err)
		}
	} else {
		tlsCfg = transport.ClientTLSConfig()
		if !a.tlsInsecure {
			tlsCfg.InsecureSkipVerify = false
		}
	}

	conn, err := transport.QUICDial(ctx, a.gatewayAddr, tlsCfg)
	if err != nil {
		return fmt.Errorf("failed to dial gateway: %w", err)
	}

	a.setConn(conn)

	log.Info().Str("gateway_addr", a.gatewayAddr).Msg("QUIC connection established")
	defer func() {
		a.setConn(nil)
		conn.CloseWithError(0, "agent shutting down")
		log.Info().Msg("QUIC connection closed")
	}()

	authStream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("failed to open auth stream: %w", err)
	}
	defer authStream.Close()

	// Send protocol Hello so the gateway can reject incompatible clients early.
	if err := transport.EncodeHello(authStream, "tunneledge/agent"); err != nil {
		return fmt.Errorf("failed to send hello: %w", err)
	}

	if len(a.tunnels) > 0 {
		return a.authenticateV2(ctx, conn, authStream)
	}
	return a.authenticateV1(ctx, conn, authStream)
}

func (a *Agent) setTunnelInfo(tunnelID string, publicURLs map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tunnelID = tunnelID
	a.publicURLs = publicURLs
}

func (a *Agent) getConn() *quic.Conn {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.conn
}

func (a *Agent) setConn(conn *quic.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conn = conn
}

func (a *Agent) authenticateV1(ctx context.Context, conn *quic.Conn, authStream *quic.Stream) error {
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

	a.setTunnelInfo(authResp.TunnelID, map[string]string{"default": authResp.PublicURL})

	log.Info().
		Str("tunnel_id", authResp.TunnelID).
		Str("public_url", authResp.PublicURL).
		Msg("authenticated with gateway (v1)")

	a.emitEvent(StatusUpdateEvent{Status: "Connected", Endpoint: authResp.PublicURL})

	if a.metrics != nil {
		a.metrics.ActiveTunnels.Inc()
		a.metrics.TunnelCreated.WithLabelValues("success").Inc()
	}

	return a.streamLoop(ctx, conn)
}

func (a *Agent) authenticateV2(ctx context.Context, conn *quic.Conn, authStream *quic.Stream) error {
	tunnelEntries := make([]transport.TunnelEntry, 0, len(a.tunnels))
	for _, t := range a.tunnels {
		tunnelEntries = append(tunnelEntries, transport.TunnelEntry{
			Label:     t.Label,
			LocalAddr: t.LocalAddr,
		})
	}

	if err := transport.EncodeAuthV2(authStream, a.token, tunnelEntries); err != nil {
		return fmt.Errorf("failed to send auth v2: %w", err)
	}

	authResp, err := transport.DecodeAuthV2Response(authStream)
	if err != nil {
		return fmt.Errorf("failed to read auth v2 response: %w", err)
	}

	if authResp.Status != transport.AuthStatusOK {
		return fmt.Errorf("authentication rejected by gateway")
	}

	publicURLs := make(map[string]string, len(authResp.Tunnels))
	for _, t := range authResp.Tunnels {
		publicURLs[t.Label] = t.Hostname
		log.Info().
			Str("tunnel_id", authResp.TunnelID).
			Str("label", t.Label).
			Str("public_url", t.Hostname).
			Msg("tunnel registered")
	}

	a.setTunnelInfo(authResp.TunnelID, publicURLs)

	log.Info().
		Str("tunnel_id", authResp.TunnelID).
		Int("tunnel_count", len(authResp.Tunnels)).
		Msg("authenticated with gateway (v2 multi-tunnel)")

	// Emit the first registered public URL as the primary endpoint.
	primaryEndpoint := ""
	if len(authResp.Tunnels) > 0 {
		primaryEndpoint = authResp.Tunnels[0].Hostname
	}
	a.emitEvent(StatusUpdateEvent{Status: "Connected", Endpoint: primaryEndpoint})

	if a.metrics != nil {
		a.metrics.ActiveTunnels.Inc()
		a.metrics.TunnelCreated.WithLabelValues("success").Inc()
	}

	return a.streamLoop(ctx, conn)
}

func (a *Agent) streamLoop(ctx context.Context, conn *quic.Conn) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, groupCtx := errgroup.WithContext(streamCtx)
	g.Go(func() error {
		a.heartbeatLoop(groupCtx)
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			cancel()
			_ = g.Wait()
			return ctx.Err()
		default:
		}

		qstream, err := conn.AcceptStream(groupCtx)
		if err != nil {
			cancel()
			_ = g.Wait()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("failed to accept stream: %w", err)
		}

		stream := qstream
		g.Go(func() error {
			a.handleStream(groupCtx, stream)
			return nil
		})
	}
}

func (a *Agent) handleStream(ctx context.Context, qstream *quic.Stream) {
	streamID := fmt.Sprintf("s-%d", qstream.StreamID())

	a.mu.RLock()
	tunnelID := a.tunnelID
	a.mu.RUnlock()

	logger := log.With().Str("tunnel_id", tunnelID).Str("stream_id", streamID).Logger()
	ctx, span := otel.Tracer("tunneledge/agent").Start(ctx, "agent.handle_stream",
		trace.WithAttributes(
			attribute.String("tunnel.id", tunnelID),
			attribute.String("stream.id", streamID),
		),
	)
	defer span.End()

	label, err := transport.DecodeStreamLabel(qstream)
	if err != nil {
		span.RecordError(err)
		logger.Error().Err(err).Msg("failed to read stream label")
		qstream.Close()
		return
	}

	localAddr, ok := a.tunnelMap[label]
	if !ok {
		err = fmt.Errorf("unknown tunnel label: %s", label)
		span.RecordError(err)
		logger.Error().Str("label", label).Msg("unknown tunnel label")
		qstream.Close()
		return
	}
	span.SetAttributes(
		attribute.String("tunnel.label", label),
		attribute.String("local.addr", localAddr),
	)

	logger = logger.With().Str("label", label).Str("local_addr", localAddr).Logger()
	logger.Info().Msg("incoming stream from gateway")

	a.emitEvent(StreamOpenedEvent{
		StreamID:  streamID,
		Label:     label,
		LocalAddr: localAddr,
	})

	tcpConn, err := net.DialTimeout("tcp", localAddr, 10*time.Second)
	if err != nil {
		span.RecordError(err)
		logger.Error().Err(err).Msg("failed to connect to local service")
		qstream.Close()
		a.emitEvent(StreamClosedEvent{
			StreamID: streamID,
			Label:    label,
			Reason:   err.Error(),
		})
		return
	}
	defer tcpConn.Close()

	if a.metrics != nil {
		a.metrics.ActiveStreams.Inc()
	}

	// BidirectionalWithIdleTimeout tracks per-stream bytes, accumulates into
	// the agent's atomic totals for telemetry, and closes idle streams after
	// a.streamIdleTimeout of inactivity.
	result, relayErr := relay.BidirectionalWithIdleTimeout(ctx, qstream, tcpConn, a.streamIdleTimeout, func(direction string, n int) {
		switch direction {
		case "sent": // qstream → tcpConn (data received from gateway)
			a.rxTotal.Add(int64(n))
		case "received": // tcpConn → qstream (data sent to gateway)
			a.txTotal.Add(int64(n))
		}
	})
	if relayErr != nil {
		span.RecordError(relayErr)
		logger.Debug().Err(relayErr).Msg("relay ended with error")
	}

	if a.metrics != nil {
		a.metrics.ActiveStreams.Dec()
		a.metrics.BytesForwarded.WithLabelValues("sent").Add(float64(result.Stats.GetSent()))
		a.metrics.BytesForwarded.WithLabelValues("received").Add(float64(result.Stats.GetReceived()))
		a.metrics.RelayDroppedFrames.Add(float64(result.Stats.GetDroppedFrames()))
		a.metrics.RelayQueueTimeouts.Add(float64(result.Stats.GetQueueTimeouts()))
		a.metrics.RelayWriteTimeouts.Add(float64(result.Stats.GetWriteTimeouts()))
		a.metrics.RelayQueueDepth.Observe(float64(result.Stats.GetMaxQueueDepth()))
	}

	logger.Info().Int64("sent", result.Stats.GetSent()).Int64("received", result.Stats.GetReceived()).Msg("stream closed")

	reason := "clean"
	if relayErr != nil {
		reason = relayErr.Error()
	}
	a.emitEvent(StreamClosedEvent{
		StreamID:      streamID,
		Label:         label,
		Reason:        reason,
		SentBytes:     result.Stats.GetSent(),
		ReceivedBytes: result.Stats.GetReceived(),
	})
}

func (a *Agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			conn := a.getConn()
			if conn == nil {
				return
			}
			s, err := conn.OpenStreamSync(ctx)
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
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tunnelID
}

func (a *Agent) PublicURLs() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make(map[string]string, len(a.publicURLs))
	for k, v := range a.publicURLs {
		result[k] = v
	}
	return result
}
