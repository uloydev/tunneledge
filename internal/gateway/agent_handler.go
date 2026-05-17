package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/internal/transport"
	"tunneledge/pkg/observability"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

func (g *Gateway) handleAgentConnection(ctx context.Context, conn *quic.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	ctx, span := otel.Tracer("tunneledge/gateway").Start(ctx, "gateway.handle_agent_connection",
		trace.WithAttributes(attribute.String("remote.addr", remoteAddr)),
	)
	defer span.End()
	traceID, spanID := observability.TraceIDs(ctx)
	logger := log.With().Str("remote_addr", remoteAddr).Str("trace_id", traceID).Str("span_id", spanID).Logger()

	acceptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	qstream, err := conn.AcceptStream(acceptCtx)
	if err != nil {
		span.RecordError(err)
		logger.Error().Err(err).Msg("failed to accept initial stream")
		conn.CloseWithError(1, "failed to accept auth stream")
		return
	}

	// First frame must be MsgHello for version negotiation.
	helloType, err := transport.ReadMessageType(qstream)
	if err != nil {
		span.RecordError(err)
		logger.Error().Err(err).Msg("failed to read hello message type")
		conn.CloseWithError(1, "auth failed")
		return
	}
	if helloType == transport.MsgHello {
		hello, hErr := transport.DecodeHello(qstream)
		if hErr != nil {
			span.RecordError(hErr)
			logger.Error().Err(hErr).Msg("failed to decode hello frame")
			conn.CloseWithError(1, "bad hello")
			return
		}
		if hello.Version != transport.ProtocolVersion {
			logger.Warn().Uint16("client_version", hello.Version).Uint16("server_version", transport.ProtocolVersion).Msg("protocol version mismatch")
			_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusVersionError, "", "")
			conn.CloseWithError(1, "unsupported protocol version")
			return
		}
		logger.Debug().Str("client_version", hello.ClientVersion).Msg("hello received")
		// Read the next message type (the actual auth frame).
		helloType, err = transport.ReadMessageType(qstream)
		if err != nil {
			span.RecordError(err)
			logger.Error().Err(err).Msg("failed to read auth message type after hello")
			conn.CloseWithError(1, "auth failed")
			return
		}
	}

	msgType := helloType

	var agentID string
	var tunnelID string
	var publicAddr string
	var localAddr string

	switch msgType {
	case transport.MsgAuthV2:
		agentID, tunnelID, publicAddr, localAddr, err = g.handleV2Auth(qstream, conn, logger)
	case transport.MsgAuth:
		r := io.MultiReader(bytes.NewReader([]byte{transport.MsgAuth}), qstream)
		agentID, tunnelID, publicAddr, localAddr, err = g.handleV1Auth(r, qstream, conn, logger)
	default:
		logger.Error().Msgf("unexpected message type: 0x%02x", msgType)
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		conn.CloseWithError(1, "unknown auth type")
		return
	}

	if err != nil {
		span.RecordError(err)
		conn.CloseWithError(1, "auth failed")
		return
	}
	span.SetAttributes(attribute.String("tunnel.id", tunnelID), attribute.String("agent.id", agentID))

	if err := g.registryClient.RegisterTunnel(tunnelID, agentID, publicAddr, localAddr); err != nil {
		span.RecordError(err)
		logger.Error().Err(err).Msg("failed to register tunnel with registry")
		conn.CloseWithError(1, "registry error")
		return
	}

	if g.metrics != nil {
		g.metrics.ActiveTunnels.Inc()
		g.metrics.TunnelCreated.WithLabelValues("success").Inc()
	}

	logger.Info().Str("tunnel_id", tunnelID).Msg("tunnel established")

	tunnelCtx, tunnelCancel := context.WithCancel(ctx)
	defer tunnelCancel()

	group, groupCtx := errgroup.WithContext(tunnelCtx)
	group.Go(func() error {
		g.heartbeatLoop(groupCtx, tunnelID)
		return nil
	})
	group.Go(func() error {
		defer tunnelCancel()
		g.agentStreamLoop(groupCtx, conn, tunnelID)
		return nil
	})

	_ = group.Wait()
}

func (g *Gateway) handleV1Auth(r io.Reader, qstream *quic.Stream, conn *quic.Conn, logger zerolog.Logger) (agentID, tunnelID, publicAddr, localAddr string, err error) {
	authMsg, err := transport.DecodeAuth(r)
	if err != nil {
		logger.Error().Err(err).Msg("failed to decode auth message")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		return "", "", "", "", err
	}

	agentID, err = g.authenticator.Authenticate(authMsg.Token)
	if err != nil {
		logger.Warn().Err(err).Msg("authentication failed")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		return "", "", "", "", err
	}

	tunnel := domain.NewActiveTunnel(agentID, []domain.TunnelRoute{{Label: "default"}})
	publicHost := g.router.Register(tunnel.ID.String())
	publicURL := fmt.Sprintf("%s:%s", publicHost, publicPort(g.publicListenAddr))
	tunnel.PublicHosts["default"] = publicURL

	if err := transport.EncodeAuthResponse(qstream, transport.AuthStatusOK, tunnel.ID.String(), publicURL); err != nil {
		logger.Error().Err(err).Msg("failed to send auth response")
		g.router.Deregister(tunnel.ID.String())
		return "", "", "", "", err
	}

	g.addTunnel(tunnel, conn)

	logger.Info().
		Str("tunnel_id", tunnel.ID.String()).
		Str("public_host", publicHost).
		Msg("tunnel established (v1 legacy)")

	return agentID, tunnel.ID.String(), publicURL, "", nil
}

