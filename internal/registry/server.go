package registry

import (
	"context"
	"sync"
	"time"

	"tunneledge/internal/domain"
	pb "tunneledge/proto/registry/v1"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the gRPC RegistryService. All distributed coordination
// is delegated to the Coordinator backend (MemCoordinator or EtcdCoordinator).
type Server struct {
	pb.UnimplementedRegistryServiceServer

	store         domain.SessionRepository
	coord         Coordinator
	cleanupCancel context.CancelFunc
	cleanupWG     sync.WaitGroup
}

// NewServer creates a registry Server. coord may be nil, in which case a
// NewMemCoordinator is created automatically (single-node default).
func NewServer(store domain.SessionRepository, coord Coordinator, cleanupInterval, sessionTTL time.Duration) *Server {
	if coord == nil {
		coord = NewMemCoordinator()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		store:         store,
		coord:         coord,
		cleanupCancel: cancel,
	}
	s.cleanupWG.Add(1)
	go func() {
		defer s.cleanupWG.Done()
		s.cleanupLoop(ctx, cleanupInterval, sessionTTL)
	}()
	return s
}

func (s *Server) Stop() {
	if s.cleanupCancel != nil {
		s.cleanupCancel()
	}
	s.cleanupWG.Wait()
}

// ─── gRPC handlers ─────────────────────────────────────────────────────────

func (s *Server) RegisterTunnel(ctx context.Context, req *pb.RegisterTunnelRequest) (*pb.RegisterTunnelResponse, error) {
	logger := log.With().Str("tunnel_id", req.TunnelId).Str("agent_id", req.AgentId).Logger()

	sess := &domain.Session{
		TunnelID:   req.TunnelId,
		AgentID:    req.AgentId,
		LocalAddr:  req.LocalAddr,
		PublicAddr: req.PublicAddr,
	}
	if lease := s.coord.CurrentLease(req.TunnelId); lease != nil {
		sess.OwnerRelayID = lease.RelayID
		sess.LeaseID = lease.LeaseID
	}

	if err := s.store.Register(ctx, sess); err != nil {
		logger.Error().Err(err).Msg("failed to register tunnel")
		return nil, status.Errorf(codes.AlreadyExists, "tunnel %s already registered", req.TunnelId)
	}

	logger.Info().Str("tunnel_id", req.TunnelId).Msg("tunnel registered")
	s.coord.Publish(domain.RegistryEvent{
		Type:   domain.RegistryEventTunnelUpserted,
		Tunnel: sess,
	})

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
	s.coord.Publish(domain.RegistryEvent{
		Type:   domain.RegistryEventTunnelDeleted,
		Tunnel: &domain.Session{TunnelID: req.TunnelId},
	})
	return &pb.DeregisterTunnelResponse{}, nil
}

func (s *Server) GetTunnel(ctx context.Context, req *pb.GetTunnelRequest) (*pb.GetTunnelResponse, error) {
	sess, err := s.store.Get(ctx, req.TunnelId)
	if err != nil {
		log.Debug().Str("tunnel_id", req.TunnelId).Err(err).Msg("tunnel not found")
		return nil, status.Errorf(codes.NotFound, "tunnel %s not found", req.TunnelId)
	}
	if lease := s.coord.CurrentLease(sess.TunnelID); lease != nil {
		sess.OwnerRelayID = lease.RelayID
		sess.LeaseID = lease.LeaseID
	}
	return s.sessionToProto(sess), nil
}

func (s *Server) ListTunnels(ctx context.Context, req *pb.ListTunnelsRequest) (*pb.ListTunnelsResponse, error) {
	sessions, err := s.store.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to list tunnels")
		return nil, status.Errorf(codes.Internal, "failed to list tunnels: %v", err)
	}

	tunnels := make([]*pb.GetTunnelResponse, 0, len(sessions))
	for _, sess := range sessions {
		if lease := s.coord.CurrentLease(sess.TunnelID); lease != nil {
			sess.OwnerRelayID = lease.RelayID
			sess.LeaseID = lease.LeaseID
		}
		tunnels = append(tunnels, s.sessionToProto(sess))
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

func (s *Server) Watch(req *pb.WatchRequest, stream pb.RegistryService_WatchServer) error {
	ctx := stream.Context()
	tunnelFilter := req.GetTunnelId()
	relayFilter := req.GetRelayId()

	events := s.coord.Subscribe(ctx)

	if req.GetIncludeExisting() {
		if err := s.sendSnapshot(stream, tunnelFilter, relayFilter); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if !matchesDomainEvent(event, tunnelFilter, relayFilter) {
				continue
			}
			protoEvent := s.domainEventToProto(event)
			if protoEvent == nil {
				continue
			}
			if err := stream.Send(protoEvent); err != nil {
				return err
			}
		}
	}
}

func (s *Server) AcquireLease(ctx context.Context, req *pb.AcquireLeaseRequest) (*pb.AcquireLeaseResponse, error) {
	ttl := ttlFromSeconds(req.GetTtlSeconds())
	lease, err := s.coord.AcquireLease(ctx, domain.LeaseRequest{
		TunnelID: req.GetTunnelId(),
		RelayID:  req.GetRelayId(),
		TTL:      ttl,
	})
	if err != nil {
		return nil, err
	}
	return &pb.AcquireLeaseResponse{Lease: s.leaseToProto(lease)}, nil
}

func (s *Server) RenewLease(ctx context.Context, req *pb.RenewLeaseRequest) (*pb.RenewLeaseResponse, error) {
	ttl := ttlFromSeconds(req.GetTtlSeconds())
	lease, err := s.coord.RenewLease(ctx, req.GetLeaseId(), ttl)
	if err != nil {
		return nil, err
	}
	return &pb.RenewLeaseResponse{Lease: s.leaseToProto(lease)}, nil
}

func (s *Server) ReleaseLease(ctx context.Context, req *pb.ReleaseLeaseRequest) (*pb.ReleaseLeaseResponse, error) {
	if _, err := s.coord.ReleaseLease(ctx, req.GetLeaseId()); err != nil {
		return nil, err
	}
	return &pb.ReleaseLeaseResponse{}, nil
}

func (s *Server) ReportRelayHealth(ctx context.Context, req *pb.ReportRelayHealthRequest) (*pb.ReportRelayHealthResponse, error) {
	if req.GetRelayId() == "" {
		return nil, status.Error(codes.InvalidArgument, "relay_id is required")
	}
	health := s.relayHealthFromProto(req.GetHealth())
	if health == nil {
		return nil, status.Error(codes.InvalidArgument, "health is required")
	}

	if _, _, err := s.coord.ReportRelayHealth(ctx, req.GetRelayId(), req.GetAdvertiseAddr(), *health); err != nil {
		return nil, err
	}
	return &pb.ReportRelayHealthResponse{}, nil
}

func (s *Server) SubscribeRelayHealth(req *pb.SubscribeRelayHealthRequest, stream pb.RegistryService_SubscribeRelayHealthServer) error {
	ctx := stream.Context()
	ch, current := s.coord.SubscribeHealth(ctx, req.GetRelayId())

	if req.GetIncludeCurrent() && current != nil {
		if err := stream.Send(&pb.SubscribeRelayHealthResponse{
			RelayId: req.GetRelayId(),
			Health:  s.relayHealthToProto(current),
		}); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case health, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&pb.SubscribeRelayHealthResponse{
				RelayId: req.GetRelayId(),
				Health:  s.relayHealthToProto(&health),
			}); err != nil {
				return err
			}
		}
	}
}

