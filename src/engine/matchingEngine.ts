import {
  Account,
  KafkaEvent,
  LedgerEntry,
  LedgerTransaction,
  MarketDepth,
  Order,
  OrderSide,
  OrderStatus,
  OrderType,
  PriceLevel,
  TradeMatch,
} from './types';

export const BROKERAGE_FEE_BPS = 15; // 0.15% = 15 bps

export class VortexSimulationEngine {
  private orders: Map<string, Order> = new Map();
  private bids: Map<string, Map<number, Order[]>> = new Map(); // symbol -> (price -> orders[])
  private asks: Map<string, Map<number, Order[]>> = new Map(); // symbol -> (price -> orders[])
  private accounts: Map<string, Account> = new Map();
  private tradeHistory: TradeMatch[] = [];
  private ledgerTransactions: LedgerTransaction[] = [];
  private eventLog: KafkaEvent[] = [];
  private processedClientOrderIds: Set<string> = new Set();
  private depthSeq: Map<string, number> = new Map();

  // Callbacks for live reactivity
  private onStateChangeListeners: Array<() => void> = [];

  constructor() {
    this.initDefaultState();
  }

  public subscribe(cb: () => void) {
    this.onStateChangeListeners.push(cb);
    return () => {
      this.onStateChangeListeners = this.onStateChangeListeners.filter((l) => l !== cb);
    };
  }

  private notify() {
    this.onStateChangeListeners.forEach((cb) => cb());
  }

  public initDefaultState() {
    this.orders.clear();
    this.bids.clear();
    this.asks.clear();
    this.accounts.clear();
    this.tradeHistory = [];
    this.ledgerTransactions = [];
    this.eventLog = [];
    this.processedClientOrderIds.clear();

    const symbols = ['VPB', 'HPG', 'FPT', 'SSI', 'MWG'];
    symbols.forEach((s) => {
      this.bids.set(s, new Map());
      this.asks.set(s, new Map());
      this.depthSeq.set(s, 0);
    });

    // Seed Accounts
    this.accounts.set('ACC_VPB_001', {
      id: 'ACC_VPB_001',
      name: 'Nguyen Van An (VPBankS Retail)',
      cashBalance: 500_000_000, // 500M VND
      lockedCash: 0,
      holdings: { VPB: 15000, HPG: 8000, FPT: 2000, SSI: 4000, MWG: 1000 },
      lockedHoldings: { VPB: 0, HPG: 0, FPT: 0, SSI: 0, MWG: 0 },
    });

    this.accounts.set('ACC_VPB_002', {
      id: 'ACC_VPB_002',
      name: 'Tran Thi Mai (VPBankS VIP)',
      cashBalance: 350_000_000, // 350M VND
      lockedCash: 0,
      holdings: { VPB: 10000, HPG: 12000, FPT: 3000, SSI: 6000, MWG: 2500 },
      lockedHoldings: { VPB: 0, HPG: 0, FPT: 0, SSI: 0, MWG: 0 },
    });

    this.accounts.set('ACC_INST_MM', {
      id: 'ACC_INST_MM',
      name: 'Vortex Liquidity Provider (MM)',
      cashBalance: 25_000_000_000, // 25 Billion VND
      lockedCash: 0,
      holdings: { VPB: 200000, HPG: 150000, FPT: 80000, SSI: 120000, MWG: 50000 },
      lockedHoldings: { VPB: 0, HPG: 0, FPT: 0, SSI: 0, MWG: 0 },
    });

    this.accounts.set('FEE_COLLECTOR', {
      id: 'FEE_COLLECTOR',
      name: 'Exchange Clearing House',
      cashBalance: 0,
      lockedCash: 0,
      holdings: {},
      lockedHoldings: {},
    });

    // Seed realistic order books for VPB
    this.seedMarketMakerLiquidity('VPB', 19500);
    this.seedMarketMakerLiquidity('HPG', 28400);
    this.seedMarketMakerLiquidity('FPT', 132000);
    this.seedMarketMakerLiquidity('SSI', 34200);

    this.notify();
  }

