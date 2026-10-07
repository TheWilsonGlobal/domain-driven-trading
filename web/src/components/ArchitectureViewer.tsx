import React, { useState } from 'react';
import { Check, Copy, FileCode, Folder, GitBranch, Terminal } from 'lucide-react';
import { useLanguage } from '../i18n/LanguageContext';

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

	q.TotalQuantity -= o.RemainingQuantity()
	q.Count--
	o.Prev = nil
	o.Next = nil
}`,
  },
  {
    path: 'internal/domain/account/account.go',
    category: 'domain',
    title: 'Account Purchasing Power & Overdraft Invariant',
    language: 'go',
    content: `package account

import "errors"

var ErrInsufficientBalance = errors.New("insufficient purchasing power")

type Account struct {
	ID          string
	CashBalance int64 // In VND ticks (avoid floating point IEEE 754)
	LockedCash  int64 // Pre-allocated for open buy orders
	Holdings    map[string]int64
	LockedStock map[string]int64
}

// ReserveFunds guarantees zero overdraft prior to engine matching
func (a *Account) ReserveFunds(notionalWithFee int64) error {
	freeCash := a.CashBalance - a.LockedCash
	if freeCash < notionalWithFee {
		return ErrInsufficientBalance
	}
	a.LockedCash += notionalWithFee
	return nil
}`,
  },
  {
    path: 'internal/domain/account/ledger.go',
    category: 'domain',
    title: 'Double-Entry Bookkeeping Ledger Voucher',
    language: 'go',
    content: `package account

// PostTradeExecution creates atomic debit/credit journal vouchers satisfying Sum(Debits) == Sum(Credits)
func (l *LedgerService) PostTradeExecution(trade TradeExecution) (*LedgerTransaction, error) {
	tx := NewLedgerTransaction(trade.TradeID)

	// Buyer: Debit Stock (+Qty), Credit Cash (-(Notional + Fee))
	tx.AddEntry(trade.BuyerAccountID, Debit, AssetStock, trade.Quantity)
	tx.AddEntry(trade.BuyerAccountID, Credit, AssetCash, trade.Notional + trade.BuyerFee)

	// Seller: Credit Stock (-Qty), Debit Cash (+(Notional - Fee))
	tx.AddEntry(trade.SellerAccountID, Credit, AssetStock, trade.Quantity)
	tx.AddEntry(trade.SellerAccountID, Debit, AssetCash, trade.Notional - trade.SellerFee)

	// Exchange Fee Collector: Debit Cash (+(BuyerFee + SellerFee))
	tx.AddEntry("FEE_COLLECTOR", Debit, AssetCash, trade.BuyerFee + trade.SellerFee)

	if !tx.IsBalanced() {
		return nil, ErrUnbalancedVoucher
	}
	return tx, nil
}`,
  },
  {
    path: 'internal/app/command/place_order.go',
    category: 'application',
    title: 'CQRS PlaceOrder Command Handler',
    language: 'go',
    content: `package command

type PlaceOrderCommandHandler struct {
	matcher     *order.Matcher
	accountRepo ports.AccountRepository
	eventBus    ports.EventPublisher
	idempotency ports.IdempotencyStore
}

func (h *PlaceOrderCommandHandler) Handle(ctx context.Context, cmd PlaceOrderCommand) (*order.Order, error) {
	// 1. Idempotency Check (Redis SETNX ClientOrderID)
	if exists := h.idempotency.SetNX(ctx, cmd.ClientOrderID, time.Minute*10); !exists {
		return nil, ErrDuplicateClientOrderID
	}

	// 2. Pre-Trade Risk & Sức Mua Verification
	acc, _ := h.accountRepo.GetByID(ctx, cmd.AccountID)
	if cmd.Side == order.SideBuy {
		requiredFunds := (cmd.Price * cmd.Quantity) * 10015 / 10000 // With 15 bps fee
		if err := acc.ReserveFunds(requiredFunds); err != nil {
			return nil, err
		}
	}

	// 3. Dispatch to In-Memory FIFO Engine
	matchedTrades, err := h.matcher.ProcessOrder(newOrder)

	// 4. Publish Event Stream (Kafka / Watermill)
	h.eventBus.PublishOrderPlaced(newOrder)
	for _, trade := range matchedTrades {
		h.eventBus.PublishOrderMatched(trade)
	}

	return newOrder, nil
}`,
  },
  {
    path: 'proto/order.proto',
    category: 'proto',
    title: 'Protobuf Order Schema',
    language: 'protobuf',
    content: `syntax = "proto3";
