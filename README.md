# Limit Order Book Matching Engine (Go)

This project is a practical implementation of a **limit order book matching engine**, similar to what is used in crypto exchanges.
It is built to demonstrate system design, concurrency handling, and clean Go architecture.

The system supports limit buy/sell orders, price-time (FIFO) matching, asynchronous processing using RabbitMQ, caching with Redis, and real-time updates via WebSocket.

---

## Features

* Limit buy and sell orders (no market orders)
* Price-time priority matching (FIFO)
* Partial and full order fills
* Multiple currency pair support
* In-memory order book with persistence
* Asynchronous order processing using RabbitMQ
* PostgreSQL for permanent storage
* Redis for order book snapshots
* WebSocket for real-time order book updates
* REST APIs for admin and users
* Order cancellation (basic support)
* Message deduplication (effectively-once processing)
* Connection pooling and performance optimizations

---

## Tech Stack

* Go **1.21+** (Gin framework)
* PostgreSQL 15+
* Redis 7+
* RabbitMQ 3.12+
* WebSocket
* Docker & Docker Compose

---

## Project Structure

```
cmd/
  server/     → HTTP API server with integrated matching engine

internal/
  api/        → HTTP handlers and routing
  engine/     → Order book & matching logic (in-memory)
  models/     → Data models with validation
  store/      → Database layer (PostgreSQL)
  messaging/  → RabbitMQ publisher/consumer
  cache/      → Redis caching layer
  ws/         → WebSocket server
  config/     → Environment configuration
  metrics/    → Metrics collection
  middleware/ → Auth, rate limiting, circuit breaker

migrations/   → SQL migration files
tests/        → Integration and load tests
docker-compose.yml
```

---

## How It Works (High Level)

1. Orders are placed via REST API
2. Orders are matched immediately in-memory using price-time priority
3. Trade events and order updates are published to RabbitMQ
4. Async consumers persist data to PostgreSQL
5. Order book snapshots are cached in Redis
6. Updates are broadcast to clients via WebSocket

**Key Design Decision**
Matching is performed synchronously in memory for low latency, while database persistence is handled asynchronously to avoid blocking the matching engine.

---

## Requirements

* Go 1.21+
* Docker & Docker Compose

---

## Setup & Run

### 1. Clone the repository

```bash
git clone https://github.com/rahulchacko7/limit-order-book
cd limit-order-book
```

---

### 2. Start infrastructure services

```bash
docker compose up -d
```

This starts:

* PostgreSQL (port 5432)
* Redis (port 6379)
* RabbitMQ (ports 5672, 15672)

RabbitMQ Management UI:
`http://localhost:15672` (guest / guest)

---

### 3. Install dependencies

```bash
go mod download
```

---

### 4. Start the server

```bash
go run cmd/server/main.go
```

The server starts on `http://localhost:8080` by default.

The server includes:

* HTTP API server
* In-memory matching engine
* RabbitMQ consumer workers
* WebSocket hub

---

## Configuration

Configuration is managed via environment variables with sensible defaults:

```env
SERVER_PORT=:8080

POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_DB=orderbook
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres

REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=

RABBITMQ_URL=amqp://guest:guest@localhost:5672/
RABBITMQ_EXCHANGE=orderbook.events

WORKER_COUNT=4
WS_ENABLED=true
```

**Note**
The system is designed to tolerate temporary Redis or RabbitMQ unavailability with reduced functionality.

---

## API Examples

### Add Currency Pair (Admin)

```http
POST /api/pairs
Content-Type: application/json

{
  "base": "BTC",
  "quote": "USDT"
}
```

---

### Place Order

```http
POST /api/orders
Content-Type: application/json

{
  "pair": "BTC/USDT",
  "side": "buy",
  "price": 30000,
  "quantity": 0.5,
  "user_id": 1
}
```

---

### View Order Book

```http
GET /api/pairs/BTC/USDT/book?levels=10
```

Response:

```json
{
  "pair": "BTC/USDT",
  "bids": [
    { "price": 29400, "quantity": 1.5 }
  ],
  "asks": [
    { "price": 29600, "quantity": 1.0 }
  ]
}
```

---

### Get Ticker (Best Bid / Ask)

```http
GET /api/pairs/BTC/USDT/ticker
```

---

### User Orders

```http
GET /api/orders?user_id=1
```

---

### Cancel Order

```http
DELETE /api/orders/:id?pair=BTC/USDT
```

---

## WebSocket

Real-time order book updates:

```
ws://localhost:8080/ws/BTC/USDT
```

**Message Types**

* `trade` – Trade execution
* `order_update` – Order status change
* `orderbook_update` – Order book depth update

---

## Database Schema

### Orders

* `id`, `user_id`, `pair`, `side`, `price`, `quantity`, `filled`, `status`, `created_at`

### Trades

* `id`, `buy_order_id`, `sell_order_id`, `price`, `quantity`, `created_at`

### Currencies

* `code`, `name`, `precision`, `min_amount`, `is_active`, `created_at`, `updated_at`

### Currency Pairs

* `base`, `quote`, `is_active`, `created_at`, `updated_at`

See the `migrations/` directory for full schema definitions.

---

## Testing

### Unit Tests

```bash
go test ./... -v
```

### Integration Tests

```bash
go test ./tests/integration/... -v
```

### Benchmarks

```bash
go test ./internal/engine -bench=. -benchmem
```

### Load Testing (k6)

```bash
k6 run tests/load/k6_orderbook.js
```

---

## Matching Algorithm

**Price-Time Priority (FIFO)**

* **Buy Orders**

  * Higher price first
  * Earlier orders first

* **Sell Orders**

  * Lower price first
  * Earlier orders first

**Order States**
`open`, `partial`, `filled`, `cancelled`

---

## Performance

**Time Complexity**

* Insert order: `O(log n)`
* Match order: `O(k log n)`
* Best price lookup: `O(1)`
* Cancel order: `O(n)`
* Depth query (k levels): `O(k)`

**Design Targets**

* Low-latency in-memory matching
* High throughput under concurrent load
* Real-time WebSocket updates

---

## Purpose

This project was built as a **practical assessment** to demonstrate:

* System design and architecture
* Concurrency handling in Go
* Clean and maintainable code structure
* Exchange-style backend patterns
* Database design and migrations
* Asynchronous processing using message queues
* Performance-aware engineering decisions

---

## Author

Built by **Rahul Chacko**
Software Engineer (Go)

---
