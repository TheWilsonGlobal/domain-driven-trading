import React, { useState, useEffect } from 'react';
import { engineInstance } from './engine/matchingEngine';
import { MarketDepth, OrderSide, OrderType, TradeMatch } from './engine/types';
import { Header } from './components/Header';
import { OrderBookDepth } from './components/OrderBookDepth';
import { OrderPlacementDesk } from './components/OrderPlacementDesk';
import { TradeTape } from './components/TradeTape';
import { AccountRiskLedger } from './components/AccountRiskLedger';
import { EventStreamViewer } from './components/EventStreamViewer';
import { ArchitectureViewer } from './components/ArchitectureViewer';
import { StressTestRunner } from './components/StressTestRunner';
import { InvariantTestSuite } from './components/InvariantTestSuite';
import { LanguageProvider, useLanguage } from './i18n/LanguageContext';

function TradingApp() {
  const { t, formatCurrency, formatNumber } = useLanguage();
  const [activeTab, setActiveTab] = useState<string>('terminal');
  const [selectedSymbol, setSelectedSymbol] = useState<string>('APX');
  const [selectedAccountId, setSelectedAccountId] = useState<string>('ACC_APX_001');

  // React state synchronized with engineInstance
  const [engineState, setEngineState] = useState<{
    depth: MarketDepth;
    trades: TradeMatch[];
    accounts: any[];
    activeOrders: any[];
    events: any[];
    ledger: any[];
  }>({
    depth: engineInstance.getMarketDepth('APX', 10),
    trades: engineInstance.getTradeHistory(),
    accounts: engineInstance.getAllAccounts(),
    activeOrders: engineInstance.getActiveOrders(),
    events: engineInstance.getEventLog(),
    ledger: engineInstance.getLedgerTransactions(),
  });

  const [prefillPrice, setPrefillPrice] = useState<number | undefined>(undefined);
  const [prefillSide, setPrefillSide] = useState<OrderSide | undefined>(undefined);

  // Subscribe to engine state changes
  useEffect(() => {
    const updateState = () => {
      setEngineState({
        depth: engineInstance.getMarketDepth(selectedSymbol, 10),
        trades: engineInstance.getTradeHistory(),
        accounts: engineInstance.getAllAccounts(),
        activeOrders: engineInstance.getActiveOrders(),
        events: engineInstance.getEventLog(),
        ledger: engineInstance.getLedgerTransactions(),
      });
    };

    updateState();
    const unsubscribe = engineInstance.subscribe(updateState);
    return () => unsubscribe();
  }, [selectedSymbol]);

  const handleResetEngine = () => {
    engineInstance.initDefaultState();
  };

  const handlePriceSelect = (price: number, side: OrderSide) => {
    setPrefillPrice(price);
    setPrefillSide(side);
  };

  const handlePlaceOrder = (params: {
    symbol: string;
    side: OrderSide;
    type: OrderType;
    price: number;
    quantity: number;
  }) => {
    const res = engineInstance.placeOrder({
      accountId: selectedAccountId,
      symbol: params.symbol,
      side: params.side,
      type: params.type,
      price: params.price,
      quantity: params.quantity,
    });

    if (res.error) {
      return { success: false, message: res.error };
    }

    const matchedQty = res.matches.reduce((sum, m) => sum + m.quantity, 0);
    let msg = `Order ${res.order.id} submitted.`;
    if (matchedQty > 0) {
      msg = `Matched ${matchedQty.toLocaleString()} shares across ${res.matches.length} trade(s).`;
    } else {
      msg = `Rested in order book at ${params.price.toLocaleString()} VND.`;
    }

    return {
      success: true,
      message: msg,
      latencyMicros: res.latencyMicros,
    };
  };

  const handleCancelOrder = (orderId: string) => {
    engineInstance.cancelOrder(orderId);
  };

  const activeAccount = engineState.accounts.find((a) => a.id === selectedAccountId) || engineState.accounts[0];

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans selection:bg-emerald-500/30 selection:text-emerald-300">
      {/* Top Bar Contract (3 zones) */}
      <Header
        activeTab={activeTab}
        setActiveTab={setActiveTab}
        accounts={engineState.accounts}
        selectedAccountId={selectedAccountId}
        onSelectAccount={setSelectedAccountId}
        onReset={handleResetEngine}
      />

      {/* Sub-Header Market Summary Bar */}
      <section aria-label="Market summary ticker" className="border-b border-slate-900 bg-slate-950/90 py-2 px-4 sm:px-6">
        <div className="max-w-7xl mx-auto flex flex-wrap items-center justify-between gap-3 text-xs font-mono">
          <div className="flex items-center gap-3">
            <span className="text-emerald-400 font-bold text-sm tracking-wide">
              {selectedSymbol} / VND
            </span>
            <span className="text-slate-500">|</span>
            <span className="text-slate-400">Best Bid:</span>
            <span className="text-emerald-400 font-medium tabular-nums whitespace-nowrap">
              {engineState.depth.bestBid ? `${formatCurrency(engineState.depth.bestBid)} VND` : '—'}
            </span>
            <span className="text-slate-400">Best Ask:</span>
            <span className="text-rose-400 font-medium tabular-nums whitespace-nowrap">
              {engineState.depth.bestAsk ? `${formatCurrency(engineState.depth.bestAsk)} VND` : '—'}
            </span>
            <span className="text-slate-400">{t.orderBook.spread}:</span>
            <span className="text-amber-400 font-medium tabular-nums whitespace-nowrap">
              {engineState.depth.spread ? `${formatCurrency(engineState.depth.spread)} VND` : '0 VND'}
            </span>
          </div>

          <div className="flex items-center gap-3 text-slate-400">
            <span className="flex items-center gap-1.5 text-emerald-400 whitespace-nowrap">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400"></span>
              FIFO Price-Time Active
            </span>
            <span className="text-slate-600">·</span>
            <span className="text-slate-400">{t.ledger.availableCash}:</span>
            <span className="text-emerald-300 font-bold tabular-nums whitespace-nowrap">
              {formatCurrency(Math.max(0, (activeAccount?.cashBalance || 0) - (activeAccount?.lockedCash || 0)))} VND
            </span>
          </div>
        </div>
      </section>

      {/* Main Content Viewport */}
      <main className="flex-1 max-w-7xl w-full mx-auto p-4 sm:p-6">
        {activeTab === 'terminal' && (
          <div className="space-y-6">
            <div className="grid grid-cols-1 lg:grid-cols-12 gap-5 items-start">
              {/* Left Column: L2 Order Book Ladder (4 cols) */}
              <div className="lg:col-span-4 h-full">
                <OrderBookDepth
                  depth={engineState.depth}
                  onPriceSelect={handlePriceSelect}
                />
              </div>

              {/* Middle Column: Order Placement Desk (4 cols) */}
              <div className="lg:col-span-4 h-full">
                <OrderPlacementDesk
                  account={activeAccount}
                  symbol={selectedSymbol}
                  onSymbolChange={setSelectedSymbol}
                  bestBid={engineState.depth.bestBid}
                  bestAsk={engineState.depth.bestAsk}
                  onPlaceOrder={handlePlaceOrder}
                  prefillPrice={prefillPrice}
                  prefillSide={prefillSide}
                />
              </div>

              {/* Right Column: Execution Tape (4 cols) */}
              <div className="lg:col-span-4 h-full">
                <TradeTape
                  trades={engineState.trades}
                  selectedSymbol={selectedSymbol}
                />
              </div>
            </div>

            {/* Bottom Section: Active Orders for current account */}
            <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4">
              <div className="flex items-center justify-between pb-3 border-b border-slate-800/80 mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-semibold text-white tracking-wide">
                    {t.ledger.activeOrdersTitle}
                  </span>
                  <span className="text-xs font-mono text-slate-400 bg-slate-900 px-2 py-0.5 rounded border border-slate-800">
                    {engineState.activeOrders.filter((o) => o.accountId === selectedAccountId).length} Active
                  </span>
                </div>
                <span className="text-[11px] text-slate-400">
                  {t.common.account} <strong className="text-emerald-400 font-mono">{selectedAccountId}</strong>
                </span>
              </div>

              {engineState.activeOrders.filter((o) => o.accountId === selectedAccountId).length > 0 ? (
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-xs font-mono tabular-nums">
                    <thead>
                      <tr className="border-b border-slate-800 text-slate-500 text-[11px]">
                        <th className="py-2 px-2">{t.ledger.orderId}</th>
                        <th className="py-2 px-2">{t.ledger.symbol}</th>
                        <th className="py-2 px-2">{t.ledger.side}</th>
                        <th className="py-2 px-2 text-right">{t.ledger.price}</th>
                        <th className="py-2 px-2 text-right">{t.ledger.quantity}</th>
                        <th className="py-2 px-2 text-right">{t.tradeTape.timeHeader}</th>
                        <th className="py-2 px-2 text-right">{t.ledger.action}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-800/50">
                      {engineState.activeOrders
                        .filter((o) => o.accountId === selectedAccountId)
                        .map((ord) => (
                          <tr key={ord.id} className="hover:bg-slate-800/30">
                            <td className="py-2 px-2 text-slate-300 font-semibold">{ord.id}</td>
                            <td className="py-2 px-2 text-white font-bold">{ord.symbol}</td>
                            <td className="py-2 px-2">
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
                            <td className="py-2 px-2 text-right text-slate-200">{formatCurrency(ord.price)}</td>
                            <td className="py-2 px-2 text-right text-slate-300">
                              {formatNumber(ord.remainingQuantity)} / {formatNumber(ord.quantity)}
                            </td>
                            <td className="py-2 px-2 text-right text-slate-500 text-[11px]">
                              {new Date(ord.createdAt).toLocaleTimeString()}
                            </td>
                            <td className="py-2 px-2 text-right">
                              <button
                                onClick={() => handleCancelOrder(ord.id)}
                                className="px-2 py-0.5 bg-rose-950/60 hover:bg-rose-900 border border-rose-800 text-rose-300 rounded text-[11px] transition-colors"
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
                <div className="text-xs text-slate-500 py-4 text-center font-mono">
                  {t.ledger.noActiveOrders}
                </div>
              )}
            </div>
          </div>
        )}

        {activeTab === 'depth' && (
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
            <div className="lg:col-span-8">
              <OrderBookDepth
                depth={engineState.depth}
                onPriceSelect={handlePriceSelect}
              />
            </div>
            <div className="lg:col-span-4">
              <TradeTape
                trades={engineState.trades}
                selectedSymbol={selectedSymbol}
              />
            </div>
          </div>
        )}

        {activeTab === 'ledger' && (
          <AccountRiskLedger
            accounts={engineState.accounts}
            selectedAccountId={selectedAccountId}
            onSelectAccount={setSelectedAccountId}
            activeOrders={engineState.activeOrders}
            onCancelOrder={handleCancelOrder}
            ledgerTransactions={engineState.ledger}
          />
        )}

        {activeTab === 'kafka' && (
          <EventStreamViewer events={engineState.events} />
        )}

        {activeTab === 'architecture' && (
          <ArchitectureViewer />
        )}

        {activeTab === 'benchmark' && (
          <div className="space-y-6">
            <StressTestRunner />
            <InvariantTestSuite />
          </div>
        )}
      </main>

      {/* Footer */}
      <footer className="border-t border-slate-900 bg-slate-950 py-4 px-6 text-xs text-slate-500 text-center font-mono">
        <div className="max-w-7xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-2">
          <span>{t.footer.title}</span>
          <span className="text-slate-600">{t.footer.features}</span>
        </div>
      </footer>
    </div>
  );
}

export default function App() {
  return (
    <LanguageProvider>
      <TradingApp />
    </LanguageProvider>
  );
}
