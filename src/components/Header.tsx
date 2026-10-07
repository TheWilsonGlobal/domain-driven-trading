import React from 'react';
import { Account } from '../engine/types';
import { RefreshCw, ShieldCheck, Zap } from 'lucide-react';

interface HeaderProps {
  activeTab: string;
  setActiveTab: (tab: string) => void;
  accounts: Account[];
  selectedAccountId: string;
  onSelectAccount: (id: string) => void;
  onReset: () => void;
}

export const Header: React.FC<HeaderProps> = ({
  activeTab,
  setActiveTab,
  accounts,
  selectedAccountId,
  onSelectAccount,
  onReset,
}) => {
  const tabs = [
    { id: 'terminal', label: 'Trading Terminal' },
    { id: 'depth', label: 'L2 Order Book' },
    { id: 'ledger', label: 'Ledger & Sức Mua' },
    { id: 'kafka', label: 'Kafka Event Log' },
    { id: 'architecture', label: 'Go Architecture' },
    { id: 'benchmark', label: 'Stress & Invariants' },
  ];

  return (
    <header className="border-b border-slate-800/80 bg-slate-950/90 backdrop-blur sticky top-0 z-40">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 h-14 flex items-center justify-between gap-4">
        {/* Zone 1: Single text element wordmark */}
        <div className="flex items-center gap-3">
          <a
            href="#"
            onClick={(e) => {
              e.preventDefault();
              setActiveTab('terminal');
            }}
            className="text-base font-bold tracking-tight text-white flex items-center gap-2 hover:text-emerald-400 transition-colors whitespace-nowrap"
          >
            <span className="w-2.5 h-2.5 rounded-full bg-emerald-500 animate-pulse"></span>
            <span>Vortex Trading Engine</span>
          </a>
          <span className="hidden lg:inline text-xs font-mono text-slate-500 bg-slate-900 px-2 py-0.5 rounded border border-slate-800">
            Go 1.22 DDD Core
          </span>
        </div>

        {/* Zone 2: 4-6 clean text navigation links */}
        <nav className="hidden md:flex items-center gap-1 sm:gap-2">
          {tabs.map((tab) => {
            const isActive = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`px-3 py-1.5 text-xs font-medium rounded-md transition-colors whitespace-nowrap ${
                  isActive
                    ? 'bg-slate-800 text-emerald-400 border border-slate-700/80'
                    : 'text-slate-400 hover:text-slate-200 hover:bg-slate-900/60'
                }`}
              >
                {tab.label}
              </button>
            );
          })}
        </nav>

        {/* Zone 3: 1-2 primary actions */}
        <div className="flex items-center gap-2 shrink-0">
          <div className="flex items-center gap-1.5 bg-slate-900/90 border border-slate-800 px-2.5 py-1 rounded-md text-xs">
            <span className="text-slate-400 hidden sm:inline">Account:</span>
            <select
              value={selectedAccountId}
              onChange={(e) => onSelectAccount(e.target.value)}
              aria-label="Select active trading account"
              className="bg-transparent text-emerald-300 font-mono text-xs focus:outline-none cursor-pointer"
            >
              {accounts.map((acc) => (
                <option key={acc.id} value={acc.id} className="bg-slate-900 text-slate-200">
                  {acc.id} · {acc.name.split(' ')[0]}
                </option>
              ))}
            </select>
          </div>

          <button
            onClick={onReset}
            title="Reset engine state and reseed market liquidity"
            className="p-1.5 text-slate-400 hover:text-slate-100 hover:bg-slate-900 rounded-md border border-slate-800 transition-colors"
          >
            <RefreshCw className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </header>
  );
};
