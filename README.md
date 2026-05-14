# TunnelEdge

A production-grade secure tunneling platform that exposes local TCP services over the internet using QUIC transport and a microservices architecture.

Inspired by [ngrok](https://ngrok.com), built to demonstrate infrastructure engineering, distributed systems, and transport-layer networking.

---

## Features

- **QUIC Transport** — Low-latency, multiplexed streams over a single connection with built-in TLS 1.3
- **TCP Forwarding** — Expose any local TCP service (HTTP, databases, custom protocols)
- **Multiplexed Streams** — Multiple independent streams over a single QUIC connection, no head-of-line blocking
- **SNI Domain Routing** — Each tunnel gets a unique subdomain (e.g., `web-agent-1.tunneledge.dev`), routed via TLS SNI on a single port
- **Automatic Reconnection** — Exponential backoff with jitter on connection loss
- **HMAC-prefilt Token Auth** — Per-entry HMAC fingerprint skips bcrypt for non-matching tokens; O(1) pre-filter
- **Protocol Versioning** — `MsgHello` handshake frame carries `ProtocolVersion`; gateway rejects incompatible clients early
- **Web Dashboard** — HTMX + SSE dashboard for managing agents and tunnels
- **Rate Limiting** — Per-IP token-bucket rate limit on auth endpoints
- **Request Tracing** — `X-Request-ID` header on every HTTP response; logged with each request
- **Structured Logging** — JSON logs with tunnel IDs, stream IDs, request IDs, and correlation context
- **Prometheus Metrics** — Active tunnels, streams, bytes forwarded, reconnect counts, error rates
- **Graceful Shutdown** — Proper SIGTERM handling with active stream cleanup
- **Docker Compose** — Full stack deployment with one command

---

## Architecture

```
┌────────────────┐
│   CLI Agent    │
│   Go + QUIC    │
└───────┬────────┘
        │ QUIC (TLS 1.3)
        │
┌───────▼────────┐          Public clients
│   Gateway      │◄──────── via TLS SNI
│   Service      │     *.tunneledge.dev:443
└───────┬────────┘
        │ gRPC
        │
┌───────▼────────┐
│   Registry     │
│   Service      │
└────────────────┘
```

### Service Breakdown

| Service | Role |
|---|---|
| **Agent** | Connects to gateway via QUIC, authenticates, relays traffic to local TCP service |
| **Gateway** | Accepts QUIC connections from agents, accepts public TLS traffic on a single port, routes to correct tunnel via SNI |
| **Registry** | gRPC service managing tunnel session state |
| **Dashboard** | HTTP API + HTMX web UI for managing agents, tunnels, and sessions |

### SNI Domain Routing

All public traffic arrives on a **single TLS port** (default `:443`). The gateway uses **TLS SNI (Server Name Indiation)** to determine the target tunnel:

1. DNS: wildcard `*.tunneledge.dev` → gateway IP
2. Client connects to `web-agent-1.tunneledge.dev:443` via TLS
3. During TLS handshake, gateway reads the SNI hostname
4. SNI → tunnel lookup via `TunnelRouter`
5. Gateway opens QUIC stream to agent → relay begins

This avoids the need for multiple public ports — only **two ports** are needed: `:4433/udp` (QUIC for agents) and `:443/tcp` (public TLS).

### Data Flow

1. Agent dials gateway over QUIC, authenticates with token
2. Gateway registers tunnel, assigns subdomains (e.g., `web-agent-1.tunneledge.dev`, `api-agent-1.tunneledge.dev`)
3. Public client connects via TLS to `web-agent-1.tunneledge.dev:443`
4. Gateway reads SNI during TLS handshake, resolves tunnel
5. Gateway opens a QUIC stream to the agent
6. Agent connects to the local TCP service
7. Bidirectional relay begins — data flows transparently

---

## Why QUIC?

| Benefit | Description |
|---|---|
| **Multiplexing** | Multiple independent streams over one connection |
| **No HOL blocking** | Stream-level flow control, not connection-level |
| **Low latency** | 0-RTT connection establishment possible |
| **TLS 1.3** | Encryption built into the protocol |
| **Connection migration** | Survives network changes (future) |

---

## Multiplexing Strategy

A single QUIC connection between agent and gateway supports **multiple concurrent streams**:

- Each incoming public TCP connection triggers a new QUIC stream
- Streams are independent — closing one doesn't affect others
- The `StreamManager` tracks active streams per tunnel with `sync.RWMutex` protection
- Proper cleanup on stream close, tunnel disconnect, or shutdown

---

## Connection Lifecycle

### Agent Startup
1. Agent dials gateway via QUIC
2. Opens auth stream, sends `MsgHello` with `ProtocolVersion`
3. Gateway validates protocol version; closes connection if mismatched
4. Agent sends `MsgAuthV2` token + tunnel definitions
5. Gateway validates token via `Authenticator` (HMAC pre-filter + bcrypt)
6. Gateway registers tunnel with registry (gRPC)
7. Gateway assigns subdomains, stores in `TunnelRouter`
8. Gateway sends auth response with tunnel ID and public URLs
9. Agent enters stream accept loop

### Incoming Public Request
1. Public client connects to `web-agent-1.tunneledge.dev:443` via TLS
2. Gateway performs TLS handshake, reads SNI hostname
3. `TunnelRouter` resolves hostname → tunnel ID
4. Gateway opens QUIC stream to agent
5. Agent accepts stream, dials local TCP service
6. Bidirectional relay begins (`io.Copy` in errgroup)

### Reconnect Flow
1. QUIC connection lost (network error, heartbeat timeout)
2. Session cleaned up on gateway side
3. Agent retries with exponential backoff (2s → 4s → 8s → ... → 30s max)
4. Jitter added to prevent thundering herd
5. Tunnel re-registered on successful reconnect

---

## Local Development Setup

### Prerequisites

- Go 1.26+
- `make`
- protoc + protoc-gen-go + protoc-gen-go-grpc (for proto regeneration)

### Quick Start

```bash
# Build everything
make build

# Run tests
make test

# Format, vet, test, and build in one command
make all
```

### Makefile Commands

Run `make help` to see all available commands:

| Command | Description |
|---|---|
| `make build` | Build all binaries to `bin/` |
| `make build-agent` | Build agent binary only |
| `make build-gateway` | Build gateway binary only |
| `make build-registry` | Build registry binary only |
| `make run-registry` | Run registry service |
| `make run-gateway` | Run gateway service |
| `make run-agent` | Run agent (`TOKEN` and `LOCAL_ADDR` env vars) |
| `make test` | Run tests + go vet |
| `make test-unit` | Run unit tests only |
| `make test-verbose` | Run tests with verbose output |
| `make vet` | Run go vet |
| `make lint` | Run golangci-lint |
| `make tidy` | Tidy go modules |
| `make fmt` | Format code |
| `make proto` | Regenerate protobuf Go code |
| `make certs` | Generate self-signed TLS certs |
| `make docker-up` | Start full stack with docker compose |
| `make docker-down` | Stop docker compose stack |
| `make docker-build` | Build docker images without starting |
| `make docker-logs` | Tail docker compose logs |
| `make clean` | Remove all generated artifacts |
| `make all` | tidy → fmt → vet → test → build |

### Run Locally (Manual)

Config files are in `config/` — edit them to change ports, tokens, or log levels:

```
config/
├── registry.yaml
├── gateway.yaml
└── agent.yaml
```

Terminal 1 — Registry:
```bash
make run-registry
```

Terminal 2 — Gateway:
```bash
make run-gateway
```

Terminal 3 — Agent:
```bash
make run-agent
```

Or print the instructions:
```bash
make run-local
```

Terminal 4 — Test with a local service (e.g., echo server):
```bash
# Start an echo service
socat TCP-LISTEN:3000,fork EXEC:cat

# Add hosts entry for local testing
echo "127.0.0.1 web-agent-1.tunneledge.dev" | sudo tee -a /etc/hosts

# Connect via TLS SNI
echo "Hello TunnelEdge" | openssl s_client -connect web-agent-1.tunneledge.dev:443 \
  -servername web-agent-1.tunneledge.dev -quiet
```

---

## Docker Setup

```bash
make docker-up
```

To stop:

```bash
make docker-down
```

This starts:
- **Registry** on port 50051
- **Gateway** on UDP port 4433 (QUIC) and TCP port 443 (public TLS with SNI routing)
- **Agent** connecting gateway → local echo service
- **Echo service** (socat) on port 6666

### Environment Variables

All services use the `TE_` prefix:

| Variable | Default | Description |
|---|---|---|
| `TE_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `TE_LOG_FORMAT` | `json` | Log format: json, console |
| `TE_OBSERVABILITY_METRICS_ENABLED` | `true` | Enable Prometheus metrics |
| `TE_OBSERVABILITY_METRICS_ADDR` | `:9090` | Metrics server address |
| `TE_DB_DRIVER` | `memory` | Database driver: postgres, memory |
| `TE_DB_DSN` | — | PostgreSQL DSN |
| `TE_DB_AUTO_MIGRATE` | `true` | Auto-run schema migrations |
| `TE_GATEWAY_QUIC_LISTEN_ADDR` | `:4433` | QUIC listen address (gateway) |
| `TE_GATEWAY_PUBLIC_LISTEN_ADDR` | `:443` | Public TLS listen address (gateway, SNI routing) |
| `TE_GATEWAY_BASE_DOMAIN` | `tunneledge.dev` | Base domain for tunnel subdomains (gateway) |
| `TE_GATEWAY_REGISTRY_ADDR` | `localhost:50051` | Registry gRPC address (gateway) |
| `TE_REGISTRY_GRPC_LISTEN_ADDR` | `:50051` | gRPC listen address (registry) |
| `TE_REGISTRY_SESSION_TTL` | `5m` | Session TTL (registry) |
| `TE_DASHBOARD_HTTP_LISTEN_ADDR` | `:8080` | HTTP listen address (dashboard) |
| `TE_DASHBOARD_JWT_SECRET` | — | JWT signing secret (dashboard) |
| `TE_DASHBOARD_JWT_TTL` | `24h` | JWT expiry duration (dashboard) |
| `TE_DASHBOARD_BASE_URL` | `http://localhost:8080` | Public base URL for email links |
| `TE_DASHBOARD_SMTP_HOST` | `localhost` | SMTP server host (dashboard) |
| `TE_DASHBOARD_SMTP_PORT` | `1025` | SMTP server port |
| `TE_DASHBOARD_SMTP_FROM` | `noreply@tunneledge.dev` | Sender address for verification emails |
| `TE_AGENT_GATEWAY_ADDR` | `localhost:4433` | Gateway QUIC address (agent) |
| `TE_AGENT_RECONNECT_DELAY` | `2s` | Initial reconnect delay (agent) |

---

## Tradeoffs

| Decision | Rationale |
|---|---|
| **QUIC over TCP** | Multiplexing without head-of-line blocking, TLS 1.3 built-in, modern transport |
| **gRPC internally** | Strict contracts, typed communication, extensibility |
| **In-memory registry** | MVP simplicity — no external dependencies, fast iteration |
| **Stateless gateway** | Future horizontal scaling, load balancing, distributed routing |
| **SNI domain routing** | Single port, DNS-native, cloud-friendly, unlimited tunnels, standard TLS mechanism |
| **zerolog over zap** | Better performance, simpler API, zero-allocation JSON logging |

---

## Future Improvements

- UDP forwarding support
- HTTP tunnel mode with request inspection
- OpenTelemetry distributed tracing (endpoint wired, SDK not yet integrated)
- Metrics UI (Grafana dashboards)
- Kubernetes deployment (Helm charts)
- Let's Encrypt / ACME auto-cert for wildcard domains
- Multi-region routing with anycast
- Web Application Firewall (WAF) rules
- pgstore integration tests (testcontainers-go)
- mTLS for agent authentication (cert path wired; client cert validation pending)

---

## Demo Usage

```bash
# 1. Start the stack
make docker-up

# 2. Add hosts entry for local testing
echo "127.0.0.1 web-agent-1.tunneledge.dev" | sudo tee -a /etc/hosts

# 3. Connect to the tunnel via TLS
echo "Hello TunnelEdge" | openssl s_client -connect web-agent-1.tunneledge.dev:443 \
  -servername web-agent-1.tunneledge.dev -quiet
# Output: Hello TunnelEdge

# 4. Check metrics
curl http://localhost:9090/metrics

# 5. Check health
curl http://localhost:9090/health

# 6. Stop the stack
make docker-down
```

---

## Project Structure

```
tunneledge/
├── cmd/
│   ├── agent/          # Agent CLI (Cobra)
│   ├── dashboard/      # Dashboard HTTP server
│   ├── gateway/        # Gateway service
│   └── registry/       # Registry service
├── internal/
│   ├── agent/          # Agent core logic, reconnect
│   │   └── agentui/    # BubbleTea TUI for agent
│   ├── auth/           # Token authentication (HMAC pre-filter + bcrypt)
│   ├── dashboard/      # HTTP handlers, service layer, middleware, SSE
│   ├── domain/         # Core types: User, AgentProfile, TunnelConfig, ActiveTunnel
│   ├── gateway/        # Gateway core, SNI router
│   ├── registry/       # gRPC server implementation
│   ├── relay/          # Bidirectional TCP relay
│   ├── store/
│   │   ├── memstore/   # In-memory session/token repositories
│   │   └── pgstore/    # PostgreSQL repositories (GORM)
│   ├── stream/         # Stream lifecycle management
│   ├── transport/      # QUIC wire protocol (Hello frame, AuthV2, relay)
│   └── tui/            # Dashboard TUI (BubbleTea)
├── pkg/
│   ├── config/         # Viper configuration (nested YAML + TE_ env vars)
│   ├── errs/           # Typed errors with codes
│   ├── logger/         # Zerolog structured logging
│   └── metrics/        # Prometheus metrics
├── proto/
│   └── registry/v1/    # Protobuf definitions
├── config/             # Per-service YAML config files
├── deployments/
│   └── docker/         # Dockerfiles & compose
├── scripts/            # Build & dev scripts
├── Makefile            # Build automation
└── instruction.md      # Engineering specification
```

## License

MIT
