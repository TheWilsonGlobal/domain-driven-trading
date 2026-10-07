import React from 'react';
import { TradeMatch } from '../engine/types';

interface TradeTapeProps {
  trades: TradeMatch[];
  selectedSymbol?: string;
}

export const TradeTape: React.FC<TradeTapeProps> = ({ trades, selectedSymbol }) => {
  const filtered = selectedSymbol
    ? trades.filter((t) => t.symbol === selectedSymbol)
    : trades;

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-3 sm:p-4 flex flex-col h-full">
      <div className="flex items-center justify-between pb-3 border-b border-slate-800/80 mb-2">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-white tracking-wide">
            Matched Execution Tape
          </span>
          <span className="text-[11px] font-mono text-emerald-400 bg-emerald-950/60 border border-emerald-800/80 px-1.5 py-0.5 rounded">
            Live Matches
          </span>
        </div>
        <span className="text-[11px] font-mono text-slate-500">
          {filtered.length} executed
        </span>
      </div>

      {/* Column Headers */}
      <div className="grid grid-cols-4 text-[11px] text-slate-400 py-1 px-2 font-medium tracking-wider border-b border-slate-800/40">
        <div>TIME / ID</div>
        <div className="text-right">PRICE (VND)</div>
        <div className="text-right">QTY</div>
        <div className="text-right">MAKER / TAKER</div>
      </div>

      <div className="flex-1 overflow-y-auto space-y-1 py-1 max-h-[380px] font-mono tabular-nums">
        {filtered.slice(0, 30).map((t) => {
          const timeStr = new Date(t.timestamp).toLocaleTimeString('vi-VN', {
            hour12: false,
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
          });

          return (
            <div
              key={t.tradeId}
              className="grid grid-cols-4 text-xs py-1 px-2 rounded hover:bg-slate-800/40 transition-colors border-b border-slate-900/40"
            >
              <div className="flex flex-col">
                <span className="text-slate-300">{timeStr}</span>
                <span className="text-[10px] text-slate-500 truncate">{t.tradeId.substring(4, 14)}</span>
              </div>
              <div
                className={`text-right font-semibold ${
                  t.takerSide === 'BUY' ? 'text-emerald-400' : 'text-rose-400'
                }`}
              >
                {t.price.toLocaleString()}
              </div>
              <div className="text-right text-slate-200">
                {t.quantity.toLocaleString()}
              </div>
              <div className="text-right flex flex-col text-[10px] text-slate-400 truncate">
                <span className="text-slate-300">{t.symbol}</span>
                <span className="text-slate-500 truncate">{t.makerAccountId.substring(4)}/{t.takerAccountId.substring(4)}</span>
              </div>
            </div>
          );
        })}

        {filtered.length === 0 && (
          <div className="text-xs text-slate-500 text-center py-10">
            No trade executions yet. Place a matching Limit/Market order to trigger sub-millisecond execution.
          </div>
        )}
      </div>
    </div>
  );
};
