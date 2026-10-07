import React, { useState } from 'react';
import { Check, Copy, FileCode, Folder, GitBranch, Terminal } from 'lucide-react';

interface FileDefinition {
  path: string;
  category: 'domain' | 'application' | 'infrastructure' | 'cmd' | 'proto' | 'deployment';
  title: string;
  language: string;
  content: string;
}

const FILES: FileDefinition[] = [
  {
    path: 'internal/domain/order/orderbook.go',
    category: 'domain',
    title: 'OrderBook (O(1) Doubly-Linked List & FIFO Priority)',
    language: 'go',
    content: `package order

import (
	"sort"
	"sync"
	"time"
)

// OrderQueue represents a FIFO doubly-linked list of orders at a specific Price level
type OrderQueue struct {
	Price         Price
	Head          *Order
	Tail          *Order
	TotalQuantity Quantity
	Count         uint32
}

func (q *OrderQueue) Enqueue(o *Order) {
	o.Next = nil
	o.Prev = q.Tail

	if q.Tail != nil {
		q.Tail.Next = o
	} else {
		q.Head = o
	}
	q.Tail = o

	q.TotalQuantity += o.RemainingQuantity()
	q.Count++
}

func (q *OrderQueue) Remove(o *Order) {
	if o.Prev != nil {
		o.Prev.Next = o.Next
	} else {
		q.Head = o.Next
	}

	if o.Next != nil {
		o.Next.Prev = o.Prev
	} else {
		q.Tail = o.Prev
	}

	rem := o.RemainingQuantity()
	if q.TotalQuantity >= rem {
		q.TotalQuantity -= rem
	} else {
		q.TotalQuantity = 0
	}

	if q.Count > 0 {
		q.Count--
	}
	o.Prev = nil
	o.Next = nil
}`,
  },
  {
    path: 'internal/domain/order/matcher.go',
    category: 'domain',
    title: 'Matcher (Continuous FIFO Matching Engine)',
    language: 'go',
    content: `package order

// Matcher performs continuous in-memory FIFO price-time matching
type Matcher struct {
	book *OrderBook
}

func (m *Matcher) ProcessOrder(taker *Order) ([]OrderMatchedEvent, error) {
	m.book.mu.Lock()
	defer m.book.mu.Unlock()

	var events []OrderMatchedEvent

	if taker.Side == SideBuy {
		events = m.matchBuyOrder(taker)
	} else {
		events = m.matchSellOrder(taker)
	}

	// If taker has remaining quantity and is a LIMIT order, rest it in the book
	if taker.RemainingQuantity() > 0 && taker.Type == TypeLimit {
		_ = m.book.AddLimitOrder(taker)
	}

	return events, nil
}`,
  },
  {
    path: 'internal/domain/account/ledger.go',
    category: 'domain',
    title: 'Double-Entry General Ledger (Debit/Credit Conservation)',
    language: 'go',
    content: `package account

// CreateTradeSettlementJournal creates balanced double-entry vouchers
// Invariant: Total Stock Debit == Total Stock Credit && Total Cash Debit == Total Cash Credit
func CreateTradeSettlementJournal(
	tradeID, buyerID, sellerID, feeCollectorID string,
	symbol order.Symbol, price order.Price, quantity order.Quantity,
) (*Transaction, error) {
	notionalCash := price.Int64() * int64(quantity.Uint32())
	buyerFee := (notionalCash * DefaultFeeRateBasisPoints) / 10000
	sellerFee := (notionalCash * DefaultFeeRateBasisPoints) / 10000
	shares := int64(quantity.Uint32())

	// Buyer: Debit Stock (+Shares), Credit Cash (-(Notional + BuyerFee))
	// Seller: Debit Cash (+(Notional - SellerFee)), Credit Stock (-Shares)
	// Clearing: Debit Cash (+(BuyerFee + SellerFee))
	...
}`,
  },
  {
    path: 'proto/order.proto',
    category: 'proto',
    title: 'Protobuf Order Schema',
    language: 'protobuf',
    content: `syntax = "proto3";
package vortex.order.v1;

message PlaceOrderRequest {
  string client_order_id = 1;
  string account_id = 2;
  string symbol = 3;
  OrderSide side = 4;
  OrderType order_type = 5;
  int64 price = 6;       // In VND ticks (avoid floating-point errors)
  uint32 quantity = 7;   // Shares
  int64 timestamp_ns = 8;
}

message OrderExecutionEvent {
  string trade_id = 1;
  string symbol = 2;
  string maker_order_id = 3;
  string taker_order_id = 4;
  int64 match_price = 7;
  uint32 match_quantity = 8;
  int64 timestamp_ns = 9;
}`,
  },
  {
    path: 'internal/infrastructure/persistence/postgres/trade_repository.go',
    category: 'infrastructure',
    title: 'PostgreSQL pgx.Batch Trade Repository',
    language: 'go',
    content: `package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/order"
)

// SaveBatch writes trades in high-performance batches using pgx.Batch
func (r *PostgresTradeRepository) SaveBatch(ctx context.Context, trades []order.OrderMatchedEvent) error {
	batch := &pgx.Batch{}
	query := \`
		INSERT INTO trades (
			trade_id, symbol, maker_order_id, taker_order_id,
			maker_account_id, taker_account_id, taker_side, price, quantity, executed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (trade_id) DO NOTHING;
	\`

	for _, t := range trades {
		batch.Queue(query, t.TradeID, string(t.Symbol), t.MakerOrderID, t.TakerOrderID,
			t.MakerAccountID, t.TakerAccountID, string(t.TakerSide),
			t.Price.Int64(), int(t.Quantity.Uint32()), t.Timestamp)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()
	return nil
}`,
  },
  {
    path: 'deployments/docker-compose.yml',
    category: 'deployment',
    title: 'Docker Compose (Redpanda, PostgreSQL 16, Redis)',
    language: 'yaml',
    content: `version: '3.8'
services:
  redpanda:
    image: docker.redpanda.com/redpandadata/redpanda:v24.1.2
    ports: ["19092:19092"]
  postgres:
    image: postgres:16-alpine
    ports: ["5432:5432"]
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
  engine:
    build: { context: .., dockerfile: deployments/Dockerfile, target: engine }
  gateway:
    build: { context: .., dockerfile: deployments/Dockerfile, target: gateway }
    ports: ["8080:8080"]
  settlement:
    build: { context: .., dockerfile: deployments/Dockerfile, target: settlement }`,
  },
  {
    path: 'scripts/k6_benchmark.js',
    category: 'deployment',
    title: 'k6 High-Throughput Load Test (>5,000 req/sec)',
    language: 'javascript',
    content: `import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    high_throughput_orders: {
      executor: 'ramping-arrival-rate',
      startRate: 500,
      timeUnit: '1s',
      stages: [
        { target: 1500, duration: '15s' },
        { target: 5000, duration: '30s' }, // Target: 5,000 orders/sec stress phase
        { target: 6500, duration: '20s' },
      ],
    },
  },
};`,
  },
];

