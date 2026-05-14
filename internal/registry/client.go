package registry

import (
	"context"
	"fmt"
	"time"

	pb "tunneledge/proto/registry/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCRegistryClient struct {
	client pb.RegistryServiceClient
	conn   *grpc.ClientConn
}

type tokenCredentials struct {
	token string
}

func (t tokenCredentials) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + t.token,
	}, nil
}

func (t tokenCredentials) RequireTransportSecurity() bool {
	return false
}

func NewGRPCRegistryClient(addr string, authToken string) (*GRPCRegistryClient, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	if authToken != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(tokenCredentials{token: authToken}))
	}

	conn, err := grpc.NewClient(addr, opts...)
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

func (c *GRPCRegistryClient) Heartbeat(tunnelID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.client.Heartbeat(ctx, &pb.HeartbeatRequest{
		TunnelId: tunnelID,
	})
	return err
}