  private seedMarketMakerLiquidity(symbol: string, basePrice: number) {
    const tick = symbol === 'FPT' ? 500 : 50;

    // Bids (Buy side)
    const bidSpreadDeltas = [1, 2, 3, 4, 5, 6, 7];
    bidSpreadDeltas.forEach((delta, idx) => {
      const price = basePrice - delta * tick;
      const qty = (10 + idx * 8) * 100;
      this.placeOrder({
        clientOrderId: `MM_INIT_BID_${symbol}_${idx}`,
        accountId: 'ACC_INST_MM',
        symbol,
        side: 'BUY',
        type: 'LIMIT',
        price,
        quantity: qty,
      });
    });

    // Asks (Sell side)
    const askSpreadDeltas = [1, 2, 3, 4, 5, 6, 7];
    askSpreadDeltas.forEach((delta, idx) => {
      const price = basePrice + delta * tick;
      const qty = (12 + idx * 6) * 100;
      this.placeOrder({
        clientOrderId: `MM_INIT_ASK_${symbol}_${idx}`,
        accountId: 'ACC_INST_MM',
        symbol,
        side: 'SELL',
        type: 'LIMIT',
        price,
        quantity: qty,
      });
    });
  }

  public getAccount(id: string): Account | undefined {
    return this.accounts.get(id);
  }

  public getAllAccounts(): Account[] {
    return Array.from(this.accounts.values());
  }

  public getAvailableCash(accountId: string): number {
    const acc = this.accounts.get(accountId);
    if (!acc) return 0;
    return Math.max(0, acc.cashBalance - acc.lockedCash);
  }

  public getAvailableHolding(accountId: string, symbol: string): number {
    const acc = this.accounts.get(accountId);
    if (!acc) return 0;
    const total = acc.holdings[symbol] || 0;
    const locked = acc.lockedHoldings[symbol] || 0;
    return Math.max(0, total - locked);
  }

  public calculateRequiredFunds(price: number, qty: number): number {
    const notional = price * qty;
    const fee = Math.floor((notional * BROKERAGE_FEE_BPS) / 10000);
    return notional + fee;
  }

  public placeOrder(params: {
    clientOrderId?: string;
    accountId: string;
    symbol: string;
    side: OrderSide;
    type: OrderType;
    price: number;
    quantity: number;
  }): { order: Order; matches: TradeMatch[]; latencyMicros: number; error?: string } {
    const startMicros = performance.now();

    const clientOrderId = params.clientOrderId || `CLI_${Date.now()}_${Math.random().toString(36).substring(2, 7)}`;
    const acc = this.accounts.get(params.accountId);

    if (!acc) {
      return { order: null as any, matches: [], latencyMicros: 0, error: `Account ${params.accountId} not found` };
    }

    // 1. Idempotency Check
    if (this.processedClientOrderIds.has(clientOrderId)) {
      return {
        order: null as any,
        matches: [],
        latencyMicros: 0,
        error: `ClientOrderId ${clientOrderId} duplicate rejected (Idempotency violation)`,
      };
    }
    this.processedClientOrderIds.add(clientOrderId);

    // 2. Pre-Trade Risk & Sức Mua Verification (Zero Overdraft Guarantee)
    let reservedFunds = 0;
    if (params.side === 'BUY') {
      const required = this.calculateRequiredFunds(params.price, params.quantity);
      const available = acc.cashBalance - acc.lockedCash;

      if (available < required) {
        return {
          order: null as any,
          matches: [],
          latencyMicros: 0,
          error: `Insufficient purchasing power. Required: ${required.toLocaleString()} VND, Available: ${available.toLocaleString()} VND`,
        };
      }
      // Immediate lock
      acc.lockedCash += required;
      reservedFunds = required;
    } else {
      const availableHolding = (acc.holdings[params.symbol] || 0) - (acc.lockedHoldings[params.symbol] || 0);
      if (availableHolding < params.quantity) {
        return {
          order: null as any,
          matches: [],
          latencyMicros: 0,
          error: `Insufficient stock holding for ${params.symbol}. Required: ${params.quantity}, Available: ${availableHolding}`,
        };
      }
      acc.lockedHoldings[params.symbol] = (acc.lockedHoldings[params.symbol] || 0) + params.quantity;
    }

    const orderId = `ORD_${Math.random().toString(36).substring(2, 8).toUpperCase()}`;
    const order: Order = {
      id: orderId,
      clientOrderId,
      accountId: params.accountId,
      symbol: params.symbol,
      side: params.side,
      type: params.type,
      price: params.price,
      quantity: params.quantity,
      filledQuantity: 0,
      remainingQuantity: params.quantity,
      status: 'NEW',
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };

    this.orders.set(orderId, order);

    // Log EDA Placement Event
    this.logKafkaEvent({
      topic: 'orders.placement',
      partitionKey: params.symbol,
      eventType: 'OrderPlaced',
      timestamp: Date.now(),
      payload: { ...order },
    });

    // 3. FIFO Matching Engine Execution
    const matches: TradeMatch[] = [];

    if (params.side === 'BUY') {
      this.matchBuyOrder(order, matches, reservedFunds);
    } else {
      this.matchSellOrder(order, matches);
    }

    // 4. If Limit order and remaining > 0, rest in Order Book
    if (order.remainingQuantity > 0 && order.type === 'LIMIT') {
      this.restLimitOrder(order);
    }

    const latencyMicros = Math.round((performance.now() - startMicros) * 1000);

    // Emit market depth update
    this.emitMarketDepth(params.symbol);
    this.notify();

    return { order, matches, latencyMicros };
  }

