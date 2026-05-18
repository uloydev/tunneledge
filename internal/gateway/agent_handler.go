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

	// Phase 3: per-IP auth rate limiting.
	if g.authRateLimiter != nil {
		remoteIP, _, _ := strings.Cut(remoteAddr, ":")
		if remoteIP == "" {
			remoteIP = remoteAddr
		}
		if !g.authRateLimiter.Allow(remoteIP) {
			logger.Warn().Str("remote_ip", remoteIP).Msg("auth rate limit exceeded; closing connection")
			conn.CloseWithError(1, "rate limit exceeded")
			return
		}
	}

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
	case transport.MsgSessionResume:
		// Phase 4B: agent is attempting to resume an existing session.
		resumed, rErr := g.handleSessionResume(ctx, qstream, conn, logger)
		if rErr != nil {
			span.RecordError(rErr)
			conn.CloseWithError(1, "resume failed")
			return
		}
		if resumed {
			// Session resumed — conn is now attached; skip auth + registration.
			return
		}
		// Resume failed (expired/not found) — read the next frame for full auth.
		nextType, nErr := transport.ReadMessageType(qstream)
		if nErr != nil {
			span.RecordError(nErr)
			conn.CloseWithError(1, "auth failed after failed resume")
			return
		}
		switch nextType {
		case transport.MsgAuthV2:
			agentID, tunnelID, publicAddr, localAddr, err = g.handleV2Auth(ctx, qstream, conn, logger)
		case transport.MsgAuth:
			r := io.MultiReader(bytes.NewReader([]byte{transport.MsgAuth}), qstream)
			agentID, tunnelID, publicAddr, localAddr, err = g.handleV1Auth(ctx, r, qstream, conn, logger)
		default:
			logger.Error().Msgf("unexpected message type after failed resume: 0x%02x", nextType)
			conn.CloseWithError(1, "unknown auth type")
			return
		}
	case transport.MsgAuthV2:
		agentID, tunnelID, publicAddr, localAddr, err = g.handleV2Auth(ctx, qstream, conn, logger)
	case transport.MsgAuth:
		r := io.MultiReader(bytes.NewReader([]byte{transport.MsgAuth}), qstream)
		agentID, tunnelID, publicAddr, localAddr, err = g.handleV1Auth(ctx, r, qstream, conn, logger)
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
		g.removeTunnel(tunnelID, "registry_error")
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

func (g *Gateway) handleV1Auth(ctx context.Context, r io.Reader, qstream *quic.Stream, conn *quic.Conn, logger zerolog.Logger) (agentID, tunnelID, publicAddr, localAddr string, err error) {
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
	lease, err := g.acquireTunnelLease(ctx, tunnel.ID.String())
	if err != nil {
		logger.Warn().Err(err).Str("tunnel_id", tunnel.ID.String()).Msg("failed to acquire tunnel lease")
		_ = transport.EncodeAuthResponse(qstream, transport.AuthStatusError, "", "")
		g.router.Deregister(tunnel.ID.String())
		return "", "", "", "", err
	}

	if err := transport.EncodeAuthResponse(qstream, transport.AuthStatusOK, tunnel.ID.String(), publicURL); err != nil {
		logger.Error().Err(err).Msg("failed to send auth response")
		g.router.Deregister(tunnel.ID.String())
		_ = g.registryClient.ReleaseLease(context.Background(), lease.LeaseID)
		return "", "", "", "", err
	}

	g.addTunnel(tunnel, conn, lease.LeaseID)

	logger.Info().
		Str("tunnel_id", tunnel.ID.String()).
		Str("public_host", publicHost).
		Msg("tunnel established (v1 legacy)")

	return agentID, tunnel.ID.String(), publicURL, "", nil
}

