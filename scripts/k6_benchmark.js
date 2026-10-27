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

// The gateway runs a single APX order book; other symbols are accepted but never rest or match.
const SYMBOL = 'APX';
const SIDES = ['BUY', 'SELL'];

// Unknown account IDs are auto-created by the gateway with starter funds, so the load is spread
// over many accounts (pre-created in setup()) to keep any one account from running out of cash.
const ACCOUNT_COUNT = parseInt(__ENV.ACCOUNTS || '20000', 10);
const gatewayPort = __ENV.PORT || '5470';
const BASE_URL = `http://localhost:${gatewayPort}`;

export function setup() {
  // Fresh prefix per run: starter shares are only granted in memory when an account is first
  // created, so accounts left over from an earlier gateway process have no holdings to sell.
  const prefix = __ENV.ACCOUNT_PREFIX || `K6${Date.now()}`;
  // Create the accounts outside the measured scenario so DB inserts do not skew order latency.
  const batchSize = 200;
  for (let start = 0; start < ACCOUNT_COUNT; start += batchSize) {
    const reqs = [];
    for (let i = start; i < Math.min(start + batchSize, ACCOUNT_COUNT); i++) {
      reqs.push(['GET', `${BASE_URL}/v1/balance/${prefix}_${i}`]);
    }
    http.batch(reqs);
  }
  return { prefix };
}

export default function (data) {
  const side = SIDES[Math.floor(Math.random() * SIDES.length)];
  const accountId = `${data.prefix}_${Math.floor(Math.random() * ACCOUNT_COUNT)}`;

  // Price base around 19,000 - 20,500 VND in ticks of 50 VND
  const basePrice = 19500;
  const tickOffset = (Math.floor(Math.random() * 20) - 10) * 50;
  const price = basePrice + tickOffset;
  const quantity = Math.floor(Math.random() * 3 + 1) * 100; // 100-300 shares (standard lots)

  const payload = JSON.stringify({
    client_order_id: `k6_${__VU}_${__ITER}_${Date.now()}`,
    account_id: accountId,
    symbol: SYMBOL,
    side: side,
    order_type: 'LIMIT',
    price: price,
    quantity: quantity,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
  };

  const res = http.post(`${BASE_URL}/v1/orders`, payload, params);
  const duration = res.timings.duration;

  const isSuccess = check(res, {
    'status is 201 or 200': (r) => r.status === 201 || r.status === 200,
    'has valid order_id': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body && body.order && body.order.order_id !== undefined;
      } catch (e) {
        return false;
      }
    },
  });

  orderPlacementSuccess.add(isSuccess);
  totalOrdersCounter.add(1);
  matchLatencyTrend.add(duration);
}