export const ArchitectureViewer: React.FC = () => {
  const [selectedPath, setSelectedPath] = useState<string>(FILES[0].path);
  const [copied, setCopied] = useState<boolean>(false);

  const currentFile = FILES.find((f) => f.path === selectedPath) || FILES[0];

  const handleCopy = () => {
    navigator.clipboard.writeText(currentFile.content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="space-y-6">
      {/* Architecture Explanatory Banner */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
        <div className="flex items-center gap-2 pb-3 border-b border-slate-800/80 mb-3">
          <GitBranch className="w-5 h-5 text-emerald-400" />
          <h2 className="text-base font-bold text-white tracking-wide">
            Hexagonal & Domain-Driven Design (DDD) Architecture
          </h2>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-xs">
          <div className="bg-slate-950/80 border border-slate-800/80 p-3 rounded-md">
            <span className="font-semibold text-emerald-400 block mb-1">
              1. Pure Domain Isolation
            </span>
            <p className="text-slate-400 text-[11px] leading-relaxed">
              <code>internal/domain/</code> has zero external dependencies (no Kafka, Postgres, HTTP).
              Calculations use <code>int64</code> VND ticks and doubly-linked FIFO queues for sub-millisecond execution.
            </p>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3 rounded-md">
            <span className="font-semibold text-blue-400 block mb-1">
              2. CQRS Application Layer
            </span>
            <p className="text-slate-400 text-[11px] leading-relaxed">
              <code>internal/app/</code> organizes commands (PlaceOrder, CancelOrder) and queries (GetOrderBook, GetBalance)
              interacting strictly through Repository & EventPublisher port interfaces.
            </p>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3 rounded-md">
            <span className="font-semibold text-purple-400 block mb-1">
              3. Event-Driven Infrastructure
            </span>
            <p className="text-slate-400 text-[11px] leading-relaxed">
              Watermill + Kafka (Redpanda) powers event distribution. Background settlement worker consumes
              <code>orders.events</code> and writes transactions to PostgreSQL 16 via <code>pgx.Batch</code>.
            </p>
          </div>
        </div>
      </div>

      {/* Code Browser Grid */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-lg overflow-hidden flex flex-col md:flex-row min-h-[500px]">
        {/* File Navigator Sidebar */}
        <div className="w-full md:w-80 border-b md:border-b-0 md:border-r border-slate-800 bg-slate-950/60 p-3 flex flex-col justify-between">
          <div>
            <div className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-2 px-2 flex items-center gap-1.5">
              <Folder className="w-3.5 h-3.5 text-slate-500" />
              Source Files
            </div>
            <div className="space-y-1">
              {FILES.map((f) => (
                <button
                  key={f.path}
                  onClick={() => setSelectedPath(f.path)}
                  className={`w-full text-left px-2.5 py-1.5 rounded text-xs font-mono transition-colors flex items-center justify-between ${
                    selectedPath === f.path
                      ? 'bg-slate-800 text-emerald-400 font-semibold'
                      : 'text-slate-400 hover:text-slate-200 hover:bg-slate-900/60'
                  }`}
                >
                  <span className="truncate">{f.path.split('/').pop()}</span>
                  <span className="text-[10px] text-slate-600 uppercase font-sans">{f.category}</span>
                </button>
              ))}
            </div>
          </div>

          <div className="mt-4 pt-3 border-t border-slate-800/80 text-[11px] text-slate-500 px-2 font-mono">
            Vortex Go 1.22+ PoC Codebase
          </div>
        </div>

        {/* Code Content Panel */}
        <div className="flex-1 flex flex-col bg-slate-950/90">
          <div className="px-4 py-3 border-b border-slate-800/80 flex items-center justify-between bg-slate-950">
            <div className="flex items-center gap-2">
              <FileCode className="w-4 h-4 text-emerald-400" />
              <span className="text-xs font-mono text-slate-200 font-bold">{currentFile.path}</span>
            </div>

            <button
              onClick={handleCopy}
              className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-slate-300 hover:text-white bg-slate-900 hover:bg-slate-800 border border-slate-700/80 rounded transition-colors"
            >
              {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
              <span>{copied ? 'Copied' : 'Copy Code'}</span>
            </button>
          </div>

          <div className="p-4 flex-1 overflow-x-auto font-mono text-xs text-slate-300 leading-relaxed">
            <pre>
              <code>{currentFile.content}</code>
            </pre>
          </div>
        </div>
      </div>
    </div>
  );
};
