# Work Plan: Predictive Inventory System

Portfolio project to learn **Go, Gin, AWS RDS and Claude (Go SDK)**.
Limit: **8 one-hour sessions**. Repository: `predictive-inventory`.

Source: `sistema-de-inventario-predictivo.zip` (README with requirements + sample base code).

**Language convention:** everything in this repository is written in English: code, comments, commit messages, documentation and this plan.

## 1. What we are building

A Go backend service that:

1. Receives sales through `POST /sales` and stores them in PostgreSQL (RDS in the cloud).
2. Runs a background worker that asks Claude for a demand forecast per SKU.
3. Stores the forecast in Redis (`forecast:<sku>`).
4. Serves `GET /inventory/:sku`, which compares forecast against stock and returns the quantity to reorder.
5. Publishes reorder messages to an SQS FIFO queue.

The four technical complexities from the README are the core of the portfolio piece, and each one has its own session:

| Complexity | Solution | Session |
| --- | --- | --- |
| Idempotent inventory updates | Transaction + `SELECT ... FOR UPDATE` | 2 |
| Exactly-once prediction per SKU | Redis lock (`SET NX` with TTL) | 4 |
| Back-pressure from Claude latency | Buffered channel of 100 + `429` when full | 4 |
| LocalStack does not emulate RDS | Real PostgreSQL in Docker + migrations | 2 and 6 |

## 2. State of the zip: a guide, not working code

The README defines the scope well. The Go code is a sketch that **does not compile** and has to be rewritten:

- `go.mod`, `Dockerfile`, SQL migrations, tests and `scripts/integration_test.sh` are missing (the README mentions them, the zip does not include them).
- Duplicated `src/src/` folder. It changes to the Go convention: `cmd/` + `internal/`.
- `predictor.go` calls an endpoint that does not exist (`/v1/predict`), uses a retired model, is missing the `bytes` import, and the API key is a string literal instead of an environment variable.
- `db_client.go` shares a single connection (`pgx.Conn`) across requests, which is not safe for concurrent use; it must be `pgxpool`. The query `interval '$2 days'` fails because the parameter sits inside a literal.
- `InsertSale` neither decrements stock nor locks the row; the Redis lock and the back-pressure queue described in the README do not exist.
- Unused imports in `main.go` and in the handler (a compile error in Go).
- `docker-compose.yml`: the LocalStack init script path and healthcheck are from old versions; the `app` service has no `Dockerfile`.
- Outdated dependencies: `go-redis/v8` → `v9`, `pgx/v4` → `v5`.
- `k6.js` expects `200` for random SKUs; seed data is needed (`SKU-0` to `SKU-99`).

## 3. Decisions made (change them if you disagree)

1. **"Claude SDK Agent" in Go.** The official Claude Agent SDK exists only for TypeScript and Python; for Go there are only community projects. We will use the official Go SDK (`github.com/anthropics/anthropic-sdk-go`) and write the agent loop by hand with tool use (`get_sales_history` and `get_current_stock` tools). It teaches more and avoids third-party dependencies.
2. **LocalStack.** Since March 2026 the `latest` image requires an account and an auth token. We pin an earlier community version (4.x) that needs no token. Alternative: create the free account (Hobby plan) and set `LOCALSTACK_AUTH_TOKEN`.
3. **Real AWS.** RDS is created with Terraform in session 8, tested, and destroyed the same day to avoid cost. It requires your own AWS account.
4. **Out of scope** (kept as backlog in the README): CDK, ECS/EKS, API Gateway, ElastiCache, CSV upload to S3, DynamoDB, Jaeger/Grafana.
5. **Dev environment.** Development happens inside a VS Code dev container (`.devcontainer/`): Go toolchain, PostgreSQL and Redis all run in containers. The Windows host only needs Docker Desktop and VS Code; Go, Terraform and k6 are never installed on Windows.
6. **Metrics.** The 2500 RPS and p99 < 200 ms targets are measured on your laptop with k6, and the real result is published whether or not the target is met.

Portfolio note: the impact figures in the README ("50 tons", "$200K per month") do not come from a real deployment. Present them as a simulated scenario and give weight to the metrics that are actually measured (latency, RPS, error rate).

## 4. Final repository layout

```text
predictive-inventory/
├── .devcontainer/               # dev container: Go + PostgreSQL + Redis
├── cmd/api/main.go              # startup: config, router, worker, graceful shutdown
├── internal/
│   ├── config/                  # environment variables
│   ├── httpapi/                 # Gin handlers and middleware
│   ├── store/                   # PostgreSQL (pgxpool): sales, stock
│   ├── cache/                   # Redis: forecasts and locks
│   ├── predictor/               # Predictor interface + Claude implementation
│   ├── worker/                  # prediction queue and worker pool
│   └── reorder/                 # SQS publisher
├── migrations/                  # versioned SQL
├── infra/                       # Terraform (LocalStack and real AWS)
├── loadtest/k6.js
├── scripts/                     # LocalStack init, seed, integration test
├── docs/PLAN.md
├── Dockerfile
├── docker-compose.yml
└── README.md
```