// ─── Cleanup loop ──────────────────────────────────────────────────────────

func (s *Server) cleanupLoop(ctx context.Context, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			released := s.coord.ReleaseExpiredLeases()
			expired, err := s.store.CleanupExpired(ctx, ttl)
			if err != nil {
				log.Error().Err(err).Msg("cleanup error")
			}
			if expired > 0 {
				log.Info().Int("expired", expired).Msg("cleaned up expired sessions")
			}
			if len(released) > 0 {
				log.Info().Int("expired_leases", len(released)).Msg("released expired leases")
			}
		}
	}
}

// ─── Snapshot helpers ──────────────────────────────────────────────────────

func (s *Server) sendSnapshot(stream pb.RegistryService_WatchServer, tunnelFilter, relayFilter string) error {
	sessions, err := s.store.List(stream.Context())
	if err != nil {
		return nil // non-fatal; best-effort snapshot
	}
	for _, sess := range sessions {
		if tunnelFilter != "" && sess.TunnelID != tunnelFilter {
			continue
		}
		if lease := s.coord.CurrentLease(sess.TunnelID); lease != nil {
			sess.OwnerRelayID = lease.RelayID
			sess.LeaseID = lease.LeaseID
		}
		if relayFilter != "" && sess.OwnerRelayID != relayFilter {
			continue
		}
		if err := stream.Send(&pb.WatchResponse{
			EventType: pb.WatchEventType_WATCH_EVENT_TYPE_TUNNEL_UPSERTED,
			Tunnel:    s.sessionToProto(sess),
		}); err != nil {
			return err
		}
	}

	for _, event := range s.coord.Snapshot(tunnelFilter, relayFilter) {
		proto := s.domainEventToProto(event)
		if proto == nil {
			continue
		}
		if err := stream.Send(proto); err != nil {
			return err
		}
	}
	return nil
}

