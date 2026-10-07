import React from 'react';
import { Account } from '../engine/types';
import { Globe, RefreshCw } from 'lucide-react';
import { useLanguage } from '../i18n/LanguageContext';

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
  const { language, setLanguage, t } = useLanguage();

  const tabs = [
    { id: 'terminal', label: t.header.tabs.terminal },
    { id: 'depth', label: t.header.tabs.depth },
    { id: 'ledger', label: t.header.tabs.ledger },
    { id: 'kafka', label: t.header.tabs.kafka },
    { id: 'architecture', label: t.header.tabs.architecture },
    { id: 'benchmark', label: t.header.tabs.benchmark },
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
            <span>{t.header.title}</span>
          </a>
        </div>

        {/* Zone 2: Navigation links */}
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

        {/* Zone 3: Actions & Language Switcher */}
        <div className="flex items-center gap-2 shrink-0">
          {/* Bilingual Switcher (ENG default vs VNI) */}
          <div className="flex items-center bg-slate-900/90 border border-slate-800 rounded-md p-0.5 text-xs">
            <button
              onClick={() => setLanguage('en')}
              title="English (Default)"
              className={`px-2 py-0.5 rounded font-mono text-[11px] font-semibold transition-colors ${
                language === 'en'
                  ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              EN
            </button>
            <button
              onClick={() => setLanguage('vi')}
              title="Tiếng Việt (VNI)"
              className={`px-2 py-0.5 rounded font-mono text-[11px] font-semibold transition-colors ${
                language === 'vi'
                  ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              VI
            </button>
          </div>

          <div className="flex items-center gap-1.5 bg-slate-900/90 border border-slate-800 px-2.5 py-1 rounded-md text-xs">
            <span className="text-slate-400 hidden sm:inline">{t.common.account}</span>
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
            title={t.common.resetTooltip}
            className="p-1.5 text-slate-400 hover:text-slate-100 hover:bg-slate-900 rounded-md border border-slate-800 transition-colors"
          >
            <RefreshCw className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </header>
  );
};
