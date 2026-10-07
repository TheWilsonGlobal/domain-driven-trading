.PHONY: help build test test-race benchmark docker-up docker-down run-gateway run-engine run-settlement k6-load

help:
	@echo "Vortex Trading Engine commands:"
	@echo "  make build          - Build all Go binaries (gateway, engine, settlement)"
	@echo "  make test           - Run domain & application unit tests"
	@echo "  make test-race      - Run tests with race condition detector"
	@echo "  make benchmark      - Run matching engine throughput benchmarks"
	@echo "  make docker-up      - Launch Redpanda/Kafka, PostgreSQL 16, and Redis"
	@echo "  make docker-down    - Tear down Docker containers and volumes"
	@echo "  make run-engine     - Start matching engine service"
	@echo "  make run-gateway    - Start REST/WebSocket API gateway"
	@echo "  make run-settlement - Start PostgreSQL async settlement worker"
	@echo "  make k6-load        - Run k6 high-throughput load test (>5,000 req/sec)"

build:
	@mkdir -p bin
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/engine ./cmd/engine
	go build -o bin/settlement ./cmd/settlement

test:
	go test -v ./internal/domain/... ./internal/app/...

test-race:
	go test -race -v ./internal/domain/... ./internal/app/...

benchmark:
	go test -bench=. -benchmem -benchtime=5s ./internal/domain/order/...

docker-up:
	docker compose -f deployments/docker-compose.yml up -d

docker-down:
	docker compose -f deployments/docker-compose.yml down -v

run-engine:
	go run ./cmd/engine/main.go

run-gateway:
	go run ./cmd/gateway/main.go

run-settlement:
	go run ./cmd/settlement/main.go

k6-load:
	k6 run scripts/k6_benchmark.js
