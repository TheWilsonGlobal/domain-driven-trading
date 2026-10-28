# Domain-Driven Trading Engine (`domain-driven-trading`)

A production-grade, low-latency, event-driven Proof of Concept (PoC) for an institutional securities trading core and risk engine in **Go (Golang)** with an interactive **React & TypeScript** web terminal. Built with Domain-Driven Design (DDD), Hexagonal Architecture, In-Memory FIFO Price-Time Priority order matching, Double-Entry ledger bookkeeping, pre-trade purchasing power ("sức mua") validation, and Kafka/Redpanda Event-Driven Architecture via Watermill.

---

## 1. Architectural Highlights

- **Pure Domain Isolation:** Zero infrastructure dependencies in `internal/domain/`. Domain entities, value objects, and events are strictly isolated.
- **In-Memory Matching Engine:** FIFO Price-Time Priority order book supporting Limit and Market Orders. Prices represented as `int64` VND ticks to eliminate IEEE 754 floating-point inaccuracies.
- **O(1) Doubly-Linked List Queues:** Orders at each price level form a doubly-linked list (`Head`/`Tail`), enabling $O(1)$ queue insertion and order cancellation.
- **Pre-Trade Risk & Sức Mua (Purchasing Power):** Immediate in-memory fund reservation ($Price \times Quantity \times (1 + FeeRate)$) with zero overdraft guarantees.
- **Double-Entry Bookkeeping:** Atomic debit/credit journal vouchers satisfying $\sum \text{Debits} = \sum \text{Credits}$ for all cash and stock settlements.
- **Event-Driven Architecture (EDA):** Kafka / Redpanda immutable event log powered by **Watermill** (`orders.placement`, `orders.events`, `market.depth`).
- **Asynchronous Settlement Worker:** Dedicated consumer persisting executed trades and ledger vouchers to PostgreSQL 16 via `pgx.Batch`.
- **Pre-Trade Idempotency:** Redis key deduplication (`SETNX`) on `ClientOrderID`.
- **Interactive Web Terminal & Invariant Test Suite:** Real-time Level 2 Order Book, Execution Tape, Double-entry Ledger, Kafka Inspector, and Bilingual (English & Vietnamese) interface.

