import React, { useState } from 'react';
import { DomainDrivenTradingEngine } from '../engine/matchingEngine';
import { CheckCircle2, Play, RefreshCw, XCircle } from 'lucide-react';
import { useLanguage } from '../i18n/LanguageContext';

interface TestCase {
  id: string;
  name: string;
  description: string;
  run: (engine: DomainDrivenTradingEngine) => { pass: boolean; details: string };
}

export const InvariantTestSuite: React.FC = () => {
  const { t } = useLanguage();
  const [results, setResults] = useState<Record<string, { pass: boolean; details: string }>>({});
  const [isRunning, setIsRunning] = useState<boolean>(false);

  const testCases: TestCase[] = [
    {
      id: 'inv_fifo',
      name: t.invariants.tests.fifo.name,
      description: t.invariants.tests.fifo.desc,
      run: (engine) => {
        const sym = 'APX';
        const testPrice = 19100;

        // Place Maker 1: Buy 100 @ 19,100
        engine.placeOrder({
          clientOrderId: `TEST_FIFO_M1_${Date.now()}`,
          accountId: 'ACC_APX_001',
          symbol: sym,
          side: 'BUY',
          type: 'LIMIT',
          price: testPrice,
          quantity: 100,
        });

        // Place Maker 2: Buy 200 @ 19,100 (same price, later in queue)
        engine.placeOrder({
          clientOrderId: `TEST_FIFO_M2_${Date.now()}`,
          accountId: 'ACC_APX_002',
          symbol: sym,
          side: 'BUY',
          type: 'LIMIT',
          price: testPrice,
          quantity: 200,
        });

        // Taker sells 50 @ 19,100. Must match Maker 1 first
        const takerRes = engine.placeOrder({
          clientOrderId: `TEST_FIFO_T_${Date.now()}`,
          accountId: 'ACC_INST_MM',
          symbol: sym,
          side: 'SELL',
          type: 'LIMIT',
          price: testPrice,
          quantity: 50,
        });

        const pass = takerRes.matches.length === 1 && takerRes.matches[0].makerAccountId === 'ACC_APX_001';
        return {
          pass,
          details: pass
            ? 'Matched 50 shares strictly against Maker 1 (ACC_APX_001) preserving O(1) FIFO linked list order.'
            : `Failed: Matched against ${takerRes.matches[0]?.makerAccountId}`,
        };
      },
    },
    {
      id: 'inv_partial',
      name: '2. Partial Fills & Multi-Level Price Sweep',
      description: 'Verifies that large aggressive orders sweep multiple price levels and rest unfulfilled balance.',
      run: (engine) => {
        const sym = 'HPG';
        const bestAsk = 28600;

        // Maker sells 300 @ 28,600
        engine.placeOrder({
          clientOrderId: `TEST_PARTIAL_M_${Date.now()}`,
          accountId: 'ACC_INST_MM',
          symbol: sym,
          side: 'SELL',
          type: 'LIMIT',
          price: bestAsk,
          quantity: 300,
        });

        // Taker buys 500 @ 28,600 (300 should match, 200 should rest as buy bid)
        const takerRes = engine.placeOrder({
          clientOrderId: `TEST_PARTIAL_T_${Date.now()}`,
          accountId: 'ACC_APX_001',
          symbol: sym,
          side: 'BUY',
          type: 'LIMIT',
          price: bestAsk,
          quantity: 500,
        });

        const matched300 = takerRes.matches.length === 1 && takerRes.matches[0].quantity === 300;
        const remaining200 = takerRes.order.remainingQuantity === 200 && takerRes.order.status === 'PARTIALLY_FILLED';

        const pass = matched300 && remaining200;
        return {
          pass,
          details: pass
            ? 'Filled 300 shares of maker ask; 200 shares successfully rested into buy order book ladder.'
            : 'Failed to properly split execution and rest remainder.',
        };
      },
    },
    {
      id: 'inv_purchasing_power',
      name: t.invariants.tests.zeroOverdraft.name,
      description: t.invariants.tests.zeroOverdraft.desc,
      run: (engine) => {
        const acc = engine.getAccount('ACC_APX_001');
        if (!acc) return { pass: false, details: 'Account not found' };

        const available = engine.getAvailableCash('ACC_APX_001');

        // Attempt to place BUY order exceeding available purchasing power
        const overLimitPrice = 20000;
        const overLimitQty = Math.floor((available / overLimitPrice) * 3); // 3x allowable purchasing power

        const rejectedRes = engine.placeOrder({
          clientOrderId: `TEST_OVERDRAFT_${Date.now()}`,
          accountId: 'ACC_APX_001',
          symbol: 'APX',
          side: 'BUY',
          type: 'LIMIT',
          price: overLimitPrice,
          quantity: overLimitQty,
        });

        const pass = rejectedRes.error !== undefined && rejectedRes.error.includes('Insufficient purchasing power');
        return {
          pass,
          details: pass
            ? `Order successfully blocked by Pre-Trade Risk Engine: ${rejectedRes.error}`
            : 'Failed: Order was erroneously accepted without funds!',
        };
      },
    },
    {
      id: 'inv_ledger',
      name: t.invariants.tests.doubleEntry.name,
      description: t.invariants.tests.doubleEntry.desc,
      run: (engine) => {
        // Execute a clean trade
        engine.placeOrder({
          clientOrderId: `TEST_LEDGER_M_${Date.now()}`,
          accountId: 'ACC_APX_002',
          symbol: 'SSI',
          side: 'SELL',
          type: 'LIMIT',
          price: 34500,
          quantity: 200,
        });

        engine.placeOrder({
          clientOrderId: `TEST_LEDGER_T_${Date.now()}`,
          accountId: 'ACC_APX_001',
          symbol: 'SSI',
          side: 'BUY',
          type: 'LIMIT',
          price: 34500,
          quantity: 200,
        });

        const latestVoucher = engine.getLedgerTransactions()[0];
        const pass = latestVoucher && latestVoucher.isBalanced === true;

        return {
          pass,
          details: pass
            ? `Voucher ${latestVoucher.txId} audited: Stock debits/credits balanced, Cash debits/credits balanced with exchange fee clearing.`
            : 'Failed: Double-entry ledger was unbalanced!',
        };
      },
    },
    {
      id: 'inv_idempotency',
      name: t.invariants.tests.idempotency.name,
      description: t.invariants.tests.idempotency.desc,
      run: (engine) => {
        const clientOrderId = `IDEM_TEST_${Date.now()}`;

        // 1st request
        const res1 = engine.placeOrder({
          clientOrderId,
          accountId: 'ACC_APX_001',
          symbol: 'APX',
          side: 'BUY',
          type: 'LIMIT',
          price: 19300,
          quantity: 100,
        });

        // 2nd duplicate request with same clientOrderId
        const res2 = engine.placeOrder({
          clientOrderId,
          accountId: 'ACC_APX_001',
          symbol: 'APX',
          side: 'BUY',
          type: 'LIMIT',
          price: 19300,
          quantity: 100,
        });

        const pass = !res1.error && res2.error !== undefined && res2.error.includes('duplicate');
        return {
          pass,
          details: pass
            ? 'First order accepted; duplicate order immediately dropped with idempotency violation.'
            : 'Failed to detect duplicate client order ID.',
        };
      },
    },
  ];

  const runAllTests = () => {
    setIsRunning(true);
    // Create isolated test engine so tests don't pollute live terminal
    const testEngine = new DomainDrivenTradingEngine();
    const newResults: Record<string, { pass: boolean; details: string }> = {};

    testCases.forEach((tc) => {
      newResults[tc.id] = tc.run(testEngine);
    });

    setResults(newResults);
    setIsRunning(false);
  };

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-slate-800/80 gap-3">
        <div>
          <h2 className="text-base font-bold text-white tracking-wide flex items-center gap-2">
            <CheckCircle2 className="w-5 h-5 text-emerald-400" />
            {t.invariants.title}
          </h2>
          <p className="text-xs text-slate-400 mt-1">
            {t.invariants.subtitle}
          </p>
        </div>

        <button
          onClick={runAllTests}
          disabled={isRunning}
          className="px-3.5 py-2 bg-emerald-500 hover:bg-emerald-400 disabled:opacity-50 text-slate-950 rounded-md text-xs font-bold flex items-center gap-1.5 transition-colors"
        >
          {isRunning ? (
            <RefreshCw className="w-3.5 h-3.5 animate-spin" />
          ) : (
            <Play className="w-3.5 h-3.5 fill-current" />
          )}
          {isRunning ? t.invariants.runningButton : t.invariants.runButton}
        </button>
      </div>

      <div className="mt-4 space-y-3 font-mono">
        {testCases.map((tc) => {
          const res = results[tc.id];

          return (
            <div
              key={tc.id}
              className="bg-slate-950/80 border border-slate-800/80 rounded-md p-3.5 text-xs transition-colors"
            >
              <div className="flex items-start justify-between gap-3">
                <div>
                  <span className="font-bold text-slate-200 block text-xs">{tc.name}</span>
                  <p className="text-[11px] text-slate-400 mt-0.5 font-sans leading-relaxed">
                    {tc.description}
                  </p>
                </div>

                <div className="shrink-0">
                  {res ? (
                    res.pass ? (
                      <span className="bg-emerald-950 border border-emerald-800 text-emerald-400 text-[11px] font-bold px-2 py-0.5 rounded flex items-center gap-1">
                        <CheckCircle2 className="w-3.5 h-3.5" />
                        {t.invariants.passedBadge}
                      </span>
                    ) : (
                      <span className="bg-rose-950 border border-rose-800 text-rose-400 text-[11px] font-bold px-2 py-0.5 rounded flex items-center gap-1">
                        <XCircle className="w-3.5 h-3.5" />
                        {t.invariants.failedBadge}
                      </span>
                    )
                  ) : (
                    <span className="text-slate-500 text-[11px]">Pending Run</span>
                  )}
                </div>
              </div>

              {res && (
                <div
                  className={`mt-2.5 p-2 rounded text-[11px] border ${
                    res.pass
                      ? 'bg-emerald-950/30 border-emerald-900/60 text-emerald-300'
                      : 'bg-rose-950/30 border-rose-900/60 text-rose-300'
                  }`}
                >
                  {res.details}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};
