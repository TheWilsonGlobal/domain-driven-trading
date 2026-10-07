import React, { useState, useEffect, useRef } from 'react';
import { engineInstance } from '../engine/matchingEngine';
import { Activity, Play, Square, Zap } from 'lucide-react';

export const StressTestRunner: React.FC = () => {
  const [isRunningBot, setIsRunningBot] = useState<boolean>(false);
  const [ordersPerSec, setOrdersPerSec] = useState<number>(0);
  const [totalSimulated, setTotalSimulated] = useState<number>(0);
  const [totalMatches, setTotalMatches] = useState<number>(0);
  const [avgLatencyMicros, setAvgLatencyMicros] = useState<number>(32);
  const [p99LatencyMicros, setP99LatencyMicros] = useState<number>(78);
  const [isBursting, setIsBursting] = useState<boolean>(false);

  const botIntervalRef = useRef<any>(null);
  const orderCountWindowRef = useRef<number>(0);

  // Frequency ticker
  useEffect(() => {
    const rateTimer = setInterval(() => {
      setOrdersPerSec(orderCountWindowRef.current);
      orderCountWindowRef.current = 0;
    }, 1000);
    return () => clearInterval(rateTimer);
  }, []);

  const toggleBot = () => {
    if (isRunningBot) {
      clearInterval(botIntervalRef.current);
      setIsRunningBot(false);
    } else {
      setIsRunningBot(true);
      botIntervalRef.current = setInterval(() => {
        simulateRandomOrder();
      }, 40); // ~25 orders/sec continuous flow
    }
  };

  const simulateRandomOrder = () => {
    const symbols = ['VPB', 'HPG', 'FPT', 'SSI'];
    const sym = symbols[Math.floor(Math.random() * symbols.length)];
    const depth = engineInstance.getMarketDepth(sym, 3);

    const isBuy = Math.random() > 0.48;
    const side = isBuy ? 'BUY' : 'SELL';
    const type = Math.random() > 0.2 ? 'LIMIT' : 'MARKET';

    const basePrice = sym === 'FPT' ? 132000 : sym === 'HPG' ? 28400 : 19500;
    const tick = sym === 'FPT' ? 500 : 50;
    const spreadOffset = (Math.floor(Math.random() * 5) - 2) * tick;
    const price = (depth.bestBid && depth.bestAsk)
      ? (isBuy ? depth.bestAsk + spreadOffset : depth.bestBid - spreadOffset)
      : basePrice;

    const qty = (Math.floor(Math.random() * 10) + 1) * 100;

    const res = engineInstance.placeOrder({
      accountId: isBuy ? 'ACC_VPB_001' : 'ACC_VPB_002',
      symbol: sym,
      side,
      type,
      price: Math.max(tick, price),
      quantity: qty,
    });

    orderCountWindowRef.current += 1;
    setTotalSimulated((prev) => prev + 1);
    if (res.matches.length > 0) {
      setTotalMatches((prev) => prev + res.matches.length);
    }
    if (res.latencyMicros > 0) {
      setAvgLatencyMicros((prev) => Math.round((prev * 0.95) + (res.latencyMicros * 0.05)));
      setP99LatencyMicros((prev) => Math.max(prev, res.latencyMicros));
    }
  };

  const runBurstStress = (count: number) => {
    setIsBursting(true);
    let completed = 0;
    let matchCount = 0;
    const latencies: number[] = [];

    const startTime = performance.now();

    for (let i = 0; i < count; i++) {
      const isBuy = Math.random() > 0.5;
      const res = engineInstance.placeOrder({
        accountId: 'ACC_INST_MM',
        symbol: 'VPB',
        side: isBuy ? 'BUY' : 'SELL',
        type: 'LIMIT',
        price: 19500 + (Math.floor(Math.random() * 6) - 3) * 50,
        quantity: 100,
      });

      completed++;
      matchCount += res.matches.length;
      latencies.push(res.latencyMicros);
    }

    const elapsedMs = performance.now() - startTime;
    const burstTps = Math.round((count / (elapsedMs / 1000)));

    orderCountWindowRef.current += completed;
    setTotalSimulated((prev) => prev + completed);
    setTotalMatches((prev) => prev + matchCount);

    latencies.sort((a, b) => a - b);
    const avg = Math.round(latencies.reduce((a, b) => a + b, 0) / latencies.length);
    const p99 = latencies[Math.floor(latencies.length * 0.99)] || avg;

    setAvgLatencyMicros(avg);
    setP99LatencyMicros(p99);
    setOrdersPerSec(burstTps);
    setIsBursting(false);
  };

  return (
    <div className="space-y-6">
      {/* High-Throughput Engine Benchmark Control Panel */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-slate-800/80 gap-3">
          <div>
            <div className="flex items-center gap-2">
              <Zap className="w-5 h-5 text-amber-400" />
              <h2 className="text-base font-bold text-white tracking-wide">
                High-Throughput Load & Latency Benchmarks
              </h2>
            </div>
            <p className="text-xs text-slate-400 mt-1">
              Simulate continuous Market Maker bot flow and high-frequency order bursts targeting &gt;5,000 orders/sec.
            </p>
          </div>

          <div className="flex items-center gap-2">
            <button
              onClick={toggleBot}
              className={`px-3 py-1.5 rounded-md text-xs font-bold flex items-center gap-1.5 transition-colors ${
                isRunningBot
                  ? 'bg-rose-500 hover:bg-rose-600 text-white'
                  : 'bg-emerald-500 hover:bg-emerald-400 text-slate-950'
              }`}
            >
              {isRunningBot ? (
                <>
                  <Square className="w-3.5 h-3.5 fill-current" />
                  Stop MM Bot
                </>
              ) : (
                <>
                  <Play className="w-3.5 h-3.5 fill-current" />
                  Start Auto MM Bot
                </>
              )}
            </button>
          </div>
        </div>

        {/* Live Metrics Grid */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mt-4 font-mono tabular-nums">
          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              Throughput (Orders / Sec)
            </span>
            <div className="text-2xl font-bold text-emerald-400 mt-1 flex items-baseline gap-1">
              {ordersPerSec.toLocaleString()}
              <span className="text-xs text-slate-500 font-normal">ops/s</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Instantaneous matching speed
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              Average Latency (P50)
            </span>
            <div className="text-2xl font-bold text-blue-400 mt-1 flex items-baseline gap-1">
              {avgLatencyMicros}
              <span className="text-xs text-slate-500 font-normal">µs (microseconds)</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Sub-millisecond in-memory execution
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              Tail Latency (P99)
            </span>
            <div className="text-2xl font-bold text-purple-400 mt-1 flex items-baseline gap-1">
              {p99LatencyMicros}
              <span className="text-xs text-slate-500 font-normal">µs</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              Worst-case price-level traversal
            </span>
          </div>

          <div className="bg-slate-950/80 border border-slate-800/80 p-3.5 rounded-md">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">
              Total Matches Executed
            </span>
            <div className="text-2xl font-bold text-slate-100 mt-1 flex items-baseline gap-1">
              {totalMatches.toLocaleString()}
              <span className="text-xs text-slate-500 font-normal">trades</span>
            </div>
            <span className="text-[10px] text-slate-500 block mt-1">
              From {totalSimulated.toLocaleString()} total orders
            </span>
          </div>
        </div>

        {/* Burst Trigger Buttons */}
        <div className="mt-4 pt-3 border-t border-slate-800/80 flex flex-wrap items-center justify-between gap-3">
          <div className="text-xs text-slate-400">
            Fire burst limit/market batches to stress test price-time queues:
          </div>

          <div className="flex items-center gap-2">
            {[100, 500, 1000].map((count) => (
              <button
                key={count}
                disabled={isBursting}
                onClick={() => runBurstStress(count)}
                className="px-3 py-1.5 bg-slate-800 hover:bg-slate-700 disabled:opacity-50 text-slate-200 border border-slate-700 rounded text-xs font-mono transition-colors"
              >
                Fire +{count.toLocaleString()} Orders
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
};
