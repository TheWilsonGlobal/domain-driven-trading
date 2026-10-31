.PHONY: help vet test-race-docker govulncheck dev dev-services dev-down build test test-race benchmark docker-up docker-up-services docker-down docker-down-services run-gateway run-engine run-settlement run-web k6-load seed-db migrate-supabase

# Go is required on the host (version pinned by the go directive in go.mod).
# The old bind-mount docker fallback was dropped: bind mounts do not work on
# this Docker daemon. Race tests need a C compiler; without one use
# `make test-race-docker` (docker create/cp/start/logs, no bind mount).
GO = go
# Keep in step with deployments/Dockerfile and go.mod.
GO_IMAGE = golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb
# Compose reads only the root .env (deployments/.env is not used).
COMPOSE = docker compose --env-file .env

help:
	@echo "Domain-Driven Trading Engine commands:"
	@echo "  make dev                 - Start all backend (Docker Compose) and launch web frontend"
	@echo "  make dev-services        - Start backend services only (external infra) and launch web frontend"
	@echo "  make dev-down            - Stop all running Docker Compose backend containers"
	@echo "  make build               - Build all Go binaries (gateway, engine, settlement)"
	@echo "  make test                - Run domain & application unit tests"
	@echo "  make test-race           - Run all tests with the race detector (needs gcc)"
	@echo "  make test-race-docker    - Same, inside a golang container (docker cp, no bind mount)"
	@echo "  make benchmark           - Run matching engine throughput benchmarks"
	@echo "  make docker-up           - Launch standalone stack (Redpanda, PostgreSQL 16, Redis, Services)"
	@echo "                             Use 'make docker-up SERVICES_ONLY=true' to start Services only (using external infra)"
	@echo "  make docker-up-services  - Launch only Trading Services (Engine, Gateway, Settlement) using external infra"
	@echo "  make docker-down         - Tear down standalone Docker containers and volumes"
	@echo "  make docker-down-services- Tear down only Trading Services containers"
	@echo "  make seed-db             - Seed initial accounts and positions into PostgreSQL"
	@echo "  make migrate-supabase    - Run Supabase migration and account verification"
	@echo "  make run-engine          - Start matching engine service locally"
	@echo "  make run-gateway         - Start REST/WebSocket API gateway locally"
	@echo "  make run-settlement      - Start PostgreSQL async settlement worker locally"
	@echo "  make run-web             - Start web frontend dev server (Vite on port 5471)"
	@echo "  make k6-load             - Run k6 high-throughput load test (>5,000 req/sec)"

dev:
	@echo "Starting backend stack with Docker Compose..."
	$(COMPOSE) -f deployments/docker-compose.yml up -d
	@echo "Starting web trading terminal at http://localhost:5471..."
	cd web && pnpm dev

dev-services:
	@echo "Starting backend services only..."
	$(COMPOSE) -f deployments/docker-compose.services.yml up -d
	@echo "Starting web trading terminal at http://localhost:5471..."
	cd web && pnpm dev

dev-down:
	$(COMPOSE) -f deployments/docker-compose.yml down

build:
	@mkdir -p bin
	$(GO) build -o bin/gateway ./cmd/gateway
	$(GO) build -o bin/engine ./cmd/engine
	$(GO) build -o bin/settlement ./cmd/settlement

vet:
	$(GO) vet ./...

test:
	$(GO) test -count=1 ./...

# Needs gcc on the host (CGO). Without gcc use test-race-docker.
test-race:
	CGO_ENABLED=1 $(GO) test -race -count=1 ./...

# Race tests inside a container: copy the tree in with docker cp (no bind mount).
test-race-docker:
	@cid=$$(docker create -w /w $(GO_IMAGE) sh -c "apk add --no-cache gcc musl-dev >/dev/null && CGO_ENABLED=1 go test -race -count=1 ./...") ; \
	for f in go.mod go.sum cmd internal scripts; do docker cp $$f $$cid:/w/$$f || exit 1; done ; \
	docker start $$cid >/dev/null ; \
	rc=$$(docker wait $$cid) ; \
	docker logs $$cid 2>&1 | tail -60 ; \
	docker rm $$cid >/dev/null ; \
	exit $$rc

govulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

benchmark:
	$(GO) test -bench=. -benchmem -benchtime=5s ./internal/domain/order/...

docker-up:
ifeq ($(SERVICES_ONLY),true)
	$(COMPOSE) -f deployments/docker-compose.services.yml up -d
else
	$(COMPOSE) -f deployments/docker-compose.yml up -d
endif

docker-up-services:
	$(COMPOSE) -f deployments/docker-compose.services.yml up -d

docker-down:
	$(COMPOSE) -f deployments/docker-compose.yml down -v

docker-down-services:
	$(COMPOSE) -f deployments/docker-compose.services.yml down

seed-db:
	@echo "Seeding accounts and positions into PostgreSQL..."
	docker exec -i infra-postgres psql -U postgres -d domain_driven_trading < internal/infrastructure/persistence/migrations/001_init.sql || \
	docker exec -i postgres psql -U postgres -d domain_driven_trading < internal/infrastructure/persistence/migrations/001_init.sql

migrate-supabase:
	@echo "Running Supabase migration and account verification..."
	python scripts/migrate_to_supabase.py

run-engine:
	$(GO) run ./cmd/engine/main.go

run-gateway:
	$(GO) run ./cmd/gateway/main.go

run-settlement:
	$(GO) run ./cmd/settlement/main.go

run-web:
	cd web && pnpm dev

k6-load:
	k6 run scripts/k6_benchmark.js