## 5. The 8 sessions

Every session ends with something that runs and a commit. If a session runs long, items marked *optional* move to the backlog.

### Session 1 — Go fundamentals and service skeleton
- **You learn:** modules and packages, structs, implicit interfaces, error handling without exceptions, `go build` / `go test` / `go vet`.
- **Work:** dev container with Go, PostgreSQL and Redis; `git init`, `go mod init`, `cmd/` + `internal/` layout, Gin with `GET /health`, configuration from environment variables, multi-stage `Dockerfile`.
- **Deliverable:** inside the dev container, `go run ./cmd/api` serves `GET /health` with `200`, and the service image builds.

### Session 2 — PostgreSQL and race-free sales
- **You learn:** `pgx/v5` and `pgxpool`, `context.Context`, transactions, migrations, integration tests with `testcontainers-go`.
- **Work:** migrations (`sales`, `inventory`), seed of 100 SKUs, `POST /sales` that inserts the sale and decrements stock in one transaction with `SELECT ... FOR UPDATE`.
- **Deliverable:** a test that fires 50 concurrent sales at the same SKU and the final stock adds up, with no negatives.

### Session 3 — Redis and the inventory endpoint
- **You learn:** `go-redis/v9`, TTL, unit tests with `miniredis` and `httptest`, table-driven tests.
- **Work:** cache layer for forecasts, `GET /inventory/:sku` with the reorder logic, correct responses for an unknown SKU and for a forecast not yet computed.
- **Deliverable:** endpoint working with a forecast set by hand in Redis, covered by tests.

### Session 4 — Concurrency: worker, back-pressure and distributed lock
- **You learn:** goroutines, channels, `select`, `sync.WaitGroup`, context cancellation, graceful shutdown.
- **Work:** `Predictor` interface with a fake implementation, buffered queue of 100, worker pool, `429` when the queue is full, per-SKU `SET NX` lock with TTL.
- **Deliverable:** tests proving a single prediction per SKU within the window and the `429` on a full queue; clean `go test -race`.

### Session 5 — Forecasting with Claude
- **You learn:** official Go SDK, Messages API, tool use, structured output, timeouts and retries, cost control.
- **Work:** real `Predictor` implementation: an agent loop where Claude requests history and stock through tools and returns validated JSON; test against a fake server (`httptest`).
- **Deliverable:** full flow sale → prediction → cache → `/inventory` with real Claude responses.
- **Requires:** an Anthropic API key with some credit.

### Session 6 — AWS locally: Terraform, LocalStack and SQS
- **You learn:** Terraform (provider, variables, outputs, state), `aws-sdk-go-v2`, FIFO queues and deduplication.
- **Work:** LocalStack in compose, Terraform creates the queue, the service publishes the reorder message when needed, end-to-end `scripts/integration_test.sh`.
- **Deliverable:** local `terraform apply` and the message visible in the queue after a sale.

### Session 7 — Observability and load testing
- **You learn:** OpenTelemetry in Gin, Prometheus metrics (`/metrics`), k6, reading p95/p99.
- **Work:** traces and metrics, k6 run with the fake predictor, tuning of connection pool and workers, second run.
- **Deliverable:** before/after results table in the README.
- **Optional:** find the saturation point.

### Session 8 — Real RDS, CI and portfolio wrap-up
- **You learn:** `aws_db_instance`, security groups, one Terraform codebase for local and cloud, GitHub Actions.
- **Work:** RDS PostgreSQL with Terraform, migrations and smoke test against RDS, `terraform destroy`; CI workflow (build, vet, test); final README with architecture, decisions, results and backlog.
- **Deliverable:** public repository ready to show.
- **Requires:** an AWS account with credentials configured.

## 6. Progress log

- **Session 1 — done (2026-11-16).** Dev container running (Go 1.27, PostgreSQL 17, Redis 7); module `github.com/xtylo/predictive-inventory`; Gin `GET /health`; `internal/config` with tests; multi-stage `Dockerfile` verified.
  - The Docker feature needs `"moby": false` on this base image.
  - To run the built image next to the dev stack: `docker run --network predictive-inventory_devcontainer_default -e POSTGRES_DSN -e REDIS_ENDPOINT predictive-inventory`, then reach it as `api:8080`.
