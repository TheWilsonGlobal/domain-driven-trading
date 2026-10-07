export type OrderSide = 'BUY' | 'SELL';
export type OrderType = 'LIMIT' | 'MARKET';
export type OrderStatus = 'NEW' | 'PARTIALLY_FILLED' | 'FILLED' | 'CANCELED' | 'REJECTED';

export interface Order {
  id: string;
  clientOrderId: string;
  accountId: string;
  symbol: string;
  side: OrderSide;
  type: OrderType;
  price: number; // In VND ticks (integer)
  quantity: number; // In shares
  filledQuantity: number;
  remainingQuantity: number;
  status: OrderStatus;
  createdAt: number;
  updatedAt: number;
}

export interface TradeMatch {
  tradeId: string;
  symbol: string;
  makerOrderId: string;
  takerOrderId: string;
  makerAccountId: string;
  takerAccountId: string;
  takerSide: OrderSide;
  price: number;
  quantity: number;
  timestamp: number;
}

export interface PriceLevel {
  price: number;
  totalQuantity: number;
  orderCount: number;
  depthPercent?: number;
}

export interface MarketDepth {
  symbol: string;
  sequence: number;
  bids: PriceLevel[]; // Descending
  asks: PriceLevel[]; // Ascending
  bestBid: number;
  bestAsk: number;
  spread: number;
  spreadPercent: number;
  timestamp: number;
}

export interface AccountHolding {
  symbol: string;
  quantity: number;
  lockedQuantity: number;
}

export interface Account {
  id: string;
  name: string;
  cashBalance: number; // In VND
  lockedCash: number;   // Reserved for open buy orders
  holdings: Record<string, number>; // symbol -> quantity
  lockedHoldings: Record<string, number>; // symbol -> quantity
}

export type LedgerEntryType = 'DEBIT' | 'CREDIT';
export type AssetType = 'CASH_VND' | 'STOCK_SHARE';

export interface LedgerEntry {
  entryId: string;
  txId: string;
  accountId: string;
  type: LedgerEntryType;
  assetType: AssetType;
  symbol?: string;
  amount: number;
  description: string;
  timestamp: number;
}

export interface LedgerTransaction {
  txId: string;
  tradeId: string;
  entries: LedgerEntry[];
  timestamp: number;
  isBalanced: boolean;
}

export interface KafkaEvent {
  topic: 'orders.placement' | 'orders.events' | 'market.depth';
  partitionKey: string;
  eventType: 'OrderPlaced' | 'OrderMatched' | 'OrderCanceled' | 'MarketDepth';
  timestamp: number;
  payload: Record<string, any>;
}