// ─── Matching / conversion helpers ────────────────────────────────────────

func matchesDomainEvent(event domain.RegistryEvent, tunnelFilter, relayFilter string) bool {
	if tunnelFilter == "" && relayFilter == "" {
		return true
	}
	switch event.Type {
	case domain.RegistryEventTunnelUpserted, domain.RegistryEventTunnelDeleted:
		if event.Tunnel == nil {
			return false
		}
		if tunnelFilter != "" && event.Tunnel.TunnelID != tunnelFilter {
			return false
		}
		if relayFilter != "" && event.Tunnel.OwnerRelayID != relayFilter {
			return false
		}
	case domain.RegistryEventLeaseAcquired, domain.RegistryEventLeaseReleased:
		if event.Lease == nil {
			return false
		}
		if tunnelFilter != "" && event.Lease.TunnelID != tunnelFilter {
			return false
		}
		if relayFilter != "" && event.Lease.RelayID != relayFilter {
			return false
		}
	case domain.RegistryEventRelayUpserted, domain.RegistryEventRelayHealthUpdated:
		if event.Relay == nil {
			return false
		}
		if relayFilter != "" && event.Relay.RelayID != relayFilter {
			return false
		}
	}
	return true
}

func (s *Server) domainEventToProto(event domain.RegistryEvent) *pb.WatchResponse {
	switch event.Type {
	case domain.RegistryEventTunnelUpserted:
		return &pb.WatchResponse{
			EventType: pb.WatchEventType_WATCH_EVENT_TYPE_TUNNEL_UPSERTED,
			Tunnel:    s.sessionToProto(event.Tunnel),
		}
	case domain.RegistryEventTunnelDeleted:
		tunnelID := ""
		if event.Tunnel != nil {
			tunnelID = event.Tunnel.TunnelID
		}
		return &pb.WatchResponse{
			EventType: pb.WatchEventType_WATCH_EVENT_TYPE_TUNNEL_DELETED,
			Tunnel:    &pb.GetTunnelResponse{TunnelId: tunnelID},
		}
	case domain.RegistryEventLeaseAcquired:
		return &pb.WatchResponse{
			EventType: pb.WatchEventType_WATCH_EVENT_TYPE_LEASE_ACQUIRED,
			Lease:     s.leaseToProto(event.Lease),
		}
	case domain.RegistryEventLeaseReleased:
		return &pb.WatchResponse{
			EventType: pb.WatchEventType_WATCH_EVENT_TYPE_LEASE_RELEASED,
			Lease:     s.leaseToProto(event.Lease),
		}
	case domain.RegistryEventRelayUpserted:
		return &pb.WatchResponse{
			EventType: pb.WatchEventType_WATCH_EVENT_TYPE_RELAY_UPSERTED,
			Relay:     s.relayToProto(event.Relay),
		}
	case domain.RegistryEventRelayHealthUpdated:
		return &pb.WatchResponse{
			EventType: pb.WatchEventType_WATCH_EVENT_TYPE_RELAY_HEALTH_UPDATED,
			Relay:     s.relayToProto(event.Relay),
			Health:    s.relayHealthToProto(event.Health),
		}
	}
	return nil
}