func (g *Gateway) handleV2Auth(ctx context.Context, qstream *quic.Stream, conn *quic.Conn, logger zerolog.Logger) (agentID, tunnelID, publicAddr, localAddr string, err error) {
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

	// Phase 3: tunnel quota enforcement.
	if g.maxTunnelsPerAgent > 0 {
		if count := g.countTunnelsByAgent(agentID); count >= g.maxTunnelsPerAgent {
			logger.Warn().Str("agent_id", agentID).Int("count", count).Int("max", g.maxTunnelsPerAgent).Msg("tunnel quota exceeded")
			_ = transport.EncodeAuthV2Response(qstream, &transport.AuthV2ResponseMessage{Status: transport.AuthStatusError})
			return "", "", "", "", fmt.Errorf("tunnel quota exceeded for agent %s", agentID)
		}
	}

	routes := make([]domain.TunnelRoute, 0, len(authMsg.Tunnels))
	hostEntries := make([]transport.TunnelHostEntry, 0, len(authMsg.Tunnels))

	for _, t := range authMsg.Tunnels {
		if err := domain.ValidateLabel(t.Label); err != nil {
			logger.Warn().Str("label", t.Label).Err(err).Msg("invalid tunnel label")
			_ = transport.EncodeAuthV2Response(qstream, &transport.AuthV2ResponseMessage{Status: transport.AuthStatusError})
			return "", "", "", "", fmt.Errorf("invalid label %q: %w", t.Label, err)
		}
		routes = append(routes, domain.TunnelRoute{Label: t.Label, LocalAddr: t.LocalAddr, TunnelType: t.TunnelType})
	}

	tunnel := domain.NewActiveTunnel(agentID, routes)
	lease, err := g.acquireTunnelLease(ctx, tunnel.ID.String())
	if err != nil {
		logger.Warn().Err(err).Str("tunnel_id", tunnel.ID.String()).Msg("failed to acquire tunnel lease")
		_ = transport.EncodeAuthV2Response(qstream, &transport.AuthV2ResponseMessage{Status: transport.AuthStatusError})
		return "", "", "", "", err
	}

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

	// Phase 4B: issue a resume token so the agent can reconnect without full auth.
	var resumeToken string
	if g.sessionResumeEnabled && g.sessionRepo != nil {
		if tok, tokErr := generateResumeToken(); tokErr == nil {
			resumeToken = tok
		}
	}

	// Phase 4C: suggest best gateways for the agent's preferred region.
	var suggestedGWs []string
	if authMsg.PreferredRegion != "" {
		suggestedGWs = g.distRouter.BestRelaysForRegion(authMsg.PreferredRegion, 2)
	}

	if err := transport.EncodeAuthV2Response(qstream, &transport.AuthV2ResponseMessage{
		Status:            transport.AuthStatusOK,
		TunnelID:          tunnel.ID.String(),
		Tunnels:           hostEntries,
		AssignedRegion:    g.region,
		ResumeToken:       resumeToken,
		SuggestedGateways: suggestedGWs,
	}); err != nil {
		logger.Error().Err(err).Msg("failed to send auth v2 response")
		g.router.DeregisterAll(tunnel.ID.String())
		_ = g.registryClient.ReleaseLease(context.Background(), lease.LeaseID)
		return "", "", "", "", err
	}

	g.addTunnel(tunnel, conn, lease.LeaseID)

	// Store the resume token in the session repository so the agent can resume later.
	if resumeToken != "" {
		deadline := time.Now().Add(g.sessionResumeTTL)
		storeCtx, storeCancel := context.WithTimeout(ctx, 5*time.Second)
		if storeErr := g.sessionRepo.SetResumable(storeCtx, tunnel.ID.String(), resumeToken, deadline); storeErr != nil {
			logger.Warn().Err(storeErr).Str("tunnel_id", tunnel.ID.String()).Msg("failed to store initial resume token")
		}
		storeCancel()
	}

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
			// Agent's QUIC connection dropped — enter session resume grace window if enabled.
			log.Error().Err(err).Str("tunnel_id", tunnelID).Msg("agent connection lost")
			if g.sessionResumeEnabled && g.sessionRepo != nil {
				g.suspendTunnel(tunnelID, "agent_disconnect")
			} else {
				g.removeTunnel(tunnelID, "stream_error")
			}
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

// handleSessionResume processes a MsgSessionResume frame. It returns (true, nil)
// when the session is successfully resumed — the caller should skip full auth and
// continue directly to agentStreamLoop. It returns (false, nil) when the token is
// invalid/expired so the caller can fall through to full auth. A non-nil error
// indicates a protocol/IO failure and the connection should be closed.
func (g *Gateway) handleSessionResume(ctx context.Context, qstream *quic.Stream, conn *quic.Conn, logger zerolog.Logger) (bool, error) {
	resumeMsg, err := transport.DecodeSessionResume(
		io.MultiReader(bytes.NewReader([]byte{transport.MsgSessionResume}), qstream),
	)
	if err != nil {
		return false, fmt.Errorf("failed to decode session resume: %w", err)
	}

	if g.sessionRepo == nil {
		// Resume not supported on this gateway.
		if err := transport.EncodeSessionResumeResp(qstream, &transport.SessionResumeRespMessage{
			Status: transport.ResumeStatusNotFound,
		}); err != nil {
			return false, err
		}
		return false, nil
	}

	lookupCtx, lookupCancel := context.WithTimeout(ctx, 5*time.Second)
	sess, lookupErr := g.sessionRepo.GetResumable(lookupCtx, resumeMsg.Token)
	lookupCancel()

	if lookupErr != nil {
		logger.Warn().Str("token_prefix", safePrefix(resumeMsg.Token, 8)).Msg("session resume token not found or expired")
		if encErr := transport.EncodeSessionResumeResp(qstream, &transport.SessionResumeRespMessage{
			Status: transport.ResumeStatusExpired,
		}); encErr != nil {
			return false, encErr
		}
		return false, nil
	}

	tunnelID := sess.TunnelID

	// Cancel the finalization timer — the agent has reconnected in time.
	g.resumeMu.Lock()
	if t, ok := g.pendingResumeTimers[tunnelID]; ok {
		t.Stop()
		delete(g.pendingResumeTimers, tunnelID)
	}
	g.resumeMu.Unlock()

	// Issue a new resume token for the next reconnect.
	newToken, genErr := generateResumeToken()
	if genErr != nil {
		newToken = "" // non-fatal; agent will fall back to full auth next time
	}

	// Re-attach the new QUIC connection to the existing tunnel entry.
	g.mu.Lock()
	at, ok := g.tunnels[tunnelID]
	if !ok {
		g.mu.Unlock()
		// Tunnel was fully removed while we were processing — treat as not found.
		if encErr := transport.EncodeSessionResumeResp(qstream, &transport.SessionResumeRespMessage{
			Status: transport.ResumeStatusNotFound,
		}); encErr != nil {
			return false, encErr
		}
		return false, nil
	}
	at.conn = conn
	at.suspended = false
	g.mu.Unlock()

	// Update the resume token in the repository.
	if newToken != "" {
		deadline := time.Now().Add(g.sessionResumeTTL)
		storeCtx, storeCancel := context.WithTimeout(ctx, 5*time.Second)
		if storeErr := g.sessionRepo.SetResumable(storeCtx, tunnelID, newToken, deadline); storeErr != nil {
			logger.Warn().Err(storeErr).Str("tunnel_id", tunnelID).Msg("failed to update resume token after resume")
			newToken = "" // don't send an invalid token to the agent
		}
		storeCancel()
	}

	if err := transport.EncodeSessionResumeResp(qstream, &transport.SessionResumeRespMessage{
		Status:   transport.ResumeStatusResumed,
		TunnelID: tunnelID,
		NewToken: newToken,
	}); err != nil {
		return false, fmt.Errorf("failed to send resume response: %w", err)
	}

	logger.Info().
		Str("tunnel_id", tunnelID).
		Str("agent_id", sess.AgentID).
		Msg("session resumed")

	// Continue the tunnel lifecycle with the new connection.
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

	return true, nil
}

// safePrefix returns up to n characters of s, or all of s if shorter.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// countTunnelsByAgent returns the number of active tunnels associated with agentID.
func (g *Gateway) countTunnelsByAgent(agentID string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	count := 0
	for _, at := range g.tunnels {
		if at.tunnel.AgentID == agentID {
			count++
		}
	}
	return count
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
			at, ok := g.getTunnel(tunnelID)
			if ok && at.leaseID != "" {
				if _, err := g.registryClient.RenewLease(ctx, at.leaseID, g.leaseTTL); err != nil {
					log.Warn().Err(err).Str("tunnel_id", tunnelID).Str("lease_id", at.leaseID).Msg("failed to renew tunnel lease")
					g.removeTunnel(tunnelID, "lease_lost")
					return
				}
			}
		}
	}
}
