package registry

import (
	"context"
	"fmt"
	"time"

	"tunneledge/internal/auth"
	"tunneledge/internal/session"
	pb "tunneledge/proto/registry/v1"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedRegistryServiceServer

	store         session.Store
	authenticator auth.Authenticator
	cleanupCancel context.CancelFunc
}

func NewServer(store session.Store, authenticator auth.Authenticator, cleanupInterval, sessionTTL time.Duration) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		store:         store,
		authenticator: authenticator,
		cleanupCancel: cancel,
	}
	go s.cleanupLoop(ctx, cleanupInterval, sessionTTL)
	return s
}

func (s *Server) Stop() {
	if s.cleanupCancel != nil {
		s.cleanupCancel()
	}
}

func (s *Server) RegisterTunnel(ctx context.Context, req *pb.RegisterTunnelRequest) (*pb.RegisterTunnelResponse, error) {
	logger := log.With().Str("tunnel_id", req.TunnelId).Str("agent_id", req.AgentId).Logger()

	if req.Token == "" {
		return nil, status.Error(codes.Unauthenticated, "token is required")
	}

	agentID, err := s.authenticator.Authenticate(req.Token)
	if err != nil {
		logger.Warn().Err(err).Msg("authentication failed")
		return nil, status.Error(codes.Unauthenticated, "authentication failed")
	}

	sess := &session.Session{
		TunnelID:   req.TunnelId,
		AgentID:    agentID,
		LocalAddr:  req.LocalAddr,
		RemoteAddr: "",
	}

	if err := s.store.Register(sess); err != nil {
		logger.Error().Err(err).Msg("failed to register tunnel")
		return nil, status.Errorf(codes.AlreadyExists, "tunnel %s already registered", req.TunnelId)
	}

	publicAddr := fmt.Sprintf(":%s", req.TunnelId)

	logger.Info().Str("public_addr", publicAddr).Msg("tunnel registered")

	return &pb.RegisterTunnelResponse{
		TunnelId:   req.TunnelId,
		PublicAddr: publicAddr,
	}, nil
}

func (s *Server) DeregisterTunnel(ctx context.Context, req *pb.DeregisterTunnelRequest) (*pb.DeregisterTunnelResponse, error) {
	logger := log.With().Str("tunnel_id", req.TunnelId).Logger()

	if err := s.store.Deregister(req.TunnelId); err != nil {
		logger.Error().Err(err).Msg("failed to deregister tunnel")
		return nil, status.Errorf(codes.NotFound, "tunnel %s not found", req.TunnelId)
	}

	logger.Info().Msg("tunnel deregistered")
	return &pb.DeregisterTunnelResponse{}, nil
}

func (s *Server) GetTunnel(ctx context.Context, req *pb.GetTunnelRequest) (*pb.GetTunnelResponse, error) {
	sess, err := s.store.Get(req.TunnelId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "tunnel %s not found", req.TunnelId)
	}

	return &pb.GetTunnelResponse{
		TunnelId:      sess.TunnelID,
		AgentId:       sess.AgentID,
		PublicAddr:    sess.PublicAddr,
		LocalAddr:     sess.LocalAddr,
		CreatedAt:     sess.CreatedAt.Unix(),
		LastHeartbeat: sess.LastHeartbeat.Unix(),
	}, nil
}

func (s *Server) ListTunnels(ctx context.Context, req *pb.ListTunnelsRequest) (*pb.ListTunnelsResponse, error) {
	sessions := s.store.List()

	tunnels := make([]*pb.GetTunnelResponse, 0, len(sessions))
	for _, sess := range sessions {
		tunnels = append(tunnels, &pb.GetTunnelResponse{
			TunnelId:      sess.TunnelID,
			AgentId:       sess.AgentID,
			PublicAddr:    sess.PublicAddr,
			LocalAddr:     sess.LocalAddr,
			CreatedAt:     sess.CreatedAt.Unix(),
			LastHeartbeat: sess.LastHeartbeat.Unix(),
		})
	}

	return &pb.ListTunnelsResponse{Tunnels: tunnels}, nil
}

func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if err := s.store.Heartbeat(req.TunnelId); err != nil {
		return &pb.HeartbeatResponse{Alive: false}, nil
	}
	return &pb.HeartbeatResponse{Alive: true}, nil
}

func (s *Server) cleanupLoop(ctx context.Context, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expired := s.store.CleanupExpired(ttl)
			if expired > 0 {
				log.Info().Int("expired", expired).Msg("cleaned up expired sessions")
			}
		}
	}
}
