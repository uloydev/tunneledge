package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/internal/transport"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

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

	msgType, err := transport.ReadMessageType(qstream)
	if err != nil {
		logger.Error().Err(err).Msg("failed to read message type")
		conn.CloseWithError(1, "auth failed")
		return
	}

	var agentID string
	var tunnelID string

	switch msgType {
	case transport.MsgAuthV2:
		agentID, tunnelID, err = g.handleV2Auth(qstream, conn, logger)
	case transport.MsgAuth:
		r := io.MultiReader(bytes.NewReader([]byte{transport.MsgAuth}), qstream)
		agentID, tunnelID, err = g.handleV1Auth(r, qstream, conn, logger)
	default:
		logger.Error().Msgf("unexpected message type: 0x%02x", msgType)
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		conn.CloseWithError(1, "unknown auth type")
		return
	}

	if err != nil {
		conn.CloseWithError(1, "auth failed")
		return
	}

	if err := g.registryClient.RegisterTunnel(tunnelID, agentID); err != nil {
		logger.Error().Err(err).Msg("failed to register tunnel with registry")
		conn.CloseWithError(1, "registry error")
		return
	}

	if g.metrics != nil {
		g.metrics.ActiveTunnels.Inc()
		g.metrics.TunnelCreated.WithLabelValues("success").Inc()
	}

	logger.Info().Str("tunnel_id", tunnelID).Msg("tunnel established")

	go g.heartbeatLoop(ctx, tunnelID)
	g.agentStreamLoop(ctx, conn, tunnelID)
}

func (g *Gateway) handleV1Auth(r io.Reader, qstream *quic.Stream, conn *quic.Conn, logger zerolog.Logger) (string, string, error) {
	authMsg, err := transport.DecodeAuth(r)
	if err != nil {
		logger.Error().Err(err).Msg("failed to decode auth message")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		return "", "", err
	}

	agentID, err := g.authenticator.Authenticate(authMsg.Token)
	if err != nil {
		logger.Warn().Err(err).Msg("authentication failed")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		return "", "", err
	}

	tunnel := domain.NewTunnel(agentID, []domain.TunnelRoute{{Label: "default"}})
	publicHost := g.router.Register(tunnel.ID.String())
	publicURL := fmt.Sprintf("%s:%s", publicHost, stripPort(g.publicListenAddr))
	tunnel.PublicHosts["default"] = publicURL

	if err := transport.EncodeAuthResponse(qstream, transport.AuthStatusOK, tunnel.ID.String(), publicURL); err != nil {
		logger.Error().Err(err).Msg("failed to send auth response")
		g.router.Deregister(tunnel.ID.String())
		return "", "", err
	}

	g.addTunnel(tunnel, conn)

	logger.Info().
		Str("tunnel_id", tunnel.ID.String()).
		Str("public_host", publicHost).
		Msg("tunnel established (v1 legacy)")

	return agentID, tunnel.ID.String(), nil
}

func (g *Gateway) handleV2Auth(qstream *quic.Stream, conn *quic.Conn, logger zerolog.Logger) (string, string, error) {
	r := io.MultiReader(bytes.NewReader([]byte{transport.MsgAuthV2}), qstream)
	authMsg, err := transport.DecodeAuthV2(r)
	if err != nil {
		logger.Error().Err(err).Msg("failed to decode auth v2 message")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		return "", "", err
	}

	agentID, err := g.authenticator.Authenticate(authMsg.Token)
	if err != nil {
		logger.Warn().Err(err).Msg("authentication failed")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		return "", "", err
	}

	routes := make([]domain.TunnelRoute, 0, len(authMsg.Tunnels))
	hostEntries := make([]transport.TunnelHostEntry, 0, len(authMsg.Tunnels))

	for _, t := range authMsg.Tunnels {
		if err := domain.ValidateLabel(t.Label); err != nil {
			logger.Warn().Str("label", t.Label).Err(err).Msg("invalid tunnel label")
			_ = transport.EncodeAuthV2Response(qstream, transport.AuthStatusError, "", nil)
			return "", "", fmt.Errorf("invalid label %q: %w", t.Label, err)
		}
		routes = append(routes, domain.TunnelRoute{Label: t.Label, LocalAddr: t.LocalAddr})
	}

	tunnel := domain.NewTunnel(agentID, routes)

	for _, t := range authMsg.Tunnels {
		hostname := g.router.RegisterLabel(tunnel.ID.String(), t.Label)
		publicURL := fmt.Sprintf("%s:%s", hostname, stripPort(g.publicListenAddr))
		tunnel.PublicHosts[t.Label] = publicURL
		hostEntries = append(hostEntries, transport.TunnelHostEntry{
			Label:    t.Label,
			Hostname: publicURL,
		})
	}

	if err := transport.EncodeAuthV2Response(qstream, transport.AuthStatusOK, tunnel.ID.String(), hostEntries); err != nil {
		logger.Error().Err(err).Msg("failed to send auth v2 response")
		g.router.DeregisterAll(tunnel.ID.String())
		return "", "", err
	}

	g.addTunnel(tunnel, conn)

	logger.Info().
		Str("tunnel_id", tunnel.ID.String()).
		Int("tunnel_count", len(authMsg.Tunnels)).
		Msg("tunnel established (v2 multi-tunnel)")

	return agentID, tunnel.ID.String(), nil
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
			if err := g.registryClient.Heartbeat(tunnelID); err != nil {
				log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("heartbeat registry update failed")
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
			if err := g.registryClient.Heartbeat(tunnelID); err != nil {
				log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("heartbeat failed")
			}
		}
	}
}
