import React from 'react';
import { Account, LedgerTransaction, Order } from '../engine/types';
import { CheckCircle, ShieldCheck } from 'lucide-react';
import { useLanguage } from '../i18n/LanguageContext';

interface AccountRiskLedgerProps {
  accounts: Account[];
  selectedAccountId: string;
  onSelectAccount: (id: string) => void;
  activeOrders: Order[];
  onCancelOrder: (orderId: string) => void;
  ledgerTransactions: LedgerTransaction[];
}

export const AccountRiskLedger: React.FC<AccountRiskLedgerProps> = ({
  accounts,
  selectedAccountId,
  onSelectAccount,
  activeOrders,
  onCancelOrder,
  ledgerTransactions,
}) => {
  const { t, formatCurrency, formatNumber } = useLanguage();
  const currentAcc = accounts.find((a) => a.id === selectedAccountId) || accounts[0];
  const availableCash = Math.max(0, currentAcc.cashBalance - currentAcc.lockedCash);

  const userActiveOrders = activeOrders.filter((o) => o.accountId === selectedAccountId);

  return (
    <div className="space-y-6">
      {/* 1. Account Risk & Purchasing Power (Sức Mua) Overview */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-slate-800/80 gap-3">
          <div>
            <div className="flex items-center gap-2">
              <h2 className="text-base font-bold text-white tracking-wide">
                {t.ledger.title}
              </h2>
              <span className="text-xs font-mono text-emerald-400 bg-emerald-950/60 border border-emerald-800 px-2 py-0.5 rounded flex items-center gap-1">
                <ShieldCheck className="w-3.5 h-3.5" />
                {t.ledger.zeroOverdraft}
              </span>
            </div>
            <p className="text-xs text-slate-400 mt-1">
              {t.ledger.subtitle}
            </p>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-xs text-slate-400">{t.common.account}</span>
            <select
              value={selectedAccountId}
              onChange={(e) => onSelectAccount(e.target.value)}
              aria-label="Select account to view details"
              className="bg-slate-950 border border-slate-800 px-3 py-1.5 rounded-md text-xs font-mono text-emerald-300 focus:outline-none"
            >
              {accounts.map((a) => (
                <option key={a.id} value={a.id} className="bg-slate-900 text-slate-200">
                  {a.id} ({a.name})
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* Financial Metrics Cards */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3 mt-4 font-mono tabular-nums">
          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              {t.ledger.availableCash}
            </span>
            <div className="text-lg font-bold text-emerald-400 mt-1">
              {formatCurrency(availableCash)} <span className="text-xs text-slate-500 font-normal">VND</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Cash Balance minus locked funds
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              {t.ledger.totalCash}
            </span>
            <div className="text-lg font-bold text-slate-100 mt-1">
              {formatCurrency(currentAcc.cashBalance)} <span className="text-xs text-slate-500 font-normal">VND</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Unadjusted cash in ledger
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              {t.ledger.lockedCash}
            </span>
            <div className="text-lg font-bold text-amber-400 mt-1">
              {formatCurrency(currentAcc.lockedCash)} <span className="text-xs text-slate-500 font-normal">VND</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Held for open BUY limit orders
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              {t.ledger.holdingsTitle}
            </span>
            <div className="text-xs text-slate-200 mt-1 flex flex-wrap gap-2">
              {Object.entries(currentAcc.holdings).map(([sym, qty]) => {
                const locked = currentAcc.lockedHoldings[sym] || 0;
                return (
                  <span key={sym} className="bg-slate-900 border border-slate-800 px-1.5 py-0.5 rounded text-[11px]">
                    <strong className="text-emerald-400">{sym}:</strong> {formatNumber(qty - locked)}
                    {locked > 0 && <span className="text-amber-400 text-[10px]"> (🔒{formatNumber(locked)})</span>}
                  </span>
                );
              })}
            </div>
          </div>
        </div>
      </div>

      {/* 2. Active Resting Orders & Instant Cancellation Desk */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
        <div className="flex items-center justify-between pb-3 border-b border-slate-800/80 mb-3">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold text-white tracking-wide">
              {t.ledger.activeOrdersTitle} ({userActiveOrders.length})
            </h3>
            <span className="text-[11px] font-mono text-slate-500">
              In-Memory Order Book Queue
            </span>
          </div>
          <span className="text-[11px] text-slate-400">
            Canceling instantly releases reserved cash / stock locks
          </span>
        </div>

        {userActiveOrders.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs font-mono tabular-nums">
              <thead>
                <tr className="border-b border-slate-800/60 text-slate-400 text-[11px]">
                  <th className="py-2 px-3">{t.ledger.orderId}</th>
                  <th className="py-2 px-3">{t.ledger.symbol}</th>
                  <th className="py-2 px-3">{t.ledger.side}</th>
                  <th className="py-2 px-3 text-right">{t.ledger.price}</th>
                  <th className="py-2 px-3 text-right">{t.ledger.quantity} / {t.ledger.filled}</th>
                  <th className="py-2 px-3">{t.ledger.status}</th>
                  <th className="py-2 px-3 text-right">{t.ledger.action}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/40">
                {userActiveOrders.map((ord) => (
                  <tr key={ord.id} className="hover:bg-slate-800/30">
                    <td className="py-2 px-3 text-slate-300 font-semibold">{ord.id}</td>
                    <td className="py-2 px-3 text-white font-bold">{ord.symbol}</td>
                    <td className="py-2 px-3">
                      <span
                        className={`px-1.5 py-0.5 rounded text-[10px] font-bold ${
                          ord.side === 'BUY'
                            ? 'bg-emerald-950 border border-emerald-800 text-emerald-400'
                            : 'bg-rose-950 border border-rose-800 text-rose-400'
                        }`}
                      >
                        {ord.side === 'BUY' ? t.orderDesk.sideBuy : t.orderDesk.sideSell}
                      </span>
                    </td>
                    <td className="py-2 px-3 text-right text-slate-100">{formatCurrency(ord.price)}</td>
                    <td className="py-2 px-3 text-right text-slate-300">
                      {formatNumber(ord.remainingQuantity)} / {formatNumber(ord.quantity)}
                    </td>
                    <td className="py-2 px-3">
                      <span className="text-amber-400 text-[11px]">{ord.status}</span>
                    </td>
                    <td className="py-2 px-3 text-right">
                      <button
                        onClick={() => onCancelOrder(ord.id)}
                        className="px-2 py-1 bg-rose-950/60 hover:bg-rose-900 border border-rose-800 text-rose-300 rounded text-[11px] transition-colors"
                      >
                        {t.ledger.cancel}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="text-xs text-slate-500 py-6 text-center font-mono">
            {t.ledger.noActiveOrders}
          </div>
        )}
      </div>

      {/* 3. Double-Entry General Ledger Journal Table */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
        <div className="flex items-center justify-between pb-3 border-b border-slate-800/80 mb-3">
          <div>
            <h3 className="text-sm font-semibold text-white tracking-wide">
              {t.ledger.journalTitle}
            </h3>
            <p className="text-xs text-slate-400 mt-0.5">
              {t.ledger.journalDesc}
            </p>
          </div>
          <span className="text-[11px] font-mono text-emerald-400 bg-emerald-950 border border-emerald-800 px-2 py-0.5 rounded">
            Balanced & Audited
          </span>
        </div>

        {ledgerTransactions.length > 0 ? (
          <div className="space-y-4">
            {ledgerTransactions.slice(0, 10).map((tx) => (
              <div
                key={tx.txId}
                className="bg-slate-950/80 border border-slate-800/80 rounded-md p-3 text-xs font-mono"
              >
                <div className="flex items-center justify-between pb-2 border-b border-slate-800/60 mb-2">
                  <div className="flex items-center gap-2">
                    <span className="font-bold text-slate-200">{tx.txId}</span>
                    <span className="text-slate-500 text-[11px]">Trade: {tx.tradeId}</span>
                  </div>
                  <div className="flex items-center gap-1.5 text-emerald-400 text-[11px]">
                    <CheckCircle className="w-3.5 h-3.5" />
                    <span>{t.ledger.balancedProof}</span>
                  </div>
                </div>

                <div className="overflow-x-auto">
                  <table className="w-full text-left text-[11px] tabular-nums">
                    <thead>
                      <tr className="text-slate-500 border-b border-slate-900">
                        <th className="pb-1">{t.ledger.account}</th>
                        <th className="pb-1">{t.ledger.entryType}</th>
                        <th className="pb-1">ASSET</th>
                        <th className="pb-1 text-right">{t.ledger.debit} / {t.ledger.credit}</th>
                        <th className="pb-1 pl-3">{t.ledger.description}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-900/50">
                      {tx.entries.map((entry) => (
                        <tr key={entry.entryId} className="hover:bg-slate-900/40">
                          <td className="py-1 text-slate-300 font-semibold">{entry.accountId}</td>
                          <td className="py-1">
                            <span
                              className={`px-1 rounded text-[10px] font-bold ${
                                entry.type === 'DEBIT'
                                  ? 'text-cyan-400 bg-cyan-950/60'
                                  : 'text-amber-400 bg-amber-950/60'
                              }`}
                            >
                              {entry.type === 'DEBIT' ? t.ledger.debit : t.ledger.credit}
                            </span>
                          </td>
                          <td className="py-1 text-slate-400">
                            {entry.assetType === 'STOCK_SHARE' ? `${entry.symbol || 'STOCK'}` : 'VND CASH'}
                          </td>
                          <td className="py-1 text-right text-slate-100 font-bold">
                            {formatNumber(entry.amount)} {entry.assetType === 'STOCK_SHARE' ? 'shares' : '₫'}
                          </td>
                          <td className="py-1 pl-3 text-slate-400 truncate max-w-xs">
                            {entry.description}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-xs text-slate-500 py-8 text-center font-mono">
            No ledger vouchers generated yet. Match trades to observe double-entry accounting records.
          </div>
        )}
      </div>
    </div>
  );
};
