import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// ---------------------------------------------------------------------------
// Authentication (the gateway's /v1 is authenticated; account id comes from
// the credential only, never from the request body):
//
//   K6_TOKEN=<jwt>   -> sends "Authorization: Bearer <K6_TOKEN>". A token is
//                       bound to ONE account, so every VU trades as that
//                       account (ACCOUNTS is ignored). For many accounts mint
//                       one token per account outside k6 and run several
//                       instances, or use the dev mode below.
//   DEV_AUTH=1       -> sends "X-Dev-Account-Id: <account>" per VU. Requires the
//                       gateway to run with AUTH_MODE=disabled-for-local-dev
//                       AND ALLOW_INSECURE_DEV_AUTH=1, with JWKS_URL unset
//                       (otherwise it refuses to start). In this mode the
//                       gateway binds 127.0.0.1 only, so run k6 on the same host.
//
// One of the two must be set; the script refuses to run unauthenticated.
//
// Accounts must already exist (no auto-create). Seed them when starting the
// gateway: SEED_ACCOUNTS=$(.\scripts\gen-seed-accounts.ps1 -Count 500) and run
// k6 with ACCOUNTS=500 (same count) and the same ACCOUNT_PREFIX (default K6).
//
// Rate limits are per client IP, and k6 is one IP. Start the gateway with e.g.
//   RATE_LIMIT_RPS=1000000 RATE_LIMIT_WRITE_RPS=1000000
// otherwise most requests answer 429 and the success rate collapses.
//
// Every POST carries a unique X-Idempotency-Key (required by the gateway).
// ---------------------------------------------------------------------------

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

// The gateway runs a single APX order book; other symbols are rejected.
const SYMBOL = 'APX';
const SIDES = ['BUY', 'SELL'];

const TOKEN = __ENV.K6_TOKEN || '';
const DEV_AUTH = __ENV.DEV_AUTH === '1';
const ACCOUNT_COUNT = parseInt(__ENV.ACCOUNTS || '100', 10);
const ACCOUNT_PREFIX = __ENV.ACCOUNT_PREFIX || 'K6';
const gatewayPort = __ENV.PORT || '5470';
const BASE_URL = __ENV.BASE_URL || `http://localhost:${gatewayPort}`;

// The account the calling VU trades as. Deterministic per VU, so a VU's
// orders, cancels and balance reads all belong to the same account.
function vuAccount() {
  return `${ACCOUNT_PREFIX}_${(__VU - 1) % ACCOUNT_COUNT}`;
}

function authHeaders() {
  if (TOKEN) {
    return { Authorization: `Bearer ${TOKEN}` };
  }
  return { 'X-Dev-Account-Id': vuAccount() };
}

export function setup() {
  if (!TOKEN && !DEV_AUTH) {
    throw new Error('set K6_TOKEN=<jwt> or DEV_AUTH=1 (gateway AUTH_MODE=disabled-for-local-dev)');
  }
  if (TOKEN) {
    return {}; // the token's account id is not known to the script; nothing to probe
  }
  // Fail fast when the seeded accounts are missing: the first VU's account
  // must answer 200 on its own balance (404 means SEED_ACCOUNTS was not applied).
  const probe = `${ACCOUNT_PREFIX}_0`;
  const res = http.get(`${BASE_URL}/v1/balance/${probe}`, { headers: { 'X-Dev-Account-Id': probe } });
  if (res.status !== 200) {
    throw new Error(`account ${probe} not usable (status ${res.status}); seed it with SEED_ACCOUNTS (scripts/gen-seed-accounts.ps1) and run the gateway with AUTH_MODE=disabled-for-local-dev`);
  }
  return {};
}

export default function () {
  const side = SIDES[Math.floor(Math.random() * SIDES.length)];

  // Price base around 19,000 - 20,500 VND in ticks of 50 VND
  const basePrice = 19500;
  const tickOffset = (Math.floor(Math.random() * 20) - 10) * 50;
  const price = basePrice + tickOffset;
  const quantity = Math.floor(Math.random() * 3 + 1) * 100; // 100-300 shares (standard lots)

  const key = `k6_${__VU}_${__ITER}_${Date.now()}`;
  // No account_id in the body: the gateway ignores it and uses the credential.
  const payload = JSON.stringify({
    client_order_id: key,
    symbol: SYMBOL,
    side: side,
    order_type: 'LIMIT',
    price: price,
    quantity: quantity,
  });

  const params = {
    headers: Object.assign(
      {
        'Content-Type': 'application/json',
        'X-Idempotency-Key': key,
      },
      authHeaders(),
    ),
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
