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

func NewGRPCRegistryClient(addr string) (*GRPCRegistryClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
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

func (c *GRPCRegistryClient) RegisterTunnel(tunnelID, agentID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.client.RegisterTunnel(ctx, &pb.RegisterTunnelRequest{
		TunnelId: tunnelID,
		AgentId:  agentID,
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