  private matchBuyOrder(taker: Order, matches: TradeMatch[], reservedFunds: number) {
    const symbolAsks = this.asks.get(taker.symbol);
    if (!symbolAsks) return;

    // Sort asks ascending (lowest sell price first)
    const sortedPrices = Array.from(symbolAsks.keys()).sort((a, b) => a - b);

    for (const askPrice of sortedPrices) {
      if (taker.remainingQuantity <= 0) break;

      // Price limit check for BUY: Taker buy price must be >= Ask price
      if (taker.type === 'LIMIT' && taker.price < askPrice) {
        break;
      }

      const queue = symbolAsks.get(askPrice) || [];
      const remainingQueue: Order[] = [];

      for (let i = 0; i < queue.length; i++) {
        const maker = queue[i];
        if (taker.remainingQuantity <= 0) {
          remainingQueue.push(maker);
          continue;
        }

        const matchQty = Math.min(taker.remainingQuantity, maker.remainingQuantity);
        const matchPrice = maker.price; // Maker sets execution price

        taker.filledQuantity += matchQty;
        taker.remainingQuantity -= matchQty;
        maker.filledQuantity += matchQty;
        maker.remainingQuantity -= matchQty;

        taker.status = taker.remainingQuantity === 0 ? 'FILLED' : 'PARTIALLY_FILLED';
        maker.status = maker.remainingQuantity === 0 ? 'FILLED' : 'PARTIALLY_FILLED';

        const tradeId = `TRD_${Date.now()}_${Math.random().toString(36).substring(2, 6).toUpperCase()}`;
        const match: TradeMatch = {
          tradeId,
          symbol: taker.symbol,
          makerOrderId: maker.id,
          takerOrderId: taker.id,
          makerAccountId: maker.accountId,
          takerAccountId: taker.accountId,
          takerSide: 'BUY',
          price: matchPrice,
          quantity: matchQty,
          timestamp: Date.now(),
        };

        matches.push(match);
        this.tradeHistory.unshift(match);
        if (this.tradeHistory.length > 200) this.tradeHistory.pop();

        // Financial & Double-entry settlement
        this.settleTrade(match, reservedFunds);

        // EDA Event
        this.logKafkaEvent({
          topic: 'orders.events',
          partitionKey: taker.symbol,
          eventType: 'OrderMatched',
          timestamp: Date.now(),
          payload: match,
        });

        if (maker.remainingQuantity > 0) {
          remainingQueue.push(maker);
        }
      }

      if (remainingQueue.length > 0) {
        symbolAsks.set(askPrice, remainingQueue);
      } else {
        symbolAsks.delete(askPrice);
      }
    }
  }

