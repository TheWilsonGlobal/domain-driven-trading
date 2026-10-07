import React from 'react';
import { MarketDepth } from '../engine/types';
import { useLanguage } from '../i18n/LanguageContext';

interface OrderBookDepthProps {
  depth: MarketDepth;
  onPriceSelect?: (price: number, side: 'BUY' | 'SELL') => void;
}

export const OrderBookDepth: React.FC<OrderBookDepthProps> = ({ depth, onPriceSelect }) => {
  const { t, formatCurrency, formatNumber } = useLanguage();

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-3 sm:p-4 flex flex-col h-full">
      <div className="flex items-center justify-between pb-3 border-b border-slate-800/80 mb-2">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-white tracking-wide">
            {depth.symbol} {t.orderBook.title}
          </span>
          <span className="text-[11px] font-mono text-slate-500">
            {t.orderBook.seq}{depth.sequence}
          </span>
        </div>
        <div className="text-right">
          <span className="text-[11px] text-slate-400">{t.orderBook.spread}: </span>
          <span className="text-xs font-mono font-medium text-amber-400">
            {formatCurrency(depth.spread)} VND ({depth.spreadPercent}%)
          </span>
        </div>
      </div>

      {/* Column Headers */}
      <div className="grid grid-cols-3 text-[11px] text-slate-400 py-1 px-2 font-medium tracking-wider border-b border-slate-800/40">
        <div>{t.orderBook.priceHeader}</div>
        <div className="text-right">{t.orderBook.qtyHeader}</div>
        <div className="text-right">{t.orderBook.ordersHeader}</div>
      </div>

      <div className="flex-1 flex flex-col justify-between py-1 min-h-[380px]">
        {/* Asks (Sells) - Inverted list so lowest ask sits at bottom next to spread */}
        <div className="flex flex-col-reverse justify-start space-y-reverse space-y-0.5">
          {depth.asks.slice(0, 7).map((ask, idx) => (
            <div
              key={`ask-${ask.price}-${idx}`}
              onClick={() => onPriceSelect && onPriceSelect(ask.price, 'BUY')}
              title={t.orderBook.clickToFill}
              className="grid grid-cols-3 text-xs py-1 px-2 rounded cursor-pointer relative hover:bg-rose-950/40 transition-colors group font-mono tabular-nums"
            >
              {/* Depth bar fill from right */}
              <div
                className="absolute top-0 right-0 bottom-0 bg-rose-500/15 pointer-events-none rounded-r"
                style={{ width: `${ask.depthPercent || 15}%` }}
              />
              <div className="text-rose-400 font-semibold relative z-10">
                {formatCurrency(ask.price)}
              </div>
              <div className="text-right text-slate-200 relative z-10">
                {formatNumber(ask.totalQuantity)}
              </div>
              <div className="text-right text-slate-500 relative z-10 group-hover:text-slate-300">
                {formatNumber(ask.orderCount)}
              </div>
            </div>
          ))}
          {depth.asks.length === 0 && (
            <div className="text-xs text-slate-600 text-center py-4">No resting asks</div>
          )}
        </div>

        {/* Spread Divider Bar */}
        <div className="my-2 py-1.5 px-3 bg-slate-950/80 border-y border-slate-800/80 flex items-center justify-between text-xs">
          <div className="flex items-center gap-2">
            <span className="text-slate-400 text-[11px]">Best Bid:</span>
            <span className="font-mono text-emerald-400 font-medium">
              {depth.bestBid ? formatCurrency(depth.bestBid) : '—'}
            </span>
          </div>
          <div className="text-slate-500 font-mono text-[11px]">
            {depth.spread ? `▲▼ ${formatCurrency(depth.spread)} VND` : 'No Spread'}
          </div>
          <div className="flex items-center gap-2">
            <span className="text-slate-400 text-[11px]">Best Ask:</span>
            <span className="font-mono text-rose-400 font-medium">
              {depth.bestAsk ? formatCurrency(depth.bestAsk) : '—'}
            </span>
          </div>
        </div>

        {/* Bids (Buys) - Highest bid sits at top next to spread */}
        <div className="flex flex-col space-y-0.5">
          {depth.bids.slice(0, 7).map((bid, idx) => (
            <div
              key={`bid-${bid.price}-${idx}`}
              onClick={() => onPriceSelect && onPriceSelect(bid.price, 'SELL')}
              title={t.orderBook.clickToFill}
              className="grid grid-cols-3 text-xs py-1 px-2 rounded cursor-pointer relative hover:bg-emerald-950/40 transition-colors group font-mono tabular-nums"
            >
              {/* Depth bar fill from right */}
              <div
                className="absolute top-0 right-0 bottom-0 bg-emerald-500/15 pointer-events-none rounded-r"
                style={{ width: `${bid.depthPercent || 15}%` }}
              />
              <div className="text-emerald-400 font-semibold relative z-10">
                {formatCurrency(bid.price)}
              </div>
              <div className="text-right text-slate-200 relative z-10">
                {formatNumber(bid.totalQuantity)}
              </div>
              <div className="text-right text-slate-500 relative z-10 group-hover:text-slate-300">
                {formatNumber(bid.orderCount)}
              </div>
            </div>
          ))}
          {depth.bids.length === 0 && (
            <div className="text-xs text-slate-600 text-center py-4">No resting bids</div>
          )}
        </div>
      </div>
    </div>
  );
};
