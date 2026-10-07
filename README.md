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

---

## 2. Directory Layout (DDD & Hexagonal)

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
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── deployments/
│   ├── docker-compose.yml        # Redpanda, PostgreSQL 16, Redis, Services
│   └── Dockerfile                # Multi-stage Alpine container build
├── scripts/
│   └── k6_benchmark.js           # Load testing targeting >5,000 orders/sec
├── Makefile
├── go.mod
└── README.md
```

---

## 3. Quick Start & Execution

### Prerequisites
- Go 1.22+
- Node.js 18+ & pnpm
- Docker & Docker Compose
- (Optional) k6 for load testing

### Run Backend with Docker Compose
```bash
make docker-up
```
This spins up:
1. **Redpanda (Kafka)** at `localhost:19092`
2. **PostgreSQL 16** at `localhost:5432` with pre-seeded accounts
3. **Redis 7** at `localhost:6379`
4. **Domain-Driven Trading Services** (Engine, Gateway, Settlement Worker)

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
Navigate to `http://localhost:5173` to access the interactive bilingual trading terminal.

### High-Throughput Load Testing (k6)
```bash
make k6-load
```

---

## 4. API Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/v1/orders` | Place a new limit or market order |
| `DELETE`| `/v1/orders/{orderID}?account_id={acc}&symbol={sym}` | Cancel active order |
| `GET` | `/v1/orderbook?symbol=APX&levels=10` | Get Level 2 market depth |
| `GET` | `/v1/balance/{accountID}` | Query real-time account purchasing power |
| `GET` | `/ws/market` | WebSocket stream for L2 depth snapshots & trade ticks |
| `GET` | `/health` | Service health status |

---

## 5. Double-Entry Accounting Verification

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
