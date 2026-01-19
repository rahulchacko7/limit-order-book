// k6 WebSocket Load Test Script for Order Book
// Usage: k6 run tests/load/ws_load_test.js
// Install: https://k6.io/docs/get-started/installation/

import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';

// Custom metrics
const wsConnectLatency = new Trend('ws_connect_latency');
const wsMessageLatency = new Trend('ws_message_latency');
const wsErrorRate = new Rate('ws_errors');
const wsMessagesReceived = new Counter('ws_messages_received');
const wsConnections = new Counter('ws_connections');

// Configuration
export const options = {
  stages: [
    { duration: '30s', target: 5 },    // Ramp up to 5 WS connections
    { duration: '1m', target: 25 },    // Stay at 25 connections
    { duration: '30s', target: 50 },   // Ramp up to 50 connections
    { duration: '1m', target: 50 },    // Stay at 50 connections
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    ws_connect_latency: ['p(95)<1000'],
    ws_message_latency: ['p(95)<500'],
    ws_errors: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.WS_URL || 'ws://localhost:8080';
const PAIRS = ['BTC-USD', 'ETH-USD', 'LTC-USD'];

export function setup() {
  console.log(`Starting WebSocket load test against ${BASE_URL}`);
  return { baseUrl: BASE_URL, pairs: PAIRS };
}

export default function (data) {
  const baseUrl = data.baseUrl;
  const pairs = data.pairs;
  
  // Randomly select a pair
  const pair = pairs[Math.floor(Math.random() * pairs.length)];
  const url = `${baseUrl}/ws/${pair}`;

  // Connect to WebSocket
  const connectStart = Date.now();
  const res = ws.connect(url, {}, function(socket) {
    const connectDuration = Date.now() - connectStart;
    wsConnectLatency.add(connectDuration);
    wsConnections.add(1);

    // Track subscription
    let subscribed = true;
    let snapshotReceived = false;

    socket.on('open', function() {
      // Subscribe to additional pairs
      socket.send(JSON.stringify({
        action: 'subscribe',
        pairs: pairs.filter(p => p !== pair),
      }));
    });

    socket.on('message', function(msg) {
      wsMessagesReceived.add(1);
      const msgLatency = Date.now() - connectStart;
      wsMessageLatency.add(msgLatency);

      try {
        const data = JSON.parse(msg);
        
        // Check for snapshot
        if (data.type === 'snapshot' && !snapshotReceived) {
          snapshotReceived = true;
          check(data, {
            'snapshot has bids': (d) => d.bids && d.bids.length > 0,
            'snapshot has asks': (d) => d.asks && d.asks.length > 0,
            'snapshot has sequence': (d) => d.sequence !== undefined,
          });
        }

        // Check for trade events
        if (data.type === 'trade') {
          check(data, {
            'trade has price': (d) => d.trade && d.trade.price > 0,
            'trade has quantity': (d) => d.trade && d.trade.quantity > 0,
          });
        }

        // Check for order updates
        if (data.type === 'order_update') {
          check(data, {
            'order_update has order_id': (d) => d.order_id > 0,
            'order_update has status': (d) => d.status !== undefined,
          });
        }

        // Check for heartbeat
        if (data.type === 'heartbeat') {
          check(data, {
            'heartbeat has sequence': (d) => d.sequence > 0,
          });
        }

        // Check for subscription ack
        if (data.type === 'subscription_ack') {
          check(data, {
            'subscription_ack success': (d) => d.success === true,
          });
        }
      } catch (e) {
        console.log(`Failed to parse message: ${e.message}`);
      }
    });

    socket.on('error', function(e) {
      wsErrorRate.add(1);
      console.log(`WebSocket error: ${e.message}`);
    });

    socket.on('close', function() {
      subscribed = false;
    });

    // Stay connected for a bit
    sleep(5);

    // Unsubscribe before closing
    if (subscribed) {
      socket.send(JSON.stringify({
        action: 'unsubscribe',
        pairs: pairs.filter(p => p !== pair),
      }));
    }

    socket.close();
  });

  check(res, {
    'websocket connected': (r) => r && r.status === 101,
  });

  if (res.status !== 101) {
    wsErrorRate.add(1);
  }

  sleep(2);
}

export function teardown(data) {
  console.log('WebSocket load test completed');
  console.log(`Total WS connections: ${wsConnections.values}`);
  console.log(`Total WS messages received: ${wsMessagesReceived.values}`);
}

// Scenarios for different load patterns

// Scenario 1: Single pair subscription
export function singlePairScenario(data) {
  const pair = 'BTC-USD';
  const url = `${data.baseUrl}/ws/${pair}`;

  const res = ws.connect(url, {}, function(socket) {
    socket.on('open', function() {
      console.log(`Connected to ${pair}`);
    });

    socket.on('message', function(msg) {
      wsMessagesReceived.add(1);
    });

    socket.on('error', function(e) {
      wsErrorRate.add(1);
    });

    sleep(10);
    socket.close();
  });

  check(res, { 'connected': (r) => r && r.status === 101 });
}

// Scenario 2: Multi-pair subscription
export function multiPairScenario(data) {
  const url = `${data.baseUrl}/ws?pairs=["BTC-USD","ETH-USD","LTC-USD"]`;

  const res = ws.connect(url, {}, function(socket) {
    socket.on('open', function() {
      console.log('Connected to multiple pairs');
    });

    socket.on('message', function(msg) {
      wsMessagesReceived.add(1);
    });

    socket.on('error', function(e) {
      wsErrorRate.add(1);
    });

    sleep(10);
    socket.close();
  });

  check(res, { 'connected': (r) => r && r.status === 101 });
}

// Scenario 3: Rapid subscribe/unsubscribe
export function rapidSubscriptionScenario(data) {
  const pairs = data.pairs;
  const url = `${data.baseUrl}/ws/BTC-USD`;

  const res = ws.connect(url, {}, function(socket) {
    socket.on('open', function() {
      // Rapidly subscribe/unsubscribe
      for (let i = 0; i < 5; i++) {
        const pair = pairs[Math.floor(Math.random() * pairs.length)];
        socket.send(JSON.stringify({
          action: i % 2 === 0 ? 'subscribe' : 'unsubscribe',
          pairs: [pair],
        }));
        sleep(0.5);
      }
    });

    socket.on('message', function(msg) {
      wsMessagesReceived.add(1);
    });

    socket.on('error', function(e) {
      wsErrorRate.add(1);
    });

    sleep(5);
    socket.close();
  });

  check(res, { 'connected': (r) => r && r.status === 101 });
}

