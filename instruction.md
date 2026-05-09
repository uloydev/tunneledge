# TunnelEdge — Production-Ready Engineering Specification

## Overview

TunnelEdge is a production-grade secure tunneling platform inspired by ngrok, designed to expose local TCP services securely over the internet using QUIC transport and a microservices architecture.

The project is intentionally engineered to showcase:

- Distributed systems understanding
- Networking fundamentals
- Concurrent programming
- Transport-layer engineering
- Production-level observability
- Scalable infrastructure design
- Resiliency and fault tolerance

The codebase must be written as if it were intended for a real infrastructure/platform engineering team.

> This project is NOT a tutorial project.
> This project is NOT a CRUD application.
> This project MUST follow production-grade software engineering principles.

---

## Core Product Goals

### Primary Goals

- Securely expose local TCP services to the public internet
- Use QUIC as primary transport layer
- Support multiplexed streams over a single connection
- Maintain low-latency bidirectional communication
- Demonstrate scalable microservice architecture
- Implement resilient reconnect and session lifecycle handling
- Showcase clean and maintainable Go architecture

### Non-Goals (MVP)

The MVP intentionally excludes:

- UDP support
- HTTP inspection
- Web dashboard
- Billing
- Kubernetes deployment automation
- Multi-region routing
- Distributed session persistence
- AI features
- TLS automation
- Multi-tenant RBAC

These may exist as future roadmap items but MUST NOT complicate the MVP.

---

## Technology Stack

| Category             | Technology                          |
|----------------------|-------------------------------------|
| Language             | Golang (latest stable version)      |
| Networking           | QUIC via quic-go, TCP socket forwarding |
| Internal Communication | gRPC, Protocol Buffers           |
| Observability        | OpenTelemetry, zap structured logging, Prometheus-ready metrics |
| Infrastructure       | Docker, Docker Compose             |
| CLI                  | Cobra CLI                           |
| Configuration        | Environment variables, optional YAML config |

---

## Engineering Philosophy

All code MUST follow these engineering standards.

### 1. Production-Ready First

Every implementation decision must optimize for:

- Reliability
- Readability
- Debuggability
- Observability
- Maintainability

Never implement "quick hacks" that sacrifice architecture quality.

### 2. Concurrency Safety

All concurrent access MUST be thread-safe.

**Requirements:**

- Avoid race conditions
- Avoid deadlocks
- Avoid goroutine leaks
- Proper context propagation
- Graceful cancellation

**Use:**

- `sync.RWMutex`
- `context.Context`
- `errgroup`
- Channel patterns only when appropriate

> Do NOT overuse channels where mutexes are more appropriate.

### 3. Graceful Shutdown

All services MUST support graceful shutdown.

**Requirements:**

- SIGTERM handling
- Active stream cleanup
- Context cancellation propagation
- Listener closure
- Goroutine termination

No hanging goroutines should remain after shutdown.

### 4. Structured Logging

All logs MUST be structured.

**Requirements:**

- Correlation IDs
- Session IDs
- Tunnel IDs
- Stream IDs
- Error classification

Avoid unstructured `fmt.Println` logging.

**Example:**

```json
{
  "service": "gateway",
  "tunnel_id": "abc123",
  "stream_id": "stream-001",
  "event": "stream_opened"
}
```

### 5. Observability First

Every major component MUST expose observability hooks.

**Requirements:**

- Connection lifecycle visibility
- Stream lifecycle visibility
- Reconnect metrics
- Active tunnel count
- Bytes transferred

Design observability as part of architecture, not as an afterthought.

### 6. Error Handling Standards

All errors MUST:

- Be wrapped
- Include context
- Preserve root cause

**Use:**

```go
fmt.Errorf("failed to open stream: %w", err)
```

- Never swallow errors.
- Never panic in recoverable paths.

### 7. Configuration Management

All runtime configuration MUST be externalized.

**Do NOT hardcode:**

- Ports
- Tokens
- Timeouts
- Retry values

**Use:**

- Environment variables
- Config structs
- Sane defaults

---

## High-Level Architecture

```
+----------------+
| CLI Agent      |
| Go + QUIC      |
+-------+--------+
        |
        | QUIC
        |
+-------v--------+
| Gateway Service|
+-------+--------+
        |
        | gRPC
        |
+-------v--------+
| Registry Svc   |
+----------------+
```

---

## Service Breakdown

### 1. CLI Agent

**Responsibilities:**

- Establish QUIC connection to gateway
- Authenticate using token
- Maintain heartbeat
- Open multiplexed streams
- Connect to local TCP services
- Relay traffic bidirectionally
- Reconnect automatically

**Requirements:**

- Reconnect support
- Exponential backoff
- Graceful reconnect
- Concurrent stream support
- Stream cleanup

> The agent must maintain a **SINGLE QUIC connection** while supporting **MULTIPLE multiplexed streams**. This is one of the key engineering showcase features.

### 2. Gateway Service

**Responsibilities:**

- Accept incoming QUIC connections
- Authenticate agents
- Manage active sessions
- Accept public TCP traffic
- Route traffic to correct tunnel
- Relay traffic efficiently

**Requirements:**

- Stateless architecture
- Concurrent stream handling
- Low memory overhead
- Proper backpressure handling
- Graceful shutdown

> Gateway should be architected to support future horizontal scaling. Avoid tightly coupling session state to gateway internals.

### 3. Registry Service

**Responsibilities:**

- Register tunnel sessions
- Store active tunnel mappings
- Manage tunnel metadata
- Handle tunnel lifecycle

**MVP Storage:**

- **For MVP:** in-memory session store
- **Future roadmap may use:** Redis, PostgreSQL, distributed coordination

