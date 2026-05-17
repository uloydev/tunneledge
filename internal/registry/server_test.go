package registry_test

import (
	"context"
	"testing"
	"time"

	"tunneledge/internal/registry"
	"tunneledge/internal/store/memstore"
	pb "tunneledge/proto/registry/v1"
)

func newServer() *registry.Server {
	store := memstore.NewMemorySessionRepository()
	srv := registry.NewServer(store, nil, 10*time.Minute, 30*time.Minute)
	return srv
}

func TestServer_RegisterTunnel(t *testing.T) {
	srv := newServer()
	defer srv.Stop()

	req := &pb.RegisterTunnelRequest{
		TunnelId:   "t-agent-1",
		AgentId:    "agent-1",
		LocalAddr:  "localhost:8080",
		PublicAddr: "agent-1.tunneledge.dev:443",
	}

	resp, err := srv.RegisterTunnel(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterTunnel failed: %v", err)
	}
	if resp.TunnelId != req.TunnelId {
		t.Errorf("expected tunnel_id %q, got %q", req.TunnelId, resp.TunnelId)
	}
}

func TestServer_RegisterTunnel_Duplicate(t *testing.T) {
	srv := newServer()
	defer srv.Stop()

	req := &pb.RegisterTunnelRequest{
		TunnelId:   "t-dup",
		AgentId:    "agent-dup",
		LocalAddr:  "localhost:9090",
		PublicAddr: "dup.tunneledge.dev:443",
	}

	if _, err := srv.RegisterTunnel(context.Background(), req); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	if _, err := srv.RegisterTunnel(context.Background(), req); err == nil {
		t.Fatal("expected error on duplicate register, got nil")
	}
}

func TestServer_GetTunnel(t *testing.T) {
	srv := newServer()
	defer srv.Stop()

	req := &pb.RegisterTunnelRequest{
		TunnelId:   "t-get-test",
		AgentId:    "agent-get",
		LocalAddr:  "127.0.0.1:3000",
		PublicAddr: "get-test.tunneledge.dev:443",
	}
	if _, err := srv.RegisterTunnel(context.Background(), req); err != nil {
		t.Fatalf("setup register failed: %v", err)
	}

	get, err := srv.GetTunnel(context.Background(), &pb.GetTunnelRequest{TunnelId: "t-get-test"})
	if err != nil {
		t.Fatalf("GetTunnel failed: %v", err)
	}
	if get.AgentId != "agent-get" {
		t.Errorf("expected agent_id %q, got %q", "agent-get", get.AgentId)
	}
}

func TestServer_GetTunnel_NotFound(t *testing.T) {
	srv := newServer()
	defer srv.Stop()

	_, err := srv.GetTunnel(context.Background(), &pb.GetTunnelRequest{TunnelId: "no-such-tunnel"})
	if err == nil {
		t.Fatal("expected error for missing tunnel, got nil")
	}
}

func TestServer_DeregisterTunnel(t *testing.T) {
	srv := newServer()
	defer srv.Stop()

	req := &pb.RegisterTunnelRequest{
		TunnelId:   "t-dereg",
		AgentId:    "agent-dereg",
		LocalAddr:  "localhost:7070",
		PublicAddr: "dereg.tunneledge.dev:443",
	}
	if _, err := srv.RegisterTunnel(context.Background(), req); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if _, err := srv.DeregisterTunnel(context.Background(), &pb.DeregisterTunnelRequest{TunnelId: "t-dereg"}); err != nil {
		t.Fatalf("DeregisterTunnel failed: %v", err)
	}

	// Should now be gone.
	if _, err := srv.GetTunnel(context.Background(), &pb.GetTunnelRequest{TunnelId: "t-dereg"}); err == nil {
		t.Fatal("expected NotFound after deregister, got nil")
	}
}
