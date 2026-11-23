// Load test for the Predictive Inventory API.
//
// Traffic mix: 80% inventory reads, 20% sales, spread over SKU-0..SKU-99.
// Targets from the README: 2500 requests/s with p99 latency under 200 ms.
//
// Run it from the dev container with scripts/loadtest.sh, which starts the
// official k6 image on the same Docker network:
//
//   RATE=2500 DURATION=60s bash scripts/loadtest.sh
import http from 'k6/http';
import { check } from 'k6';

// "dev" is the dev container's service name on the Compose network.
const BASE_URL = __ENV.BASE_URL || 'http://dev:8080';
const RATE = Number(__ENV.RATE || 2500);
const DURATION = __ENV.DURATION || '60s';
const SKUS = 100;

export const options = {
  // constant-arrival-rate is an open model: k6 starts a new request every
  // 1/RATE seconds whether or not earlier ones have finished, like real
  // users. A closed model (a fixed number of looping users) slows down
  // when the server slows down and hides the latency it should measure.
  scenarios: {
    reads: {
      executor: 'constant-arrival-rate',
      exec: 'readInventory',
      rate: Math.round(RATE * 0.8),
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 200,
      maxVUs: 2000,
    },
    writes: {
      executor: 'constant-arrival-rate',
      exec: 'recordSale',
      rate: Math.round(RATE * 0.2),
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 100,
      maxVUs: 1000,
    },
  },

  // A failed threshold makes k6 exit with a non-zero code, so this file
  // can gate a CI pipeline.
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{scenario:reads}': ['p(99)<200'],
    'http_req_duration{scenario:writes}': ['p(99)<200'],
  },

  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

function randomSku() {
  return `SKU-${Math.floor(Math.random() * SKUS)}`;
}

export function readInventory() {
  const res = http.get(`${BASE_URL}/inventory/${randomSku()}`, {
    // Without a name tag, every SKU would be reported as its own URL.
    tags: { name: 'GET /inventory/:sku' },
  });
  check(res, { 'inventory 200': (r) => r.status === 200 });
}

export function recordSale() {
  const res = http.post(
    `${BASE_URL}/sales`,
    JSON.stringify({ sku: randomSku(), quantity: 1 }),
    {
      headers: {
        'Content-Type': 'application/json',
        // Unique per request: each one is a new sale, not a retry.
        'Idempotency-Key': `k6-${__VU}-${__ITER}-${Date.now()}`,
      },
      tags: { name: 'POST /sales' },
    },
  );
  check(res, { 'sale 201': (r) => r.status === 201 });
}
