package registry

import (
	"context"
	"time"

	"tunneledge/internal/domain"
	pb "tunneledge/proto/registry/v1"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedRegistryServiceServer

	store         domain.SessionRepository
	cleanupCancel context.CancelFunc
}

func NewServer(store domain.SessionRepository, cleanupInterval, sessionTTL time.Duration) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		store:         store,
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

	sess := &domain.Session{
		TunnelID:   req.TunnelId,
		AgentID:    req.AgentId,
		LocalAddr:  req.LocalAddr,
		PublicAddr: req.PublicAddr,
	}

	if err := s.store.Register(ctx, sess); err != nil {
		logger.Error().Err(err).Msg("failed to register tunnel")
		return nil, status.Errorf(codes.AlreadyExists, "tunnel %s already registered", req.TunnelId)
	}

	logger.Info().Str("tunnel_id", req.TunnelId).Msg("tunnel registered")

	return &pb.RegisterTunnelResponse{
		TunnelId:   req.TunnelId,
		PublicAddr: req.PublicAddr,
	}, nil
}

func (s *Server) DeregisterTunnel(ctx context.Context, req *pb.DeregisterTunnelRequest) (*pb.DeregisterTunnelResponse, error) {
	logger := log.With().Str("tunnel_id", req.TunnelId).Logger()

	if err := s.store.Deregister(ctx, req.TunnelId); err != nil {
		logger.Error().Err(err).Msg("failed to deregister tunnel")
		return nil, status.Errorf(codes.NotFound, "tunnel %s not found", req.TunnelId)
	}

	logger.Info().Msg("tunnel deregistered")
	return &pb.DeregisterTunnelResponse{}, nil
}

func (s *Server) GetTunnel(ctx context.Context, req *pb.GetTunnelRequest) (*pb.GetTunnelResponse, error) {
	sess, err := s.store.Get(ctx, req.TunnelId)
	if err != nil {
		log.Debug().Str("tunnel_id", req.TunnelId).Err(err).Msg("tunnel not found")
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
	sessions, err := s.store.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to list tunnels")
		return nil, status.Errorf(codes.Internal, "failed to list tunnels: %v", err)
	}

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
	if err := s.store.Heartbeat(ctx, req.TunnelId); err != nil {
		log.Warn().Str("tunnel_id", req.TunnelId).Err(err).Msg("heartbeat failed — session may have expired")
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
			expired, err := s.store.CleanupExpired(ctx, ttl)
			if err != nil {
				log.Error().Err(err).Msg("cleanup error")
			}
			if expired > 0 {
				log.Info().Int("expired", expired).Msg("cleaned up expired sessions")
			}
		}
	}
}
