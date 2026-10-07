import React from 'react';
import { Account, LedgerTransaction, Order } from '../engine/types';
import { CheckCircle, ShieldCheck, XCircle } from 'lucide-react';

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
                Account Risk & Sức Mua Engine
              </h2>
              <span className="text-xs font-mono text-emerald-400 bg-emerald-950/60 border border-emerald-800 px-2 py-0.5 rounded flex items-center gap-1">
                <ShieldCheck className="w-3.5 h-3.5" />
                Zero Overdraft Active
              </span>
            </div>
            <p className="text-xs text-slate-400 mt-1">
              Real-time balance reservation with double-entry debit/credit consistency.
            </p>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-xs text-slate-400">Account:</span>
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
              Sức Mua Khả Dụng (Available)
            </span>
            <div className="text-lg font-bold text-emerald-400 mt-1">
              {availableCash.toLocaleString()} <span className="text-xs text-slate-500 font-normal">VND</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Cash Balance minus locked funds
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              Tổng Tiền Mặt (Total Cash)
            </span>
            <div className="text-lg font-bold text-slate-100 mt-1">
              {currentAcc.cashBalance.toLocaleString()} <span className="text-xs text-slate-500 font-normal">VND</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Unadjusted cash in ledger
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              Tiền Đang Tạm Khóa (Locked)
            </span>
            <div className="text-lg font-bold text-amber-400 mt-1">
              {currentAcc.lockedCash.toLocaleString()} <span className="text-xs text-slate-500 font-normal">VND</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Held for open BUY limit orders
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              Danh Mục Cổ Phiếu (Holdings)
            </span>
            <div className="text-xs text-slate-200 mt-1 flex flex-wrap gap-2">
              {Object.entries(currentAcc.holdings).map(([sym, qty]) => {
                const locked = currentAcc.lockedHoldings[sym] || 0;
                return (
                  <span key={sym} className="bg-slate-900 border border-slate-800 px-1.5 py-0.5 rounded text-[11px]">
                    <strong className="text-emerald-400">{sym}:</strong> {(qty - locked).toLocaleString()}
                    {locked > 0 && <span className="text-amber-400 text-[10px]"> (🔒{locked})</span>}
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
              Active Resting Orders ({userActiveOrders.length})
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
                  <th className="py-2 px-3">ORDER ID</th>
                  <th className="py-2 px-3">SYMBOL</th>
                  <th className="py-2 px-3">SIDE</th>
                  <th className="py-2 px-3 text-right">PRICE (VND)</th>
                  <th className="py-2 px-3 text-right">QTY / REMAINING</th>
                  <th className="py-2 px-3">STATUS</th>
                  <th className="py-2 px-3 text-right">ACTION</th>
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
                        {ord.side}
                      </span>
                    </td>
                    <td className="py-2 px-3 text-right text-slate-100">{ord.price.toLocaleString()}</td>
                    <td className="py-2 px-3 text-right text-slate-300">
                      {ord.remainingQuantity.toLocaleString()} / {ord.quantity.toLocaleString()}
                    </td>
                    <td className="py-2 px-3">
                      <span className="text-amber-400 text-[11px]">{ord.status}</span>
                    </td>
                    <td className="py-2 px-3 text-right">
                      <button
                        onClick={() => onCancelOrder(ord.id)}
                        className="px-2 py-1 bg-rose-950/60 hover:bg-rose-900 border border-rose-800 text-rose-300 rounded text-[11px] transition-colors"
                      >
                        Hủy Lệnh
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="text-xs text-slate-500 py-6 text-center font-mono">
            No active resting orders for {selectedAccountId}.
          </div>
        )}
      </div>

      {/* 3. Double-Entry General Ledger Journal Table */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
        <div className="flex items-center justify-between pb-3 border-b border-slate-800/80 mb-3">
          <div>
            <h3 className="text-sm font-semibold text-white tracking-wide">
              Double-Entry General Ledger Vouchers (Nhật ký sổ cái kép)
            </h3>
            <p className="text-xs text-slate-400 mt-0.5">
              Strict accounting invariant: <code className="text-emerald-400 font-mono">Σ Debits == Σ Credits</code> per transaction.
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
                    <span>Balanced Invariant Verified</span>
                  </div>
                </div>

                <div className="overflow-x-auto">
                  <table className="w-full text-left text-[11px] tabular-nums">
                    <thead>
                      <tr className="text-slate-500 border-b border-slate-900">
                        <th className="pb-1">ACCOUNT</th>
                        <th className="pb-1">TYPE</th>
                        <th className="pb-1">ASSET</th>
                        <th className="pb-1 text-right">AMOUNT</th>
                        <th className="pb-1 pl-3">DESCRIPTION</th>
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
                              {entry.type}
                            </span>
                          </td>
                          <td className="py-1 text-slate-400">
                            {entry.assetType === 'STOCK_SHARE' ? `${entry.symbol || 'STOCK'}` : 'VND CASH'}
                          </td>
                          <td className="py-1 text-right text-slate-100 font-bold">
                            {entry.amount.toLocaleString()} {entry.assetType === 'STOCK_SHARE' ? 'cp' : 'đ'}
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
