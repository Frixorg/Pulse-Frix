# Pulse developer Makefile. See docs/DEVELOPMENT.md.
.DEFAULT_GOAL := help
SHELL := /bin/bash

AGENT_DIR := agent
API_DIR := apps/api
DASH_DIR := apps/dashboard
BIN := bin

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo 0.1.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%FT%TZ)
LDFLAGS := -s -w \
  -X github.com/frix-me/pulse/agent/internal/version.Version=$(VERSION) \
  -X github.com/frix-me/pulse/agent/internal/version.Commit=$(COMMIT) \
  -X github.com/frix-me/pulse/agent/internal/version.BuildDate=$(DATE)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: agent api dashboard ## Build everything

.PHONY: agent
agent: ## Build the agent + pulse CLI into ./bin
	@mkdir -p $(BIN)
	cd $(AGENT_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ../$(BIN)/pulse-agent ./cmd/pulse-agent
	cd $(AGENT_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ../$(BIN)/pulse ./cmd/pulse

.PHONY: api
api: ## Build the control-plane API into ./bin
	@mkdir -p $(BIN)
	cd $(API_DIR) && CGO_ENABLED=0 go build -trimpath -o ../../$(BIN)/pulse-api ./cmd/pulse-api

.PHONY: dashboard
dashboard: ## Build the dashboard SPA
	cd $(DASH_DIR) && (pnpm install --no-frozen-lockfile || npm install) && npm run build

.PHONY: test
test: ## Run Go unit tests (agent + api)
	cd $(AGENT_DIR) && go test ./...
	cd $(API_DIR) && go test ./...

.PHONY: lint
lint: ## Vet Go modules and typecheck the dashboard
	cd $(AGENT_DIR) && go vet ./...
	cd $(API_DIR) && go vet ./...
	cd $(DASH_DIR) && (pnpm install --no-frozen-lockfile || npm install) && npm run typecheck

.PHONY: test-nondestructive
test-nondestructive: ## Run the non-destructive installer test (requires Docker)
	bash tests/non-destructive/run.sh

.PHONY: dev
dev: ## Start API + dashboard + monitoring locally (Docker)
	docker compose -f monitoring/docker-compose.monitoring.yml up -d
	@echo "Run the API:        cd $(API_DIR) && go run ./cmd/pulse-api"
	@echo "Run the dashboard:  cd $(DASH_DIR) && npm run dev"

.PHONY: docker-build
docker-build: ## Build all container images
	docker build -t pulse-agent:$(VERSION) $(AGENT_DIR)
	docker build -t pulse-api:$(VERSION) $(API_DIR)
	docker build -t pulse-dashboard:$(VERSION) $(DASH_DIR)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN) $(DASH_DIR)/dist