  private matchSellOrder(taker: Order, matches: TradeMatch[]) {
    const symbolBids = this.bids.get(taker.symbol);
    if (!symbolBids) return;

    // Sort bids descending (highest buy price first)
    const sortedPrices = Array.from(symbolBids.keys()).sort((a, b) => b - a);

    for (const bidPrice of sortedPrices) {
      if (taker.remainingQuantity <= 0) break;

      // Price limit check for SELL: Taker sell price must be <= Bid price
      if (taker.type === 'LIMIT' && taker.price > bidPrice) {
        break;
      }

      const queue = symbolBids.get(bidPrice) || [];
      const remainingQueue: Order[] = [];

      for (let i = 0; i < queue.length; i++) {
        const maker = queue[i];
        if (taker.remainingQuantity <= 0) {
          remainingQueue.push(maker);
          continue;
        }

        const matchQty = Math.min(taker.remainingQuantity, maker.remainingQuantity);
        const matchPrice = maker.price;

        taker.filledQuantity += matchQty;
        taker.remainingQuantity -= matchQty;
        maker.filledQuantity += matchQty;
        maker.remainingQuantity -= matchQty;

        taker.status = taker.remainingQuantity === 0 ? 'FILLED' : 'PARTIALLY_FILLED';
        maker.status = maker.remainingQuantity === 0 ? 'FILLED' : 'PARTIALLY_FILLED';

        const tradeId = `TRD_${Date.now()}_${Math.random().toString(36).substring(2, 6).toUpperCase()}`;
        const match: TradeMatch = {
          tradeId,
          symbol: taker.symbol,
          makerOrderId: maker.id,
          takerOrderId: taker.id,
          makerAccountId: maker.accountId,
          takerAccountId: taker.accountId,
          takerSide: 'SELL',
          price: matchPrice,
          quantity: matchQty,
          timestamp: Date.now(),
        };

        matches.push(match);
        this.tradeHistory.unshift(match);
        if (this.tradeHistory.length > 200) this.tradeHistory.pop();

        // Financial settlement
        this.settleTrade(match, 0);

        this.logKafkaEvent({
          topic: 'orders.events',
          partitionKey: taker.symbol,
          eventType: 'OrderMatched',
          timestamp: Date.now(),
          payload: match,
        });

        if (maker.remainingQuantity > 0) {
          remainingQueue.push(maker);
        }
      }

      if (remainingQueue.length > 0) {
        symbolBids.set(bidPrice, remainingQueue);
      } else {
        symbolBids.delete(bidPrice);
      }
    }
  }

  private restLimitOrder(order: Order) {
    const book = order.side === 'BUY' ? this.bids.get(order.symbol) : this.asks.get(order.symbol);
    if (!book) return;

    if (!book.has(order.price)) {
      book.set(order.price, []);
    }
    book.get(order.price)!.push(order);
  }