func (g *Gateway) handleV2Auth(qstream *quic.Stream, conn *quic.Conn, logger zerolog.Logger) (agentID, tunnelID, publicAddr, localAddr string, err error) {
	r := io.MultiReader(bytes.NewReader([]byte{transport.MsgAuthV2}), qstream)
	authMsg, err := transport.DecodeAuthV2(r)
	if err != nil {
		logger.Error().Err(err).Msg("failed to decode auth v2 message")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		return "", "", "", "", err
	}

	agentID, err = g.authenticator.Authenticate(authMsg.Token)
	if err != nil {
		logger.Warn().Err(err).Msg("authentication failed")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		return "", "", "", "", err
	}

	routes := make([]domain.TunnelRoute, 0, len(authMsg.Tunnels))
	hostEntries := make([]transport.TunnelHostEntry, 0, len(authMsg.Tunnels))

	for _, t := range authMsg.Tunnels {
		if err := domain.ValidateLabel(t.Label); err != nil {
			logger.Warn().Str("label", t.Label).Err(err).Msg("invalid tunnel label")
			_ = transport.EncodeAuthV2Response(qstream, transport.AuthStatusError, "", nil)
			return "", "", "", "", fmt.Errorf("invalid label %q: %w", t.Label, err)
		}
		routes = append(routes, domain.TunnelRoute{Label: t.Label, LocalAddr: t.LocalAddr})
	}

	tunnel := domain.NewActiveTunnel(agentID, routes)

	var publicParts []string
	var localParts []string
	for _, t := range authMsg.Tunnels {
		hostname := g.router.RegisterLabel(tunnel.ID.String(), t.Label)
		publicURL := fmt.Sprintf("%s:%s", hostname, publicPort(g.publicListenAddr))
		tunnel.PublicHosts[t.Label] = publicURL
		hostEntries = append(hostEntries, transport.TunnelHostEntry{
			Label:    t.Label,
			Hostname: publicURL,
		})
		publicParts = append(publicParts, t.Label+"="+publicURL)
		if t.LocalAddr != "" {
			localParts = append(localParts, t.Label+"="+t.LocalAddr)
		}
	}

	if err := transport.EncodeAuthV2Response(qstream, transport.AuthStatusOK, tunnel.ID.String(), hostEntries); err != nil {
		logger.Error().Err(err).Msg("failed to send auth v2 response")
		g.router.DeregisterAll(tunnel.ID.String())
		return "", "", "", "", err
	}

	g.addTunnel(tunnel, conn)

	logger.Info().
		Str("tunnel_id", tunnel.ID.String()).
		Int("tunnel_count", len(authMsg.Tunnels)).
		Msg("tunnel established (v2 multi-tunnel)")

	return agentID, tunnel.ID.String(),
		strings.Join(publicParts, ","),
		strings.Join(localParts, ","),
		nil
}

func (g *Gateway) agentStreamLoop(ctx context.Context, conn *quic.Conn, tunnelID string) {
	for {
		select {
		case <-ctx.Done():
			g.removeTunnel(tunnelID, "shutdown")
			return
		default:
		}

		s, err := conn.AcceptStream(ctx)
		if err != nil {
			if ctx.Err() != nil {
				g.removeTunnel(tunnelID, "context_canceled")
				return
			}
			log.Error().Err(err).Str("tunnel_id", tunnelID).Msg("failed to accept stream")
			g.removeTunnel(tunnelID, "stream_error")
			return
		}

		// Read message type to identify heartbeats vs other streams
		msgType, err := transport.ReadMessageType(s)
		if err != nil {
			s.Close()
			continue
		}

		switch msgType {
		case transport.MsgHeartbeat:
			log.Debug().Str("tunnel_id", tunnelID).Msg("heartbeat received from agent")
			alive, err := g.registryClient.Heartbeat(tunnelID)
			if err != nil {
				log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("heartbeat registry update failed")
			} else if !alive {
				log.Warn().Str("tunnel_id", tunnelID).Msg("registry heartbeat reported expired session")
				g.removeTunnel(tunnelID, "session_expired")
				s.Close()
				return
			}
			s.Close()
		default:
			// Unknown message type on agent-initiated stream
			log.Debug().Str("tunnel_id", tunnelID).Uint8("msg_type", msgType).Msg("unknown message from agent")
			s.Close()
		}
	}
}

func (g *Gateway) heartbeatLoop(ctx context.Context, tunnelID string) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			alive, err := g.registryClient.Heartbeat(tunnelID)
			if err != nil {
				log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("heartbeat failed")
				continue
			}
			if !alive {
				log.Warn().Str("tunnel_id", tunnelID).Msg("registry heartbeat reported expired session")
				g.removeTunnel(tunnelID, "session_expired")
				return
			}
		}
	}
}
