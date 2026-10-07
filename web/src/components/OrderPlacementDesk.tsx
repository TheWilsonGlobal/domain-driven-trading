import React, { useState, useEffect } from 'react';
import { Account, OrderSide, OrderType } from '../engine/types';
import { BROKERAGE_FEE_BPS } from '../engine/matchingEngine';
import { ArrowDownLeft, ArrowUpRight, CheckCircle2, ShieldAlert } from 'lucide-react';
import { useLanguage } from '../i18n/LanguageContext';

interface OrderPlacementDeskProps {
  account: Account;
  symbol: string;
  onSymbolChange: (symbol: string) => void;
  bestBid: number;
  bestAsk: number;
  onPlaceOrder: (params: {
    symbol: string;
    side: OrderSide;
    type: OrderType;
    price: number;
    quantity: number;
  }) => { success: boolean; message: string; latencyMicros?: number };
  prefillPrice?: number;
  prefillSide?: OrderSide;
}

export const OrderPlacementDesk: React.FC<OrderPlacementDeskProps> = ({
  account,
  symbol,
  onSymbolChange,
  bestBid,
  bestAsk,
  onPlaceOrder,
  prefillPrice,
  prefillSide,
}) => {
  const { t, formatCurrency, formatNumber } = useLanguage();
  const [side, setSide] = useState<OrderSide>('BUY');
  const [orderType, setOrderType] = useState<OrderType>('LIMIT');
  const [price, setPrice] = useState<number>(19500);
  const [quantity, setQuantity] = useState<number>(1000);
  const [statusMsg, setStatusMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const availableCash = Math.max(0, account.cashBalance - account.lockedCash);
  const availableHolding = Math.max(0, (account.holdings[symbol] || 0) - (account.lockedHoldings[symbol] || 0));

  // Tick size: 50 VND for standard stocks, 500 VND for > 100k (e.g. FPT)
  const tickSize = symbol === 'FPT' ? 500 : 50;

  useEffect(() => {
    if (prefillPrice) {
      setPrice(prefillPrice);
    }
  }, [prefillPrice]);

  useEffect(() => {
    if (prefillSide) {
      setSide(prefillSide);
    }
  }, [prefillSide]);

  // Adjust default price when symbol changes
  useEffect(() => {
    if (symbol === 'APX') setPrice(19500);
    else if (symbol === 'HPG') setPrice(28400);
    else if (symbol === 'FPT') setPrice(132000);
    else if (symbol === 'SSI') setPrice(34200);
    else if (symbol === 'MWG') setPrice(56000);
  }, [symbol]);

  // Financial calculations
  const effectivePrice = orderType === 'MARKET' ? (side === 'BUY' ? (bestAsk || price) : (bestBid || price)) : price;
  const notional = effectivePrice * quantity;
  const estimatedFee = Math.floor((notional * BROKERAGE_FEE_BPS) / 10000);
  const requiredFunds = notional + estimatedFee;

  const hasSufficientFunds = side === 'BUY' ? availableCash >= requiredFunds : availableHolding >= quantity;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (quantity <= 0) {
      setStatusMsg({ type: 'error', text: t.orderDesk.errQuantityZero });
      return;
    }
    if (orderType === 'LIMIT' && price <= 0) {
      setStatusMsg({ type: 'error', text: t.orderDesk.errPriceZero });
      return;
    }

    const res = onPlaceOrder({
      symbol,
      side,
      type: orderType,
      price: effectivePrice,
      quantity,
    });

    if (res.success) {
      setStatusMsg({
        type: 'success',
        text: `${res.message} ${res.latencyMicros ? `(Engine: ${res.latencyMicros} µs)` : ''}`,
      });
      setTimeout(() => setStatusMsg(null), 4000);
    } else {
      setStatusMsg({ type: 'error', text: res.message });
    }
  };

  const symbols = ['APX', 'HPG', 'FPT', 'SSI', 'MWG'];

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-3 sm:p-4 flex flex-col justify-between">
      <div>
        {/* Header and Symbol Switcher */}
        <div className="flex items-center justify-between pb-3 border-b border-slate-800/80 mb-3">
          <div className="flex items-center gap-1.5 p-0.5 bg-slate-950 border border-slate-800 rounded-md">
            {symbols.map((sym) => (
              <button
                key={sym}
                type="button"
                onClick={() => onSymbolChange(sym)}
                className={`px-2.5 py-1 text-xs font-semibold rounded transition-colors ${
                  symbol === sym
                    ? 'bg-emerald-600 text-white shadow-sm'
                    : 'text-slate-400 hover:text-slate-200'
                }`}
              >
                {sym}
              </button>
            ))}
          </div>

          <div className="flex items-center gap-1 p-0.5 bg-slate-950 border border-slate-800 rounded-md">
            <button
              type="button"
              onClick={() => setOrderType('LIMIT')}
              className={`px-2.5 py-1 text-xs font-medium rounded transition-colors ${
                orderType === 'LIMIT' ? 'bg-slate-800 text-white' : 'text-slate-400'
              }`}
            >
              LIMIT
            </button>
            <button
              type="button"
              onClick={() => setOrderType('MARKET')}
              className={`px-2.5 py-1 text-xs font-medium rounded transition-colors ${
                orderType === 'MARKET' ? 'bg-slate-800 text-white' : 'text-slate-400'
              }`}
            >
              MARKET
            </button>
          </div>
        </div>

        {/* Side Selector (BUY / SELL) */}
        <div className="grid grid-cols-2 gap-2 mb-3">
          <button
            type="button"
            onClick={() => setSide('BUY')}
            className={`py-2 px-3 text-xs font-bold rounded-md flex items-center justify-center gap-1.5 transition-colors ${
              side === 'BUY'
                ? 'bg-emerald-500 text-slate-950 ring-2 ring-emerald-400/50'
                : 'bg-slate-950 text-slate-400 hover:bg-slate-800/60 border border-slate-800'
            }`}
          >
            <ArrowDownLeft className="w-4 h-4" />
            <span>{t.orderDesk.sideBuy}</span>
          </button>
          <button
            type="button"
            onClick={() => setSide('SELL')}
            className={`py-2 px-3 text-xs font-bold rounded-md flex items-center justify-center gap-1.5 transition-colors ${
              side === 'SELL'
                ? 'bg-rose-500 text-white ring-2 ring-rose-400/50'
                : 'bg-slate-950 text-slate-400 hover:bg-slate-800/60 border border-slate-800'
            }`}
          >
            <ArrowUpRight className="w-4 h-4" />
            <span>{t.orderDesk.sideSell}</span>
          </button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-3">
          {/* Price Input */}
          <div>
            <div className="flex justify-between text-xs mb-1">
              <label htmlFor="price-input" className="text-slate-400">{t.orderDesk.price}</label>
              <span className="text-[11px] font-mono text-slate-500">
                Tick: {tickSize} VND
              </span>
            </div>
            {orderType === 'LIMIT' ? (
              <div className="flex gap-1.5">
                <button
                  type="button"
                  onClick={() => setPrice((p) => Math.max(tickSize, p - tickSize))}
                  className="px-2.5 py-1.5 bg-slate-950 border border-slate-800 hover:border-slate-700 text-slate-300 rounded font-mono text-xs"
                >
                  -{tickSize}
                </button>
                <input
                  id="price-input"
                  type="number"
                  step={tickSize}
                  value={price}
                  onChange={(e) => setPrice(Number(e.target.value))}
                  className="flex-1 bg-slate-950 border border-slate-800 focus:border-emerald-500 px-3 py-1.5 rounded text-white font-mono text-sm focus:outline-none tabular-nums text-right"
                />
                <button
                  type="button"
                  onClick={() => setPrice((p) => p + tickSize)}
                  className="px-2.5 py-1.5 bg-slate-950 border border-slate-800 hover:border-slate-700 text-slate-300 rounded font-mono text-xs"
                >
                  +{tickSize}
                </button>
              </div>
            ) : (
              <div className="bg-slate-950 border border-slate-800/80 p-2 rounded text-xs text-amber-300 font-mono flex items-center justify-between">
                <span>{orderType === 'MARKET' ? t.orderDesk.marketOrder : ''}</span>
                <span>{side === 'BUY' ? formatCurrency(bestAsk || price) : formatCurrency(bestBid || price)} VND</span>
              </div>
            )}
          </div>

          {/* Quantity Input */}
          <div>
            <div className="flex justify-between text-xs mb-1">
              <label htmlFor="qty-input" className="text-slate-400">{t.orderDesk.quantity}</label>
              <span className="text-[11px] font-mono text-slate-500">100 / lot</span>
            </div>
            <input
              id="qty-input"
              type="number"
              step="100"
              value={quantity}
              onChange={(e) => setQuantity(Number(e.target.value))}
              className="w-full bg-slate-950 border border-slate-800 focus:border-emerald-500 px-3 py-1.5 rounded text-white font-mono text-sm focus:outline-none tabular-nums text-right"
            />
            {/* Lot presets */}
            <div className="grid grid-cols-4 gap-1 mt-1.5">
              {[500, 1000, 5000, 10000].map((lot) => (
                <button
                  key={lot}
                  type="button"
                  onClick={() => setQuantity(lot)}
                  className="py-1 px-1 bg-slate-950 border border-slate-800 hover:border-slate-700 text-slate-400 hover:text-slate-200 text-[11px] font-mono rounded"
                >
                  {formatNumber(lot)}
                </button>
              ))}
            </div>
          </div>

          {/* Purchasing Power & Risk Summary */}
          <div className="bg-slate-950/80 border border-slate-800/80 rounded-md p-2.5 text-xs space-y-1.5 font-mono tabular-nums">
            <div className="flex justify-between items-center gap-2 text-slate-400">
              <span className="truncate">{t.orderDesk.notional}:</span>
              <span className="text-slate-200 whitespace-nowrap shrink-0 text-right">{formatCurrency(notional)} VND</span>
            </div>
            <div className="flex justify-between items-center gap-2 text-slate-400">
              <span className="truncate">{t.orderDesk.estFee}:</span>
              <span className="text-slate-200 whitespace-nowrap shrink-0 text-right">{formatCurrency(estimatedFee)} VND</span>
            </div>
            <div className="flex justify-between items-center gap-2 font-semibold pt-1 border-t border-slate-800/80 text-white">
              <span className="truncate">{side === 'BUY' ? t.orderDesk.requiredPower : t.orderDesk.requiredShares}:</span>
              <span className={`whitespace-nowrap shrink-0 text-right ${side === 'BUY' ? 'text-emerald-400' : 'text-rose-400'}`}>
                {side === 'BUY' ? `${formatCurrency(requiredFunds)} VND` : `${formatNumber(quantity)} shares`}
              </span>
            </div>
            <div className="flex justify-between items-center gap-2 text-[11px] text-slate-500 pt-0.5">
              <span className="truncate">{side === 'BUY' ? t.orderDesk.availableCash : t.orderDesk.availableShares}:</span>
              <span className={`whitespace-nowrap shrink-0 text-right ${hasSufficientFunds ? 'text-slate-300' : 'text-rose-400 font-bold'}`}>
                {side === 'BUY' ? `${formatCurrency(availableCash)} VND` : `${formatNumber(availableHolding)} shares`}
              </span>
            </div>
          </div>

          {/* Zero Overdraft Warning if insufficient funds */}
          {!hasSufficientFunds && (
            <div className="bg-rose-950/40 border border-rose-800/80 p-2 rounded text-xs text-rose-300 flex items-center gap-2">
              <ShieldAlert className="w-4 h-4 shrink-0 text-rose-400" />
              <span>{t.orderDesk.overdraftGuaranteed}</span>
            </div>
          )}

          {/* Submit Order Button */}
          <button
            type="submit"
            disabled={!hasSufficientFunds}
            className={`w-full py-2.5 px-4 font-bold text-xs rounded-md uppercase tracking-wider transition-all shadow-sm ${
              side === 'BUY'
                ? 'bg-emerald-500 hover:bg-emerald-400 text-slate-950 disabled:bg-emerald-950 disabled:text-emerald-700'
                : 'bg-rose-500 hover:bg-rose-400 text-white disabled:bg-rose-950 disabled:text-rose-700'
            }`}
          >
            {side === 'BUY' ? `${t.orderDesk.submitBuy} ${symbol}` : `${t.orderDesk.submitSell} ${symbol}`}
          </button>
        </form>
      </div>

      {/* Execution Feedback Banner */}
      {statusMsg && (
        <div
          className={`mt-3 p-2.5 rounded-md text-xs flex items-center gap-2 ${
            statusMsg.type === 'success'
              ? 'bg-emerald-950/60 border border-emerald-800 text-emerald-300'
              : 'bg-rose-950/60 border border-rose-800 text-rose-300'
          }`}
        >
          {statusMsg.type === 'success' ? (
            <CheckCircle2 className="w-4 h-4 shrink-0 text-emerald-400" />
          ) : (
            <ShieldAlert className="w-4 h-4 shrink-0 text-rose-400" />
          )}
          <span className="font-mono">{statusMsg.text}</span>
        </div>
      )}
    </div>
  );
};
