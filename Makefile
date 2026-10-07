.PHONY: help build test test-race benchmark docker-up docker-up-services docker-down docker-down-services run-gateway run-engine run-settlement k6-load seed-db

# Check if Go compiler is installed on the host; if not, seamlessly fall back to containerized Go
HAS_GO := $(shell go version 2>nul)

ifeq ($(HAS_GO),)
GO = docker run --rm -v "$(CURDIR):/app" -w /app golang:1.22-alpine go
GO_RACE = docker run --rm -v "$(CURDIR):/app" -w /app golang:1.22 go
else
GO = go
GO_RACE = go
endif

help:
	@echo "Domain-Driven Trading Engine commands:"
	@echo "  make build               - Build all Go binaries (gateway, engine, settlement)"
	@echo "  make test                - Run domain & application unit tests"
	@echo "  make test-race           - Run tests with race condition detector"
	@echo "  make benchmark           - Run matching engine throughput benchmarks"
	@echo "  make docker-up           - Launch standalone stack (Redpanda, PostgreSQL 16, Redis, Services)"
	@echo "                             Use 'make docker-up SERVICES_ONLY=true' to start Services only (using external infra)"
	@echo "  make docker-up-services  - Launch only Trading Services (Engine, Gateway, Settlement) using external infra"
	@echo "  make docker-down         - Tear down standalone Docker containers and volumes"
	@echo "  make docker-down-services- Tear down only Trading Services containers"
	@echo "  make seed-db             - Seed initial accounts and positions into PostgreSQL"
	@echo "  make run-engine          - Start matching engine service locally"
	@echo "  make run-gateway         - Start REST/WebSocket API gateway locally"
	@echo "  make run-settlement      - Start PostgreSQL async settlement worker locally"
	@echo "  make k6-load             - Run k6 high-throughput load test (>5,000 req/sec)"

build:
	@mkdir -p bin
	$(GO) build -o bin/gateway ./cmd/gateway
	$(GO) build -o bin/engine ./cmd/engine
	$(GO) build -o bin/settlement ./cmd/settlement

test:
	$(GO) test -v ./internal/domain/... ./internal/app/...

test-race:
	$(GO_RACE) test -race -v ./internal/domain/... ./internal/app/...

benchmark:
	$(GO) test -bench=. -benchmem -benchtime=5s ./internal/domain/order/...

docker-up:
ifeq ($(SERVICES_ONLY),true)
	docker compose -f deployments/docker-compose.services.yml up -d
else
	docker compose -f deployments/docker-compose.yml up -d
endif

docker-up-services:
	docker compose -f deployments/docker-compose.services.yml up -d

docker-down:
	docker compose -f deployments/docker-compose.yml down -v

docker-down-services:
	docker compose -f deployments/docker-compose.services.yml down

seed-db:
	@echo "Seeding accounts and positions into PostgreSQL..."
	docker exec -i infra-postgres psql -U postgres -d domain_driven_trading < internal/infrastructure/persistence/migrations/001_init.sql || \
	docker exec -i postgres psql -U postgres -d domain_driven_trading < internal/infrastructure/persistence/migrations/001_init.sql

run-engine:
	$(GO) run ./cmd/engine/main.go

run-gateway:
	$(GO) run ./cmd/gateway/main.go

run-settlement:
	$(GO) run ./cmd/settlement/main.go

k6-load:
	k6 run scripts/k6_benchmark.js
