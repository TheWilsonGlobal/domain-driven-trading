import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// Custom metrics for financial throughput and latency evaluation
const orderPlacementSuccess = new Rate('order_placement_success');
const matchLatencyTrend = new Trend('engine_latency_ms', true);
const totalOrdersCounter = new Counter('orders_submitted');

export const options = {
  scenarios: {
    high_throughput_orders: {
      executor: 'ramping-arrival-rate',
      startRate: 500,
      timeUnit: '1s',
      preAllocatedVUs: 100,
      maxVUs: 1000,
      stages: [
        { target: 1500, duration: '15s' }, // Warmup to 1,500 req/sec
        { target: 5000, duration: '30s' }, // Target: 5,000 orders/sec stress phase
        { target: 6500, duration: '20s' }, // Peak burst: 6,500 orders/sec
        { target: 0, duration: '10s' },    // Cooldown
      ],
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<15', 'p(99)<35'], // Sub-millisecond engine + fast HTTP Gateway
    order_placement_success: ['rate>0.999'],       // 99.9% success rate
  },
};

const SYMBOLS = ['VPB', 'HPG', 'FPT', 'SSI', 'MWG'];
const SIDES = ['BUY', 'SELL'];
const ACCOUNTS = ['ACC_VPB_001', 'ACC_VPB_002', 'ACC_VPB_003', 'ACC_INST_MM'];

export default function () {
  const symbol = SYMBOLS[Math.floor(Math.random() * SYMBOLS.length)];
  const side = SIDES[Math.floor(Math.random() * SIDES.length)];
  const accountId = ACCOUNTS[Math.floor(Math.random() * ACCOUNTS.length)];

  // Price base around 19,000 - 20,500 VND in ticks of 50 VND
  const basePrice = 19500;
  const tickOffset = (Math.floor(Math.random() * 20) - 10) * 50;
  const price = basePrice + tickOffset;
  const quantity = Math.floor(Math.random() * 50 + 1) * 100; // 100-share standard lots

  const payload = JSON.stringify({
    client_order_id: `k6_${__VU}_${__ITER}_${Date.now()}`,
    account_id: accountId,
    symbol: symbol,
    side: side,
    order_type: 'LIMIT',
    price: price,
    quantity: quantity,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'X-Idempotency-Key': `idem_${__VU}_${__ITER}`,
    },
  };

  const startTime = new Date().getTime();
  const res = http.post('http://localhost:8080/v1/orders', payload, params);
  const duration = new Date().getTime() - startTime;

  const isSuccess = check(res, {
    'status is 201 or 200': (r) => r.status === 201 || r.status === 200,
    'has valid order_id': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body && body.order_id !== undefined;
      } catch (e) {
        return false;
      }
    },
  });

  orderPlacementSuccess.add(isSuccess);
  totalOrdersCounter.add(1);
  matchLatencyTrend.add(duration);
}
