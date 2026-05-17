package registry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"time"

	"tunneledge/internal/domain"
	pb "tunneledge/proto/registry/v1"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCRegistryClient struct {
	client pb.RegistryServiceClient
	conn   *grpc.ClientConn
}

type tokenCredentials struct {
	token            string
	requireTransport bool
}

func (t tokenCredentials) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + t.token,
	}, nil
}

func (t tokenCredentials) RequireTransportSecurity() bool {
	return t.requireTransport
}

// ClientOptions configures the gRPC registry client.
type ClientOptions struct {
	// TLSCertFile is the path to a PEM CA certificate used to verify the
	// registry server's TLS certificate. Set to "insecure" (or leave empty)
	// to skip verification — development only.
	TLSCertFile string
	// AuthToken is the bearer token sent with each RPC. Empty = no auth header.
	AuthToken string
}

func NewGRPCRegistryClient(addr string, authToken string) (*GRPCRegistryClient, error) {
	return NewGRPCRegistryClientWithOptions(addr, ClientOptions{
		TLSCertFile: "insecure",
		AuthToken:   authToken,
	})
}

// NewGRPCRegistryClientWithOptions creates a registry client with explicit TLS config.
func NewGRPCRegistryClientWithOptions(addr string, opts ClientOptions) (*GRPCRegistryClient, error) {
	tlsCert := opts.TLSCertFile

	var transportCreds grpc.DialOption
	useTLS := tlsCert != "" && tlsCert != "insecure"

	if useTLS {
		caCert, err := os.ReadFile(tlsCert)
		if err != nil {
			return nil, fmt.Errorf("failed to read registry TLS cert %s: %w", tlsCert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse registry TLS cert %s", tlsCert)
		}
		transportCreds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS13,
		}))
	} else {
		transportCreds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	dialOpts := []grpc.DialOption{transportCreds, grpc.WithStatsHandler(otelgrpc.NewClientHandler())}

	if opts.AuthToken != "" {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(tokenCredentials{
			token:            opts.AuthToken,
			requireTransport: useTLS,
		}))
	}

	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to registry: %w", err)
	}

	return &GRPCRegistryClient{
		client: pb.NewRegistryServiceClient(conn),
		conn:   conn,
	}, nil
}

func (c *GRPCRegistryClient) Close() error {
	return c.conn.Close()
}

func (c *GRPCRegistryClient) RegisterTunnel(tunnelID, agentID, publicAddr, localAddr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.client.RegisterTunnel(ctx, &pb.RegisterTunnelRequest{
		TunnelId:   tunnelID,
		AgentId:    agentID,
		PublicAddr: publicAddr,
		LocalAddr:  localAddr,
	})
	if err != nil {
		return fmt.Errorf("failed to register tunnel %s: %w", tunnelID, err)
	}
	return nil
}

func (c *GRPCRegistryClient) DeregisterTunnel(tunnelID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.client.DeregisterTunnel(ctx, &pb.DeregisterTunnelRequest{
		TunnelId: tunnelID,
	})
	return err
}

func (c *GRPCRegistryClient) Heartbeat(tunnelID string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.client.Heartbeat(ctx, &pb.HeartbeatRequest{
		TunnelId: tunnelID,
	})
	if err != nil {
		return false, err
	}
	return resp.GetAlive(), nil
}

func (c *GRPCRegistryClient) Watch(ctx context.Context, opts domain.WatchOptions) (<-chan domain.RegistryEvent, error) {
	stream, err := c.client.Watch(ctx, &pb.WatchRequest{
		TunnelId:        opts.TunnelID,
		RelayId:         opts.RelayID,
		IncludeExisting: opts.IncludeExisting,
	})
	if err != nil {
		return nil, err
	}

	events := make(chan domain.RegistryEvent)
	go func() {
		defer close(events)
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF || ctx.Err() != nil {
					return
				}
				return
			}

			select {
			case <-ctx.Done():
				return
			case events <- registryEventFromProto(resp):
			}
		}
	}()

	return events, nil
}

func (c *GRPCRegistryClient) AcquireLease(ctx context.Context, req domain.LeaseRequest) (*domain.Lease, error) {
	resp, err := c.client.AcquireLease(ctx, &pb.AcquireLeaseRequest{
		TunnelId:   req.TunnelID,
		RelayId:    req.RelayID,
		TtlSeconds: int64(req.TTL / time.Second),
	})
	if err != nil {
		return nil, err
	}
	return leaseFromProto(resp.GetLease()), nil
}

func (c *GRPCRegistryClient) RenewLease(ctx context.Context, leaseID string, ttl time.Duration) (*domain.Lease, error) {
	resp, err := c.client.RenewLease(ctx, &pb.RenewLeaseRequest{
		LeaseId:    leaseID,
		TtlSeconds: int64(ttl / time.Second),
	})
	if err != nil {
		return nil, err
	}
	return leaseFromProto(resp.GetLease()), nil
}

