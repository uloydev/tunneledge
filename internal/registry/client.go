package registry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

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