- **Session 2 — done (2026-11-19).** goose migrations embedded with `go:embed` and applied at startup; `pgxpool` store; `POST /sales` with `SELECT ... FOR UPDATE`, `Idempotency-Key` header (201 new, 200 replay, 400, 404, 409); `scripts/seed.sql` (SKU-0..SKU-99, 1000 units); `testcontainers-go` tests: 50 buyers for 30 units, 10 concurrent retries of one key, unknown SKU — all passing with `-race`.
  - Added beyond the README: idempotency keys, since `FOR UPDATE` alone prevents races but not duplicate retries.
  - Load the seed with `docker exec -i predictive-inventory_devcontainer-postgres-1 psql -U inventory < scripts/seed.sql`.
  - Lesson: a machine restart wiped all uncommitted work. Commit after every step.
- **Session 3 — done (2026-11-19).** `internal/cache` stores forecasts as JSON in Redis (`forecast:<sku>`, 24h TTL) with `miniredis` tests; `internal/planning.ReorderQuantity` (target = ceil(demand × 1.5), never negative) with table-driven tests; `GET /inventory/:sku` (200 ok, 200 `forecast_pending`, 404, 500) with `httptest` and fake dependencies; `store.GetStock` integration test.
  - A missing forecast is a normal state (`forecast_pending`), not a 500 as in the original sketch.
  - The dev database can come back empty after Docker recreates its volume; re-run the seed if every SKU returns 404.
  - Set a forecast by hand: `docker exec predictive-inventory_devcontainer-redis-1 redis-cli SET forecast:SKU-1 '{"sku":"SKU-1","demand":800,"horizon_days":30,"generated_at":"2026-11-19T12:00:00Z"}' EX 3600`.
- **Session 4 — done (2026-11-21).** `predictor.Predictor` interface with a `Fake` (fixed demand after a delay); `cache.TryLock`/`Unlock` (`SET NX` with TTL); `internal/worker` pool: bounded channel queue, non-blocking `Enqueue` (`ErrQueueFull`, `ErrClosed`), per-SKU dedup lock held for the whole 5-minute window and released on failure; `POST /forecasts/:sku/refresh` (202, 404, 429 with `Retry-After`, 503); graceful shutdown with `signal.NotifyContext`, `http.Server.Shutdown`, then `Pool.Shutdown`, sharing a 15s deadline. Tests: 20 triggers with 4 workers reach the model once, failed prediction releases the lock, full queue, enqueue after shutdown, slow work cancelled at the deadline.
  - Deviation from the README: `POST /sales` never returns 429. The sale is already committed when the forecast is queued, so queueing is best effort; back-pressure is exposed on the refresh endpoint instead.
  - Use a built binary (`go build -o bin/api ./cmd/api`) to test shutdown: `go run` does not reliably forward signals.
- **Session 5 — done (2026-11-21).** `predictor.Claude`: agent loop on the Anthropic Messages API (official Go SDK v2, `github.com/anthropics/anthropic-sdk-go/v2`) with tools `get_sales_history`, `get_current_stock` and `submit_forecast`; `tool_choice: any` forces a structured answer; tool input validated as untrusted, invalid answers returned with `is_error` so the model self-corrects; max 6 turns and 1024 tokens per call; token usage logged. `store.SalesByDay` (`generate_series` + `LEFT JOIN`, zero-filled days). Forecast `rationale` stored in Redis and returned by `/inventory`. `scripts/seed_sales.sql` adds 90 days of synthetic history (trend, weekend peak, noise) for SKU-0..SKU-9. Tests use an `httptest` fake of `/v1/messages`.
  - Config: `ANTHROPIC_API_KEY` (empty = fake predictor), `ANTHROPIC_BASE_URL` (empty = Anthropic API), `ANTHROPIC_MODEL` (default `claude-haiku-5-5`).
  - Development runs on Token Harbor's Anthropic-compatible endpoint with a free model: `ANTHROPIC_BASE_URL=https://tokenharbor.ai`, `ANTHROPIC_MODEL=mimo-v2.6-flash:free`. Switch to a Claude model for the final demo.
  - First real run, SKU-3: forecast 585 units vs 544 sold in the last 30 days (+7.5%, consistent with the upward trend); the model ignored today's zero as a non-stockout.
  - Backlog idea: a deterministic baseline (moving average) next to the LLM forecast, to compare accuracy and as a fallback.
- **Next: session 6** — LocalStack, Terraform and the SQS FIFO reorder queue.

## 7. Before session 1

- Install Docker Desktop (WSL 2 backend) and VS Code with the Dev Containers extension on Windows. Nothing else goes on the host.
- Recommended, 1–2 hours on your own: A Tour of Go (https://go.dev/tour), at least through interfaces. That way session 1 goes to the project instead of syntax.
- Terraform and k6 are added to the dev container when needed (sessions 6 and 7).