func (c *GRPCRegistryClient) ReleaseLease(ctx context.Context, leaseID string) error {
	_, err := c.client.ReleaseLease(ctx, &pb.ReleaseLeaseRequest{LeaseId: leaseID})
	return err
}

func (c *GRPCRegistryClient) ReportRelayHealth(ctx context.Context, relayID string, health domain.RelayHealth) error {
	_, err := c.client.ReportRelayHealth(ctx, &pb.ReportRelayHealthRequest{
		RelayId:       relayID,
		Health:        relayHealthToProto(health),
		AdvertiseAddr: health.AdvertiseAddr,
	})
	return err
}

func (c *GRPCRegistryClient) SubscribeRelayHealth(ctx context.Context, relayID string, includeCurrent bool) (<-chan domain.RelayHealth, error) {
	stream, err := c.client.SubscribeRelayHealth(ctx, &pb.SubscribeRelayHealthRequest{
		RelayId:        relayID,
		IncludeCurrent: includeCurrent,
	})
	if err != nil {
		return nil, err
	}

	updates := make(chan domain.RelayHealth)
	go func() {
		defer close(updates)
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF || ctx.Err() != nil {
					return
				}
				return
			}

			health := relayHealthFromProto(resp.GetHealth())
			if health == nil {
				continue
			}

			select {
			case <-ctx.Done():
				return
			case updates <- *health:
			}
		}
	}()

	return updates, nil
}

func registryEventFromProto(resp *pb.WatchResponse) domain.RegistryEvent {
	return domain.RegistryEvent{
		Type:   registryEventTypeFromProto(resp.GetEventType()),
		Tunnel: sessionFromProto(resp.GetTunnel()),
		Lease:  leaseFromProto(resp.GetLease()),
		Relay:  relayInfoFromProto(resp.GetRelay()),
		Health: relayHealthFromProto(resp.GetHealth()),
	}
}

func registryEventTypeFromProto(eventType pb.WatchEventType) domain.RegistryEventType {
	switch eventType {
	case pb.WatchEventType_WATCH_EVENT_TYPE_TUNNEL_UPSERTED:
		return domain.RegistryEventTunnelUpserted
	case pb.WatchEventType_WATCH_EVENT_TYPE_TUNNEL_DELETED:
		return domain.RegistryEventTunnelDeleted
	case pb.WatchEventType_WATCH_EVENT_TYPE_LEASE_ACQUIRED:
		return domain.RegistryEventLeaseAcquired
	case pb.WatchEventType_WATCH_EVENT_TYPE_LEASE_RELEASED:
		return domain.RegistryEventLeaseReleased
	case pb.WatchEventType_WATCH_EVENT_TYPE_RELAY_UPSERTED:
		return domain.RegistryEventRelayUpserted
	case pb.WatchEventType_WATCH_EVENT_TYPE_RELAY_HEALTH_UPDATED:
		return domain.RegistryEventRelayHealthUpdated
	default:
		return domain.RegistryEventUnknown
	}
}

func sessionFromProto(resp *pb.GetTunnelResponse) *domain.Session {
	if resp == nil {
		return nil
	}

	return &domain.Session{
		TunnelID:      resp.GetTunnelId(),
		AgentID:       resp.GetAgentId(),
		OwnerRelayID:  resp.GetOwnerRelayId(),
		LeaseID:       resp.GetLeaseId(),
		PublicAddr:    resp.GetPublicAddr(),
		LocalAddr:     resp.GetLocalAddr(),
		CreatedAt:     time.Unix(resp.GetCreatedAt(), 0),
		LastHeartbeat: time.Unix(resp.GetLastHeartbeat(), 0),
	}
}

func leaseFromProto(lease *pb.LeaseInfo) *domain.Lease {
	if lease == nil {
		return nil
	}

	return &domain.Lease{
		LeaseID:   lease.GetLeaseId(),
		TunnelID:  lease.GetTunnelId(),
		RelayID:   lease.GetRelayId(),
		ExpiresAt: time.Unix(lease.GetExpiresAtUnix(), 0),
		Version:   lease.GetVersion(),
	}
}

func relayInfoFromProto(relay *pb.RelayInfo) *domain.RelayInfo {
	if relay == nil {
		return nil
	}

	return &domain.RelayInfo{
		RelayID:       relay.GetRelayId(),
		AdvertiseAddr: relay.GetAdvertiseAddr(),
		State:         relay.GetState(),
		ActiveTunnels: relay.GetActiveTunnels(),
		ActiveStreams: relay.GetActiveStreams(),
		LastSeen:      time.Unix(relay.GetLastSeenUnix(), 0),
	}
}

func relayHealthToProto(health domain.RelayHealth) *pb.RelayHealth {
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

func relayHealthFromProto(health *pb.RelayHealth) *domain.RelayHealth {
	if health == nil {
		return nil
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
		RecordedAt:             time.Unix(health.GetRecordedAtUnix(), 0),
	}
}