> **Status:** this is a PoC. After the phase-03 fix run the backend (matching, risk, settlement, auth, persistence, Kafka, Redis, WebSocket) is wired and tested; the web terminal is still a simulation. See [Implementation Status](#7-implementation-status).

---

## 2. Tech Stack

| Layer | Technology | Notes |
|---|---|---|
| Language | **Go 1.22** | Three binaries: `gateway`, `engine`, `settlement` |
| HTTP / WebSocket | `go-chi/chi` v5, `go-chi/cors`, `gorilla/websocket` | REST on `:5470`, `/ws/market` stream |
| Messaging | **Watermill** v1.3 (Kafka-compatible) over **Redpanda** v24.1 | Topics `orders.placement`, `orders.events`, `market.depth`; in-memory Go-channel bus is the only adapter wired today |
| Database | **PostgreSQL 16** via `jackc/pgx` v5 (`pgxpool`, `pgx.Batch`) | Local container or **Supabase** (transaction pooler, simple protocol for PgBouncer) |
| Cache / idempotency | **Redis 7** via `go-redis` v9 | `SETNX` on `ClientOrderID`, with in-memory fallback |
| IDs | `google/uuid` | Order IDs; trade IDs are `TRD-<ns>-<seq>` |
| Contracts | Protocol Buffers (`proto/`) | Schema definitions; messages are currently JSON on the wire |
| Frontend | **React 18**, **TypeScript 5**, **Vite 5**, **Tailwind CSS 4**, `lucide-react` | Bilingual (EN/VI) terminal on `:5471`; pnpm |
| Packaging | Docker multi-stage (Alpine 3.19), Docker Compose | Targets `gateway`, `engine`, `settlement` |
| Tooling | Make, **k6**, PowerShell dev script (`scripts/dev.ps1`) | Load test, Supabase migration helpers (Go and Python) |
| Testing | `go test` (+ `-race`, benchmarks) | Domain matcher and order book tests |

**Architectural patterns:** DDD, Hexagonal (ports and adapters), CQRS-style command/query handlers, event-driven settlement, double-entry bookkeeping.

---

## 3. Directory Layout (DDD & Hexagonal)

```text
.
├── cmd/
│   ├── gateway/                  # REST API & WebSocket Market Data Gateway
│   ├── engine/                   # Core In-Memory Matching Engine
│   └── settlement/               # Kafka Consumer -> PostgreSQL 16 Batch Settlement
├── proto/                        # Protocol Buffers Definitions
│   ├── order.proto               # PlaceOrder, CancelOrder, ExecutionEvent
│   └── market.proto              # MarketDepthSnapshot (L2), TradeTick
├── internal/
│   ├── domain/                   # PURE DOMAIN (Zero external dependencies)
│   │   ├── order/                # Order entity, OrderBook, FIFO Matcher, Events
│   │   ├── account/              # Account, Purchasing Power ("Sức mua"), Double-entry Ledger
│   │   └── common/               # Domain errors (ErrInsufficientBalance, ErrInvalidPrice)
│   ├── app/                      # APPLICATION USE CASES (CQRS)
│   │   ├── command/              # PlaceOrderCommandHandler, CancelOrderCommandHandler
│   │   ├── query/                # GetOrderBookHandler, GetBalanceHandler
│   │   └── ports/                # Repository & EventPublisher interfaces
│   └── infrastructure/           # ADAPTERS & EXTERNAL DRIVERS
│       ├── persistence/          # PostgreSQL (pgx/v5) + SQL Migrations
│       ├── kafka/                # Watermill Kafka producer & consumer
│       ├── redis/                # Redis idempotency store
│       └── transport/            # Chi HTTP router & Gorilla WebSocket Hub
├── web/                          # FRONTEND WEB APPLICATION (React + Vite + Tailwind + TypeScript)
│   ├── src/
│   │   ├── components/           # Trading terminal, L2 depth, Ledger, Kafka viewer, Invariant test
│   │   ├── engine/               # Client-side deterministic matching & ledger simulation
│   │   ├── i18n/                 # Bilingual localization (English default & Vietnamese)
│   │   ├── App.tsx               # Main application container
│   │   └── main.tsx              # React entrypoint
│   ├── public/favicon.svg
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── deployments/
│   ├── docker-compose.yml        # Redpanda, PostgreSQL 16, Redis, Services
│   ├── docker-compose.services.yml # Services only, against external infra
│   └── Dockerfile                # Multi-stage Alpine container build
├── scripts/
│   ├── k6_benchmark.js           # Load testing targeting >5,000 orders/sec
│   ├── migrate_to_supabase.go/.py # Schema migration + account verification
│   └── dev.ps1                   # Windows dev launcher
├── Makefile
├── go.mod
└── README.md
```

---

## 4. Quick Start & Execution

### Prerequisites
- Go 1.22+
- Node.js 18+ & pnpm
- Docker & Docker Compose
- (Optional) k6 for load testing

### Configuration
Copy `.env.example` to `.env`. Key variables: `PORT` (5470), `WEB_PORT` (5471), `DATABASE_URL`, `DIRECT_URL`, `KAFKA_BROKERS`, `REDIS_ADDR`, and the `SUPABASE_*` keys. Never commit real credentials. Without `DATABASE_URL` the gateway falls back to in-memory accounts.

### Run Backend with Docker Compose

#### Option A: Standalone Stack (Self-contained)
```bash
make docker-up
```
This spins up:
1. **Redpanda (Kafka)** at `localhost:19092`
2. **PostgreSQL 16** at `localhost:5432` with pre-seeded accounts
3. **Redis 7** at `localhost:6379`
4. **Domain-Driven Trading Services** (Engine, Gateway, Settlement Worker)

#### Option B: Domain-Driven Trading Services Only (Using External Infrastructure)
When running against existing or external shared infrastructure (Kafka/Redpanda, PostgreSQL 16, Redis):
```bash
# Start only Engine, Gateway, and Settlement Worker:
make docker-up SERVICES_ONLY=true
# Or shortcut:
make docker-up-services

# (Optional) Seed accounts into PostgreSQL:
make seed-db

# Stop services only:
make docker-down-services
```

### Run Unit Tests & Invariant Verification
```bash
make test
# Or with race condition detector
make test-race
```

### Run Benchmarks
```bash
make benchmark
```

### Run Web Frontend
```bash
cd web
pnpm install
pnpm dev
```
Navigate to `http://localhost:5471` to access the interactive bilingual trading terminal (API Gateway runs on `http://localhost:5470`).

### High-Throughput Load Testing (k6)
```bash
make k6-load
```

---

## 5. API Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/v1/orders` | Place a new limit or market order |
| `DELETE`| `/v1/orders/{orderID}?account_id={acc}&symbol={sym}` | Cancel active order |
| `GET` | `/v1/orderbook?symbol=APX&levels=10` | Get Level 2 market depth |
| `GET` | `/v1/balance/{accountID}` | Query real-time account purchasing power |
| `GET` | `/ws/market` | WebSocket stream for L2 depth snapshots & trade ticks |
| `GET` | `/health` | Service health status |

---

## 6. Double-Entry Accounting Verification

For every executed trade:
- **Buyer Ledger:**
  - $\text{Debit Asset (Stock): } +\text{Quantity}$
  - $\text{Credit Cash (VND): } -(\text{Notional} + \text{BuyerFee})$
- **Seller Ledger:**
  - $\text{Credit Asset (Stock): } -\text{Quantity}$
  - $\text{Debit Cash (VND): } +(\text{Notional} - \text{SellerFee})$
- **Exchange Fee Collector:**
  - $\text{Debit Cash (VND): } +(\text{BuyerFee} + \text{SellerFee})$

$$\sum \text{Stock Debits} = \sum \text{Stock Credits}$$
$$\sum \text{Cash Debits} = \sum \text{Cash Credits}$$
Zero overdraft invariant is enforced prior to entering the matching queue.

---

## 7. Implementation Status

| Capability | State |
|---|---|
| FIFO price-time matching, limit and market orders | Working, thread-safe, self-trade prevention, unit-tested |
| Pre-trade reservation (cash and shares) and cancel release | Working: per-order reservation, both sides settled, fees to `FEE_COLLECTOR`, conservation test |
| Input validation | Working: side, type, qty/price bounds, tick, symbol whitelist, checked arithmetic |
| REST API (`/v1/orders`, `/v1/orderbook`, `/v1/balance`, `/health`) | Working; domain errors map to 404/409/422, 413, 415, 429 |
| AuthN/Z | JWT (JWKS) on `/v1`, account taken from the token claim; `AUTH_MODE=disabled-for-local-dev` (needs `ALLOW_INSECURE_DEV_AUTH=1`, loopback only). Unverified against a real Supabase issuer |
| Account persistence on PostgreSQL / Supabase | Working: no create-on-read, atomic conditional reserve, holdings persisted, explicit seeding |
| Trade and double-entry journal persistence | Working: one DB transaction per order; verified on a throwaway Postgres |
| Order persistence and recovery | Working: book and locks rebuilt on start (restart test). Crash between the three commits can leave drift |
| Kafka / Redpanda transport | Working: Watermill-Kafka by `KAFKA_BROKERS`; `BUS=memory` is explicit. `cmd/engine` still uses the in-memory bus (deprecated) |
| Redis idempotency | Working: `REDIS_ADDR`, key `idem:{account}:{key}`, replay returns the stored result; `IDEMPOTENCY=memory` is explicit |
| WebSocket `/ws/market` | Working: fed from the bus, origin allowlist, client cap, ping/pong |
| Settlement worker | Acks only after persist, DLQ for poison, fresh-context flush; idempotent trade insert |
| Container hardening / CI | Non-root, digest-pinned images, read-only compose services, Go 1.27.3, CI workflow (not yet run on GitHub). Images must be rebuilt to take effect |
| Web terminal | Not done: still a client-side simulation; it does not call the gateway or send a token |
| Not done | Proto contracts (D8), SCRAM auth for Kafka, per-account rate limits, durable outbox |

Full diagrams and a gap-closing plan: `C:/Git/.notes/JOBs/fin/domain-driven-trading/architecture/architecture.md`.