---

## Connection Lifecycle

### Agent Startup

1. Agent starts
2. Agent authenticates
3. QUIC connection established
4. Tunnel registered
5. Public endpoint assigned
6. Agent waits for incoming stream

### Incoming Public Request

1. Public client connects
2. Gateway identifies tunnel
3. QUIC stream opened
4. Agent accepts stream
5. Local TCP connection opened
6. Bidirectional relay begins

### Reconnect Flow

1. Connection lost
2. Heartbeat timeout triggered
3. Session cleaned
4. Agent retries connection
5. Tunnel restored

---

## Multiplexing Requirements

QUIC multiplexing is a **CORE** architectural requirement.

**Requirements:**

- Single QUIC connection
- Multiple independent streams
- Concurrent stream processing
- Stream isolation
- Proper cleanup

> The implementation MUST avoid head-of-line blocking.

---

## Performance Requirements

### Target Metrics

| Metric              | Target                            |
|---------------------|-----------------------------------|
| Tunnel Setup        | < 3 seconds                       |
| Concurrent Streams  | Minimum 100 concurrent streams locally |

### Resource Usage

- Avoid unnecessary allocations
- Avoid memory leaks
- Minimize copies

### Stability

- No goroutine leaks
- No stream leak
- Stable reconnect behavior

---

## Security Requirements

### Authentication

- **MVP:** Token-based authentication
- **Future:** JWT, mTLS, OAuth

### Transport Security

- QUIC already provides TLS 1.3 encryption

### Security Constraints

**Never:**

- Expose unauthenticated tunnel
- Trust client input
- Ignore malformed stream data

---

## Codebase Structure

```
/tunneledge
├── /cmd
│   ├── /gateway
│   ├── /registry
│   └── /agent
├── /internal
│   ├── /gateway
│   ├── /registry
│   ├── /agent
│   ├── /relay
│   ├── /transport
│   ├── /auth
│   ├── /session
│   └── /stream
├── /pkg
│   ├── /logger
│   ├── /config
│   ├── /metrics
│   └── /errors
├── /proto
├── /deployments
│   └── /docker
└── /scripts
```

---

## Go Engineering Standards

### Mandatory Standards

| Standard                | Description                                                    |
|-------------------------|----------------------------------------------------------------|
| Always Use Context      | All long-running operations MUST accept `context.Context`.     |
| Avoid Global State      | Dependency injection preferred.                                |
| Interfaces at Boundaries| Use interfaces ONLY where abstraction is needed. Do not create unnecessary interfaces. |
| Explicit Ownership      | Each service/component must clearly own lifecycle, cleanup, and concurrency management. |
| Avoid God Objects       | Keep components focused.                                       |

---

## Testing Standards

### Required Tests

| Test Type          | Scope                                                |
|--------------------|------------------------------------------------------|
| Unit Tests         | Session management, stream lifecycle, reconnect strategy, registry behavior |
| Concurrency Tests  | Race conditions, concurrent stream safety             |
| Integration Tests  | End-to-end tunnel validation                         |

---

## Important Tradeoffs

These MUST be documented.

| Decision              | Rationale                                                         |
|-----------------------|-------------------------------------------------------------------|
| **Why QUIC?**         | Multiplexing, low latency, modern transport, connection migration, no head-of-line blocking |
| **Why gRPC Internally?** | Strict contracts, typed communication, extensibility, maintainability |
| **Why In-Memory Registry?** | MVP prioritizes simplicity, fast iteration, reduced operational complexity |
| **Why Stateless Gateway?** | Future scaling should support horizontal scaling, load balancing, distributed routing |

---

## Development Rules for GitHub Copilot

The following rules MUST be followed when generating code.

### Architecture Rules

- Prefer composition over inheritance
- Keep packages small and focused
- Avoid cyclic dependencies
- Follow clean architecture principles
- Separate transport from business logic

### Concurrency Rules

- Every goroutine must have clear termination logic
- Never start anonymous background goroutines without lifecycle ownership
- Always close resources explicitly
- Context cancellation must propagate properly

### Networking Rules

- Handle partial reads/writes correctly
- Never assume full buffer writes
- Handle connection closure gracefully
- Validate stream state before operations

### Logging Rules

- All logs structured
- Never log sensitive token values
- Include contextual identifiers

### Error Rules

- Wrap all external-facing errors
- Preserve root causes
- Never ignore returned errors

### Testing Rules

- New components should include tests
- Concurrency-sensitive logic must include race-safe testing
- Critical relay logic must have integration tests

---

## MVP Deliverables

### Required Deliverables

- [ ] Working QUIC tunnel
- [ ] TCP forwarding
- [ ] Multiplexed stream support
- [ ] Reconnect support
- [ ] Structured logging
- [ ] Dockerized services
- [ ] Architecture diagram
- [ ] Clean README
- [ ] Production-grade code structure

### README Expectations

README must include the following sections:

1. Overview
2. Features
3. Architecture
4. Why QUIC
5. Multiplexing strategy
6. Connection lifecycle
7. Local development setup
8. Docker setup
9. Tradeoffs
10. Future improvements
11. Demo usage

---

## Future Roadmap

Potential future features:

- UDP support
- HTTP tunnels
- Dashboard
- Metrics UI
- Distributed registry
- Kubernetes integration
- mTLS
- Multi-region routing
- Rate limiting
- WAF
- AI-assisted observability

---

## Final Goal

The final project should feel like:

- A real infrastructure engineering system
- A platform engineering showcase
- A distributed systems portfolio project
- A production-minded networking platform

> This project should demonstrate engineering maturity beyond typical backend CRUD applications.
