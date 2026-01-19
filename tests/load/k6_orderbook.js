// k6 Load Test Script for Order Book Matching Engine
// Usage: k6 run tests/load/k6_orderbook.js
// Install: https://k6.io/docs/get-started/installation/

import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const orderLatency = new Trend('order_latency');
const cancelLatency = new Trend('cancel_latency');
const errorRate = new Rate('errors');
const ordersPlaced = new Counter('orders_placed');
const ordersCancelled = new Counter('orders_cancelled');

// Configuration
export const options = {
  stages: [
    { duration: '30s', target: 10 },   // Ramp up to 10 users
    { duration: '1m', target: 50 },    // Stay at 50 users
    { duration: '30s', target: 100 },  // Ramp up to 100 users
    { duration: '1m', target: 100 },   // Stay at 100 users
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],
    order_latency: ['p(95)<200'],
    errors: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.API_URL || 'http://localhost:8080';
const PAIRS = ['BTC-USD', 'ETH-USD', 'LTC-USD'];

export function setup() {
  console.log(`Starting load test against ${BASE_URL}`);
  return { baseUrl: BASE_URL };
}

export default function (data) {
  const baseUrl = data.baseUrl;
  const pair = PAIRS[Math.floor(Math.random() * PAIRS.length)];
  const userId = Math.floor(Math.random() * 10000) + 1;

  // Test 1: Place Order
  const placeStart = Date.now();
  const orderRes = placeOrder(baseUrl, pair, userId);
  const placeDuration = Date.now() - placeStart;
  orderLatency.add(placeDuration);

  let orderId = null;
  if (placeRes.status === 200) {
    orderId = placeRes.json('id');
    ordersPlaced.add(1);
  } else {
    errorRate.add(1);
    console.log(`Failed to place order: ${placeRes.status} - ${placeRes.body}`);
  }

  // Test 2: Get Order (if we got an order ID)
  if (orderId) {
    const getRes = http.get(
      `${baseUrl}/api/orders/${orderId}?pair=${pair}`,
      { headers: { 'Content-Type': 'application/json' } }
    );
    check(getRes, {
      'get order status 200': (r) => r.status === 200 || r.status === 404,
    });
  }

  // Test 3: Get Order Book
  const bookRes = http.get(
    `${baseUrl}/api/pairs/${pair}/book?levels=10`,
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(bookRes, {
    'order book status 200': (r) => r.status === 200,
  });

  // Test 4: Get Ticker
  const tickerRes = http.get(
    `${baseUrl}/api/pairs/${pair}/ticker`,
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(tickerRes, {
    'ticker status 200': (r) => r.status === 200,
  });

  // Test 5: List Pairs
  const pairsRes = http.get(
    `${baseUrl}/api/pairs`,
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(pairsRes, {
    'list pairs status 200': (r) => r.status === 200,
  });

  // Test 6: Cancel Order (if we got an order ID)
  if (orderId) {
    const cancelStart = Date.now();
    const cancelRes = http.del(
      `${baseUrl}/api/orders/${orderId}?pair=${pair}`,
      null,
      { headers: { 'Content-Type': 'application/json' } }
    );
    const cancelDuration = Date.now() - cancelStart;
    cancelLatency.add(cancelDuration);

    if (cancelRes.status === 200) {
      ordersCancelled.add(1);
    } else {
      errorRate.add(1);
      console.log(`Failed to cancel order: ${cancelRes.status} - ${cancelRes.body}`);
    }
  }

  sleep(1);
}

function placeOrder(baseUrl, pair, userId) {
  const price = 50000 + Math.random() * 1000;
  const quantity = 0.1 + Math.random() * 0.9;
  const side = Math.random() > 0.5 ? 'buy' : 'sell';

  const payload = JSON.stringify({
    user_id: userId,
    pair: pair,
    side: side,
    price: parseFloat(price.toFixed(2)),
    quantity: parseFloat(quantity.toFixed(6)),
  });

  const res = http.post(
    `${baseUrl}/api/orders`,
    payload,
    {
      headers: {
        'Content-Type': 'application/json',
        'X-User-ID': userId.toString(),
      },
    }
  );

  check(res, {
    'place order status 200': (r) => r.status === 200,
  });

  return res;
}

export function teardown(data) {
  console.log('Load test completed');
  console.log(`Total orders placed: ${ordersPlaced.values}`);
  console.log(`Total orders cancelled: ${ordersCancelled.values}`);
}

