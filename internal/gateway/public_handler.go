package gateway

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"tunneledge/internal/relay"
	"tunneledge/internal/transport"

	"github.com/rs/zerolog/log"
)

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
		Logger()

	if g.maxStreams > 0 && int64(g.streamManager.CountByTunnel(tunnelID)) >= g.maxStreams {
		logger.Warn().Int64("max_streams", g.maxStreams).Msg("stream limit reached")
		conn.Close()
		return
	}

	qstream, err := at.conn.OpenStreamSync(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to open QUIC stream")
		conn.Close()
		return
	}

	if err := transport.EncodeStreamLabel(qstream, label); err != nil {
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
		logger.Debug().Err(err).Msg("relay ended with error")
	}

	if g.metrics != nil {
		g.metrics.ActiveStreams.Dec()
		g.metrics.StreamDuration.Observe(time.Since(s.CreatedAt).Seconds())
		g.metrics.BytesForwarded.WithLabelValues("sent").Add(float64(result.Stats.GetSent()))
		g.metrics.BytesForwarded.WithLabelValues("received").Add(float64(result.Stats.GetReceived()))
	}

	logger.Info().Int64("sent", result.Stats.GetSent()).Int64("received", result.Stats.GetReceived()).Msg("stream closed")
}