  private settleTrade(trade: TradeMatch, takerReservedFunds: number) {
    const buyerId = trade.takerSide === 'BUY' ? trade.takerAccountId : trade.makerAccountId;
    const sellerId = trade.takerSide === 'SELL' ? trade.takerAccountId : trade.makerAccountId;
    const buyer = this.accounts.get(buyerId);
    const seller = this.accounts.get(sellerId);
    const feeCollector = this.accounts.get('FEE_COLLECTOR');

    if (!buyer || !seller || !feeCollector) return;

    const notional = trade.price * trade.quantity;
    const buyerFee = Math.floor((notional * BROKERAGE_FEE_BPS) / 10000);
    const sellerFee = Math.floor((notional * BROKERAGE_FEE_BPS) / 10000);
    const totalBuyerCost = notional + buyerFee;
    const netSellerProceeds = notional - sellerFee;

    // Buyer settlement:
    // Unlock reserved cash
    const unlockAmount = trade.takerSide === 'BUY' ? takerReservedFunds : this.calculateRequiredFunds(trade.price, trade.quantity);
    buyer.lockedCash = Math.max(0, buyer.lockedCash - unlockAmount);
    buyer.cashBalance -= totalBuyerCost;
    buyer.holdings[trade.symbol] = (buyer.holdings[trade.symbol] || 0) + trade.quantity;

    // Seller settlement:
    seller.lockedHoldings[trade.symbol] = Math.max(0, (seller.lockedHoldings[trade.symbol] || 0) - trade.quantity);
    seller.holdings[trade.symbol] = Math.max(0, (seller.holdings[trade.symbol] || 0) - trade.quantity);
    seller.cashBalance += netSellerProceeds;

    // Exchange fee collector
    feeCollector.cashBalance += (buyerFee + sellerFee);

    // Create Double-Entry Ledger Voucher
    const now = Date.now();
    const txId = `TX_${trade.tradeId}`;
    const entries: LedgerEntry[] = [
      {
        entryId: `${txId}_1`,
        txId,
        accountId: buyerId,
        type: 'DEBIT',
        assetType: 'STOCK_SHARE',
        symbol: trade.symbol,
        amount: trade.quantity,
        description: `Acquired ${trade.quantity} shares of ${trade.symbol}`,
        timestamp: now,
      },
      {
        entryId: `${txId}_2`,
        txId,
        accountId: sellerId,
        type: 'CREDIT',
        assetType: 'STOCK_SHARE',
        symbol: trade.symbol,
        amount: trade.quantity,
        description: `Disposed ${trade.quantity} shares of ${trade.symbol}`,
        timestamp: now,
      },
      {
        entryId: `${txId}_3`,
        txId,
        accountId: buyerId,
        type: 'CREDIT',
        assetType: 'CASH_VND',
        amount: totalBuyerCost,
        description: `Cash outflow for ${trade.symbol} + 0.15% fee`,
        timestamp: now,
      },
      {
        entryId: `${txId}_4`,
        txId,
        accountId: sellerId,
        type: 'DEBIT',
        assetType: 'CASH_VND',
        amount: netSellerProceeds,
        description: `Net cash inflow from ${trade.symbol} sale`,
        timestamp: now,
      },
      {
        entryId: `${txId}_5`,
        txId,
        accountId: 'FEE_COLLECTOR',
        type: 'DEBIT',
        assetType: 'CASH_VND',
        amount: buyerFee + sellerFee,
        description: `Clearing exchange brokerage fee (${buyerFee + sellerFee} VND)`,
        timestamp: now,
      },
    ];

    // Verify mathematical invariant: Total Stock Debit == Total Stock Credit, Total Cash Debit == Total Cash Credit
    const stockDebit = entries.filter((e) => e.assetType === 'STOCK_SHARE' && e.type === 'DEBIT').reduce((acc, e) => acc + e.amount, 0);
    const stockCredit = entries.filter((e) => e.assetType === 'STOCK_SHARE' && e.type === 'CREDIT').reduce((acc, e) => acc + e.amount, 0);
    const cashDebit = entries.filter((e) => e.assetType === 'CASH_VND' && e.type === 'DEBIT').reduce((acc, e) => acc + e.amount, 0);
    const cashCredit = entries.filter((e) => e.assetType === 'CASH_VND' && e.type === 'CREDIT').reduce((acc, e) => acc + e.amount, 0);

    const isBalanced = stockDebit === stockCredit && cashDebit === cashCredit;

    const voucher: LedgerTransaction = {
      txId,
      tradeId: trade.tradeId,
      entries,
      timestamp: now,
      isBalanced,
    };

    this.ledgerTransactions.unshift(voucher);
    if (this.ledgerTransactions.length > 100) this.ledgerTransactions.pop();
  }

  public cancelOrder(orderId: string): { order?: Order; error?: string } {
    const order = this.orders.get(orderId);
    if (!order) return { error: `Order ${orderId} not found` };
    if (order.status === 'FILLED' || order.status === 'CANCELED') {
      return { error: `Order is already ${order.status}` };
    }

    const remQty = order.remainingQuantity;
    order.status = 'CANCELED';
    order.updatedAt = Date.now();

    // Remove from orderbook
    const book = order.side === 'BUY' ? this.bids.get(order.symbol) : this.asks.get(order.symbol);
    if (book && book.has(order.price)) {
      const filtered = book.get(order.price)!.filter((o) => o.id !== orderId);
      if (filtered.length > 0) {
        book.set(order.price, filtered);
      } else {
        book.delete(order.price);
      }
    }

    // Release locked cash or holdings
    const acc = this.accounts.get(order.accountId);
    if (acc) {
      if (order.side === 'BUY') {
        const releasedFunds = this.calculateRequiredFunds(order.price, remQty);
        acc.lockedCash = Math.max(0, acc.lockedCash - releasedFunds);
      } else {
        acc.lockedHoldings[order.symbol] = Math.max(0, (acc.lockedHoldings[order.symbol] || 0) - remQty);
      }
    }

    this.logKafkaEvent({
      topic: 'orders.events',
      partitionKey: order.symbol,
      eventType: 'OrderCanceled',
      timestamp: Date.now(),
      payload: {
        orderId: order.id,
        accountId: order.accountId,
        symbol: order.symbol,
        canceledQuantity: remQty,
      },
    });

    this.emitMarketDepth(order.symbol);
    this.notify();

    return { order };
  }