func (s *Server) sessionToProto(sess *domain.Session) *pb.GetTunnelResponse {
	if sess == nil {
		return nil
	}
	return &pb.GetTunnelResponse{
		TunnelId:      sess.TunnelID,
		AgentId:       sess.AgentID,
		PublicAddr:    sess.PublicAddr,
		LocalAddr:     sess.LocalAddr,
		CreatedAt:     sess.CreatedAt.Unix(),
		LastHeartbeat: sess.LastHeartbeat.Unix(),
		OwnerRelayId:  sess.OwnerRelayID,
		LeaseId:       sess.LeaseID,
	}
}

func (s *Server) leaseToProto(lease *domain.Lease) *pb.LeaseInfo {
	if lease == nil {
		return nil
	}
	return &pb.LeaseInfo{
		LeaseId:       lease.LeaseID,
		TunnelId:      lease.TunnelID,
		RelayId:       lease.RelayID,
		ExpiresAtUnix: lease.ExpiresAt.Unix(),
		Version:       lease.Version,
	}
}

func (s *Server) relayToProto(relay *domain.RelayInfo) *pb.RelayInfo {
	if relay == nil {
		return nil
	}
	return &pb.RelayInfo{
		RelayId:       relay.RelayID,
		AdvertiseAddr: relay.AdvertiseAddr,
		State:         relay.State,
		ActiveTunnels: relay.ActiveTunnels,
		ActiveStreams: relay.ActiveStreams,
		LastSeenUnix:  relay.LastSeen.Unix(),
	}
}

func (s *Server) relayHealthToProto(health *domain.RelayHealth) *pb.RelayHealth {
	if health == nil {
		return nil
	}
	return &pb.RelayHealth{
		RttMillis:              health.RTTMillis,
		HeartbeatLatencyMillis: health.HeartbeatLatencyMillis,
		ActiveTunnels:          health.ActiveTunnels,
		ActiveStreams:          health.ActiveStreams,
		BytesPerSecond:         health.BytesPerSecond,
		PacketLossPct:          health.PacketLossPct,
		CpuUtilizationPct:      health.CPUUtilizationPct,
		MemoryUtilizationPct:   health.MemoryUtilizationPct,
		RecordedAtUnix:         health.RecordedAt.Unix(),
	}
}

func (s *Server) relayHealthFromProto(health *pb.RelayHealth) *domain.RelayHealth {
	if health == nil {
		return nil
	}
	recordedAt := time.Unix(health.GetRecordedAtUnix(), 0)
	if health.GetRecordedAtUnix() == 0 {
		recordedAt = time.Now()
	}
	return &domain.RelayHealth{
		RTTMillis:              health.GetRttMillis(),
		HeartbeatLatencyMillis: health.GetHeartbeatLatencyMillis(),
		ActiveTunnels:          health.GetActiveTunnels(),
		ActiveStreams:          health.GetActiveStreams(),
		BytesPerSecond:         health.GetBytesPerSecond(),
		PacketLossPct:          health.GetPacketLossPct(),
		CPUUtilizationPct:      health.GetCpuUtilizationPct(),
		MemoryUtilizationPct:   health.GetMemoryUtilizationPct(),
		RecordedAt:             recordedAt,
	}
}

func ttlFromSeconds(seconds int64) time.Duration {
	if seconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func cloneLease(lease *domain.Lease) *domain.Lease {
	if lease == nil {
		return nil
	}
	cp := *lease
	return &cp
}

func cloneRelayInfo(relay *domain.RelayInfo) *domain.RelayInfo {
	if relay == nil {
		return nil
	}
	cp := *relay
	return &cp
}

func cloneRelayHealth(health *domain.RelayHealth) *domain.RelayHealth {
	if health == nil {
		return nil
	}
	cp := *health
	return &cp
}
