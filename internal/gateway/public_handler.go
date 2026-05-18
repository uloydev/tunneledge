package gateway

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/netip"
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
		// Hostname not in the local routing table — check distributed router
		// for a peer gateway that owns this tunnel.
		if remote := g.distRouter.Lookup(hostname); remote != nil {
			span.SetAttributes(
				attribute.String("remote.relay_id", remote.OwnerRelayID),
				attribute.String("remote.addr", remote.OwnerAddr),
			)
			g.proxyToRemoteGateway(ctx, conn, hostname, remote)
			return
		}
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

	// Phase 4B: tunnel may be in session resume grace window (conn == nil).
	if at.suspended || at.conn == nil {
		log.Warn().Str("tunnel_id", tunnelID).Msg("tunnel is reconnecting; dropping connection")
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

	if g.maxStreamsPerTunnel > 0 && int64(g.streamManager.CountByTunnel(tunnelID)) >= g.maxStreamsPerTunnel {
		logger.Warn().Int64("max_streams_per_tunnel", g.maxStreamsPerTunnel).Msg("per-tunnel stream limit reached")
		conn.Close()
		return
	}

	// Tunnel ACL enforcement: deny-first CIDR rules.
	if g.tunnelACLs != nil {
		clientIPStr, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		clientAddr, parseErr := netip.ParseAddr(clientIPStr)
		if parseErr == nil {
			acls, aclErr := g.tunnelACLs.List(ctx, tunnelID)
			if aclErr == nil && len(acls) > 0 {
				allowed := false
				for _, acl := range acls {
					prefix, prefixErr := netip.ParsePrefix(acl.CIDR)
					if prefixErr != nil {
						continue
					}
					if prefix.Contains(clientAddr) {
						if acl.ACLType == "deny" {
							logger.Warn().Str("client_ip", clientIPStr).Str("cidr", acl.CIDR).Msg("ACL denied connection")
							conn.Close()
							return
						}
						allowed = true
					}
				}
				if !allowed {
					logger.Warn().Str("client_ip", clientIPStr).Msg("no matching ACL allow rule; denying connection")
					conn.Close()
					return
				}
			}
		}
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

// proxyToRemoteGateway TCP-forwards the already-established TLS conn to the
// public address of the gateway that holds the tunnel lease. The TLS stream is
// forwarded opaquely (the outer TLS layer from the edge is preserved and the
// inner application TLS negotiation is handled end-to-end by the owner
// gateway). This is intentionally a simple L4 splice rather than a QUIC
// stream because the remote gateway's public listener is a TLS/TCP endpoint.
func (g *Gateway) proxyToRemoteGateway(ctx context.Context, conn net.Conn, hostname string, remote *remoteRoute) {
	defer conn.Close()

	if remote.OwnerAddr == "" {
		log.Warn().
			Str("hostname", hostname).
			Str("owner_relay", remote.OwnerRelayID).
			Msg("remote relay address unknown, cannot forward")
		return
	}

	logger := log.With().
		Str("hostname", hostname).
		Str("owner_relay", remote.OwnerRelayID).
		Str("owner_addr", remote.OwnerAddr).
		Str("client_addr", conn.RemoteAddr().String()).
		Logger()

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dialer := &net.Dialer{}
	upstream, err := dialer.DialContext(dialCtx, "tcp", remote.OwnerAddr)
	if err != nil {
		logger.Error().Err(err).Msg("failed to connect to remote gateway")
		return
	}
	defer upstream.Close()

	logger.Info().Msg("proxying public connection to remote gateway")

	done := make(chan struct{}, 2)
	copy := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go copy(upstream, conn)
	go copy(conn, upstream)

	select {
	case <-done:
	case <-ctx.Done():
	}
}
