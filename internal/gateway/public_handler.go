package gateway

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"tunneledge/internal/relay"
	"tunneledge/internal/transport"
	"tunneledge/pkg/observability"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (g *Gateway) handlePublicConnection(ctx context.Context, conn net.Conn) {
	ctx, span := otel.Tracer("tunneledge/gateway").Start(ctx, "gateway.handle_public_connection",
		trace.WithAttributes(attribute.String("client.addr", conn.RemoteAddr().String())),
	)
	defer span.End()
	traceID, spanID := observability.TraceIDs(ctx)

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		span.RecordError(context.Canceled)
		log.Error().Msg("expected TLS connection")
		conn.Close()
		return
	}

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		span.RecordError(err)
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
	span.SetAttributes(attribute.String("hostname", hostname))

	tunnelID, label, ok := g.router.LookupWithLabel(hostname)
	if !ok {
		log.Warn().Str("hostname", hostname).Msg("unknown tunnel hostname")
		conn.Close()
		return
	}

	at, ok := g.getTunnel(tunnelID)
	if !ok {
		log.Warn().Str("tunnel_id", tunnelID).Msg("tunnel not found for hostname")
		conn.Close()
		return
	}

	logger := log.With().
		Str("tunnel_id", tunnelID).
		Str("hostname", hostname).
		Str("label", label).
		Str("client_addr", conn.RemoteAddr().String()).
		Str("trace_id", traceID).
		Str("span_id", spanID).
		Logger()
	span.SetAttributes(attribute.String("tunnel.id", tunnelID), attribute.String("tunnel.label", label))

	if g.maxStreams > 0 && int64(g.streamManager.CountByTunnel(tunnelID)) >= g.maxStreams {
		logger.Warn().Int64("max_streams", g.maxStreams).Msg("stream limit reached")
		conn.Close()
		return
	}

	qstream, err := at.conn.OpenStreamSync(ctx)
	if err != nil {
		span.RecordError(err)
		logger.Error().Err(err).Msg("failed to open QUIC stream")
		conn.Close()
		return
	}

	if err := transport.EncodeStreamLabel(qstream, label); err != nil {
		span.RecordError(err)
		logger.Error().Err(err).Msg("failed to send stream label")
		qstream.Close()
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

	result, err := relay.BidirectionalWithIdleTimeout(ctx, conn, qstream, g.streamIdleTimeout, nil)
	if err != nil {
		span.RecordError(err)
		logger.Debug().Err(err).Msg("relay ended with error")
	}

	if g.metrics != nil {
		g.metrics.ActiveStreams.Dec()
		g.metrics.StreamDuration.Observe(time.Since(s.CreatedAt).Seconds())
		g.metrics.BytesForwarded.WithLabelValues("sent").Add(float64(result.Stats.GetSent()))
		g.metrics.BytesForwarded.WithLabelValues("received").Add(float64(result.Stats.GetReceived()))
		g.metrics.RelayDroppedFrames.Add(float64(result.Stats.GetDroppedFrames()))
		g.metrics.RelayQueueTimeouts.Add(float64(result.Stats.GetQueueTimeouts()))
		g.metrics.RelayWriteTimeouts.Add(float64(result.Stats.GetWriteTimeouts()))
		g.metrics.RelayQueueDepth.Observe(float64(result.Stats.GetMaxQueueDepth()))
	}

	logger.Info().Int64("sent", result.Stats.GetSent()).Int64("received", result.Stats.GetReceived()).Msg("stream closed")
}