package domain_driven_trading.order.v1;

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
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
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
    container_name: ddt-redpanda
    ports: ["19092:19092"]
  postgres:
    image: postgres:16-alpine
    container_name: ddt-postgres
    ports: ["5432:5432"]
  redis:
    image: redis:7-alpine
    container_name: ddt-redis
    ports: ["6379:6379"]
  engine:
    build: { context: .., dockerfile: deployments/Dockerfile, target: engine }
    container_name: ddt-engine
  gateway:
    build: { context: .., dockerfile: deployments/Dockerfile, target: gateway }
    container_name: ddt-gateway
    ports: ["8080:8080"]
  settlement:
    build: { context: .., dockerfile: deployments/Dockerfile, target: settlement }
    container_name: ddt-settlement`,
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
  thresholds: {
    http_req_duration: ['p(95)<15', 'p(99)<35'], // Sub-millisecond engine + fast HTTP Gateway
    order_placement_success: ['rate>0.999'],       // 99.9% success rate
  },
};`,
  },
];

export const ArchitectureViewer: React.FC = () => {
  const { t } = useLanguage();
  const [selectedCategory, setSelectedCategory] = useState<string>('all');
  const [selectedPath, setSelectedPath] = useState<string>(FILES[0].path);
  const [copied, setCopied] = useState<boolean>(false);

  const categories = [
    { id: 'all', label: t.common.all },
    { id: 'domain', label: t.architecture.categories.domain },
    { id: 'application', label: t.architecture.categories.application },
    { id: 'infrastructure', label: t.architecture.categories.infrastructure },
    { id: 'proto', label: t.architecture.categories.proto },
    { id: 'deployment', label: t.architecture.categories.deployment },
  ];

  const filteredFiles = selectedCategory === 'all'
    ? FILES
    : FILES.filter((f) => f.category === selectedCategory);

  const currentFile = FILES.find((f) => f.path === selectedPath) || FILES[0];

  const handleCopy = () => {
    navigator.clipboard.writeText(currentFile.content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-slate-800/80 gap-3">
        <div>
          <h2 className="text-base font-bold text-white tracking-wide flex items-center gap-2">
            <GitBranch className="w-4 h-4 text-emerald-400" />
            {t.architecture.title}
          </h2>
          <p className="text-xs text-slate-400 mt-1">
            {t.architecture.subtitle}
          </p>
        </div>

        {/* Categories Selector */}
        <div className="flex items-center gap-1 p-0.5 bg-slate-950 border border-slate-800 rounded-md text-xs overflow-x-auto">
          {categories.map((c) => (
            <button
              key={c.id}
              onClick={() => setSelectedCategory(c.id)}
              className={`px-2.5 py-1 rounded text-[11px] font-medium transition-colors whitespace-nowrap ${
                selectedCategory === c.id
                  ? 'bg-slate-800 text-emerald-400 font-semibold'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {c.label}
            </button>
          ))}
        </div>
      </div>

      <div className="mt-4 flex flex-col lg:flex-row border border-slate-800 rounded-lg overflow-hidden min-h-[500px]">
        {/* File Tree Sidebar */}
        <div className="w-full lg:w-72 bg-slate-950 border-b lg:border-b-0 lg:border-r border-slate-800/80 p-3 flex flex-col justify-between">
          <div>
            <div className="text-[11px] font-mono text-slate-500 uppercase tracking-wider mb-2 px-2 flex items-center gap-1.5">
              <Folder className="w-3.5 h-3.5 text-slate-400" />
              <span>Project Structure</span>
            </div>
            <div className="space-y-1">
              {filteredFiles.map((f) => (
                <button
                  key={f.path}
                  onClick={() => setSelectedPath(f.path)}
                  className={`w-full text-left px-2.5 py-2 rounded text-xs font-mono transition-colors flex items-center justify-between gap-2 ${
                    selectedPath === f.path
                      ? 'bg-slate-800 text-emerald-400 border border-slate-700/80'
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
            Domain-Driven Trading Go 1.22+ PoC
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
              <span>{copied ? t.common.copied : t.common.copyCode}</span>
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
