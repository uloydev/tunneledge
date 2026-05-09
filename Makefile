.PHONY: help build build-agent build-gateway build-registry build-http-echo run run-registry run-gateway run-agent \
       run-local test test-unit test-verbose vet lint tidy fmt proto \
       docker-up docker-down docker-build docker-logs \
       certs clean clean-bin all

BINARY_DIR  := bin
GO          := go
MAIN_FLAGS  := -ldflags="-s -w"
DOCKER_DIR  := deployments/docker
CERTS_DIR   := certs
CONFIG_DIR  := config

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ── Build ────────────────────────────────────────────────────────────────────

build: build-agent build-gateway build-registry build-http-echo ## Build all binaries

build-agent: ## Build agent binary
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(MAIN_FLAGS) -o $(BINARY_DIR)/agent ./cmd/agent

build-gateway: ## Build gateway binary
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(MAIN_FLAGS) -o $(BINARY_DIR)/gateway ./cmd/gateway

build-registry: ## Build registry binary
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(MAIN_FLAGS) -o $(BINARY_DIR)/registry ./cmd/registry

build-http-echo: ## Build http-echo binary
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(MAIN_FLAGS) -o $(BINARY_DIR)/http-echo ./cmd/http-echo

# ── Run (local dev) ──────────────────────────────────────────────────────────

run-registry: ## Run registry service
	$(GO) run ./cmd/registry -c $(CONFIG_DIR)/registry.yaml

run-gateway: ## Run gateway service
	$(GO) run ./cmd/gateway -c $(CONFIG_DIR)/gateway.yaml

run-agent: ## Run agent (set TOKEN and LOCAL_ADDR)
	$(GO) run ./cmd/agent -c $(CONFIG_DIR)/agent.yaml --token $${TOKEN:-dev-token} --local-addr $${LOCAL_ADDR:-localhost:3000}

run-local: ## Run all services locally (requires 3 terminals — prints instructions)
	@echo ""
	@echo "Run each command in a separate terminal:"
	@echo ""
	@echo "  \033[36mTerminal 1 — Registry:\033[0m   make run-registry"
	@echo "  \033[36mTerminal 2 — Gateway:\033[0m    make run-gateway"
	@echo "  \033[36mTerminal 3 — Agent:\033[0m      make run-agent"
	@echo ""
	@echo "Then test with:"
	@echo "  echo 'Hello' | openssl s_client -connect agent-1.tunneledge.dev:443 -servername agent-1.tunneledge.dev -quiet"
	@echo ""

# ── Test & Quality ───────────────────────────────────────────────────────────

test: test-unit vet ## Run tests and vet

test-unit: ## Run unit tests
	$(GO) test -count=1 ./internal/...

test-verbose: ## Run tests with verbose output
	$(GO) test -count=1 -v ./internal/...

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

tidy: ## Tidy go modules
	$(GO) mod tidy

fmt: ## Format code
	golangci-lint fmt ./... 2>/dev/null || gofmt -w -s .

# ── Code Generation ──────────────────────────────────────────────────────────

proto: ## Regenerate protobuf Go code
	@bash scripts/generate-proto.sh

certs: ## Generate self-signed TLS certs
	@bash scripts/generate-certs.sh

# ── Docker ───────────────────────────────────────────────────────────────────

docker-up: ## Start full stack with docker compose
	docker compose -f $(DOCKER_DIR)/docker-compose.yml up --build -d

docker-down: ## Stop docker compose stack
	docker compose -f $(DOCKER_DIR)/docker-compose.yml down

docker-build: ## Build all docker images (no start)
	docker compose -f $(DOCKER_DIR)/docker-compose.yml build

docker-logs: ## Tail docker compose logs
	docker compose -f $(DOCKER_DIR)/docker-compose.yml logs -f

# ── Clean ────────────────────────────────────────────────────────────────────

clean: clean-bin ## Remove all generated artifacts

clean-bin: ## Remove built binaries
	rm -rf $(BINARY_DIR)

# ── All-in-one ───────────────────────────────────────────────────────────────

all: tidy fmt vet test build ## Format, vet, test, and build everything