  public getMarketDepth(symbol: string, maxLevels = 10): MarketDepth {
    const symbolBids = this.bids.get(symbol) || new Map();
    const symbolAsks = this.asks.get(symbol) || new Map();

    const sortedBidPrices = Array.from(symbolBids.keys()).sort((a, b) => b - a);
    const sortedAskPrices = Array.from(symbolAsks.keys()).sort((a, b) => a - b);

    const bids: PriceLevel[] = sortedBidPrices.slice(0, maxLevels).map((price) => {
      const orders = symbolBids.get(price) || [];
      const totalQuantity = orders.reduce((sum: number, o: Order) => sum + o.remainingQuantity, 0);
      return { price, totalQuantity, orderCount: orders.length };
    });

    const asks: PriceLevel[] = sortedAskPrices.slice(0, maxLevels).map((price) => {
      const orders = symbolAsks.get(price) || [];
      const totalQuantity = orders.reduce((sum: number, o: Order) => sum + o.remainingQuantity, 0);
      return { price, totalQuantity, orderCount: orders.length };
    });

    const maxVolume = Math.max(
      ...bids.map((b) => b.totalQuantity),
      ...asks.map((a) => a.totalQuantity),
      1,
    );

    bids.forEach((b) => (b.depthPercent = Math.round((b.totalQuantity / maxVolume) * 100)));
    asks.forEach((a) => (a.depthPercent = Math.round((a.totalQuantity / maxVolume) * 100)));

    const bestBid = bids.length > 0 ? bids[0].price : 0;
    const bestAsk = asks.length > 0 ? asks[0].price : 0;
    const spread = bestBid && bestAsk ? bestAsk - bestBid : 0;
    const spreadPercent = bestBid && bestAsk ? +((spread / bestAsk) * 100).toFixed(2) : 0;

    const seq = (this.depthSeq.get(symbol) || 0) + 1;
    this.depthSeq.set(symbol, seq);

    return {
      symbol,
      sequence: seq,
      bids,
      asks,
      bestBid,
      bestAsk,
      spread,
      spreadPercent,
      timestamp: Date.now(),
    };
  }

  private emitMarketDepth(symbol: string) {
    const depth = this.getMarketDepth(symbol, 10);
    this.logKafkaEvent({
      topic: 'market.depth',
      partitionKey: symbol,
      eventType: 'MarketDepth',
      timestamp: Date.now(),
      payload: depth,
    });
  }

  private logKafkaEvent(event: KafkaEvent) {
    this.eventLog.unshift(event);
    if (this.eventLog.length > 150) this.eventLog.pop();
  }

  public getTradeHistory(): TradeMatch[] {
    return this.tradeHistory;
  }

  public getLedgerTransactions(): LedgerTransaction[] {
    return this.ledgerTransactions;
  }

  public getEventLog(): KafkaEvent[] {
    return this.eventLog;
  }

  public getActiveOrders(accountId?: string): Order[] {
    const all = Array.from(this.orders.values()).filter(
      (o) => o.status === 'NEW' || o.status === 'PARTIALLY_FILLED',
    );
    if (accountId) {
      return all.filter((o) => o.accountId === accountId);
    }
    return all.sort((a, b) => b.createdAt - a.createdAt);
  }
}

// Global Singleton Instance for live simulation
export const engineInstance = new VortexSimulationEngine();
