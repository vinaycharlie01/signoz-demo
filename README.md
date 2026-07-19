# signoz-demo

A small, production-shaped Golang **Order Service**, built with **Hexagonal
Architecture** (Ports & Adapters), instrumented end-to-end with
**OpenTelemetry** (traces, metrics, and logs), and integrated with a
self-hosted **SigNoz** telemetry backend running via **Foundry**.

---

## Table of Contents

1. [Project overview](#project-overview)
2. [Architecture](#architecture)
3. [How it integrates with SigNoz (Foundry)](#how-it-integrates-with-signoz-foundry)
4. [Prerequisites](#prerequisites)
5. [Quick start](#quick-start)
   - [Step 1 — Start SigNoz (Foundry)](#step-1--start-signoz-foundry)
   - [Step 2 — Start the Order Service](#step-2--start-the-order-service)
6. [Environment variables](#environment-variables)
7. [Running locally (without Docker)](#running-locally-without-docker)
8. [Sending test requests](#sending-test-requests)
9. [Load generation](#load-generation)
10. [Viewing telemetry in SigNoz](#viewing-telemetry-in-signoz)
11. [Troubleshooting](#troubleshooting)
12. [Cleanup](#cleanup)
13. [Simplifications](#simplifications-vs-a-real-production-service)

---

## Project overview

| | |
| --- | --- |
| Domain | Orders (`POST` / `GET /api/v1/orders`) |
| Architecture | Hexagonal — `internal/domain` has zero framework imports |
| Database | SQLite (`modernc.org/sqlite`, pure Go, no cgo) |
| Observability | OpenTelemetry SDK — **traces + metrics + logs**, exported over OTLP/gRPC |
| Telemetry backend | Self-hosted **SigNoz** via [Foundry](https://signoz.io/docs/install/docker/) |
| Build tooling | [Mage](https://magefile.org/) — no Makefile, no shell scripts |

---

## Architecture

```
Client
  │  HTTP :8090
  ▼
internal/adapters/http      (driving adapter — net/http + otelhttp)
  │  ports.OrderService
  ▼
internal/application        (use cases — OrderService)
  │  ports.OrderRepository
  ▼
internal/adapters/sqlite    (driven adapter — database/sql + modernc.org/sqlite)
  │
  ▼
SQLite (orders.db)
```

`internal/domain` sits behind both ports and imports neither `net/http`,
`database/sql`, nor OpenTelemetry.

### Telemetry signal flow

```
Order Service (Go)
  │
  │  OTLP/gRPC  :4317   (traces + metrics + logs — single endpoint)
  ▼
SigNoz ingester (signoz-otel-collector)   ← runs inside Foundry
  │
  ├─ traces   → ClickHouse (signoz_traces)
  ├─ metrics  → ClickHouse (signoz_metrics / signoz_meter)
  └─ logs     → ClickHouse (signoz_logs)
                    │
                    ▼
             SigNoz UI  :8080
```

All three signals share a **single OTLP/gRPC endpoint** (`signoz-ingester:4317`
inside `signoz-network`).  No separate endpoints, no per-signal configuration
needed.

---

## How it integrates with SigNoz (Foundry)

SigNoz is installed via **Foundry** (the modern installation method — the old
`docker compose up` in the SigNoz repo itself is deprecated).  Foundry starts:

| Component | Role |
| --- | --- |
| `signoz-telemetrystore-clickhouse-0-0` | Telemetry storage (traces, metrics, logs) |
| `signoz-telemetrykeeper-clickhousekeeper-0` | ClickHouse coordination |
| `signoz-telemetrystore-migrator` | Schema bootstrap/migrations |
| `ingester` (alias: `signoz-ingester`) | OTLP receiver → ClickHouse writer |
| `signoz-metastore-postgres-0` | SigNoz metadata (users, dashboards, alerts) |
| `signoz-signoz-0` | SigNoz API + UI (`:8080`) |

All of these run on a Docker network called **`signoz-network`**.

This demo's `docker-compose.yml` **only starts the Order Service app** and
attaches it to the already-running `signoz-network`.  The app sends all
telemetry to `signoz-ingester:4317` — the ingester that Foundry manages.

> **Why not bundle ClickHouse + the collector here?**  
> The Foundry stack already owns ClickHouse, the schema migrations, and the
> collector.  Duplicating them would create two independent ClickHouse instances
> (one for SigNoz, one for the demo) and the SigNoz UI would never see the
> demo's data.  Joining `signoz-network` is the correct, zero-duplication way
> to integrate.

---

## Prerequisites

| Tool | Version | Purpose |
| --- | --- | --- |
| Go | 1.24+ | Build the service |
| Docker + Docker Compose | v2.x | Run everything |
| [Mage](https://magefile.org/) | latest | Build targets |

Install Mage once:

```bash
go install github.com/magefile/mage@latest
# make sure $(go env GOPATH)/bin is on PATH
```

---

## Quick start

### Step 1 — Start SigNoz (Foundry)

From the **foundry repo root** (one directory above this one):

```bash
cd /path/to/foundry
docker compose -f pours/deployment/compose.yaml up -d
```

Wait until SigNoz is healthy (usually ~60 seconds):

```bash
docker compose -f pours/deployment/compose.yaml ps
# signoz-signoz-0 should show "healthy"
```

Open the SigNoz UI: **http://localhost:8080**

> **First-time setup**: SigNoz will prompt you to create an admin account on
> the first visit.

### Step 2 — Start the Order Service

```bash
# from signoz-demo/
mage docker:up
```

This will:
1. Cross-compile the Go binary for Linux (`dist/linux_{amd64,arm64}/api`)
2. Build the Docker image
3. Start the `app` container joined to `signoz-network`

Verify the app is up:

```bash
curl http://localhost:8090/health
# → {"status":"ok"}
```

---

## Environment variables

All variables have sensible defaults for the Docker Compose setup.  Override
them if needed (e.g. for a different ingester address in a remote deployment).

| Variable | Default | Meaning |
| --- | --- | --- |
| `HTTP_ADDR` | `:8090` | Address the HTTP API listens on |
| `DB_PATH` | `./data/orders.db` | SQLite file path |
| `OTEL_SERVICE_NAME` | `signoz-demo-order-service` | `service.name` resource attribute — how the service appears in SigNoz |
| `SERVICE_VERSION` | `0.1.0` | `service.version` resource attribute |
| `DEPLOYMENT_ENVIRONMENT` | `local` | `deployment.environment.name` resource attribute |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTLP/gRPC endpoint for **all three signals** (traces, metrics, logs) |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Skip TLS — fine for local dev; never set in production |

In the Docker Compose stack, `OTEL_EXPORTER_OTLP_ENDPOINT` is set to
`signoz-ingester:4317` (the ingester's in-network alias).  When running the
app on the host directly, use `localhost:4317`.

---

## Running locally (without Docker)

If you want to run the Go binary directly on your host (while SigNoz still runs
in Docker):

```bash
# 1. Ensure Foundry is running (Step 1 above)

# 2. Build and run the service
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
OTEL_EXPORTER_OTLP_INSECURE=true \
mage go:run
```

The Foundry ingester binds ports `4317` and `4318` to the host loopback, so
`localhost:4317` works from outside Docker.

---

## Sending test requests

```bash
# Create an order
curl -s -X POST http://localhost:8090/api/v1/orders \
  -H 'Content-Type: application/json' \
  -d '{"customer_name":"Alice","item":"widget","quantity":2,"amount_cents":1999}' | jq

# List all orders
curl -s http://localhost:8090/api/v1/orders | jq

# Get a specific order (replace <id> with the id returned above)
curl -s http://localhost:8090/api/v1/orders/<id> | jq

# Health / readiness
curl http://localhost:8090/health
curl http://localhost:8090/ready
```

---

## Load generation

`cmd/loadgen` is a small Go program that drives the API with realistic traffic
patterns, wired into Mage via the `loadgen-*.yaml` files:

```bash
mage loadgen:normal       # 20 healthy sequential requests
mage loadgen:slow         # requests that hit the injected ~1.8 s SQLite latency
mage loadgen:errors       # mix of use-case-level and repository-level failures
mage loadgen:concurrent   # 60 requests, 10 concurrent workers, mixed scenarios
```

Each target sets the `X-Demo-Scenario` header (`slow` / `error` / `db-fail`).
You can also trigger a scenario manually:

```bash
curl -s -X POST http://localhost:8090/api/v1/orders \
  -H 'Content-Type: application/json' \
  -H 'X-Demo-Scenario: slow' \
  -d '{"customer_name":"Bob","item":"gadget","quantity":1,"amount_cents":999}'
```

---

## Viewing telemetry in SigNoz

### Traces

1. Open **http://localhost:8080** → **Services** → `signoz-demo-order-service`
2. Click **Traces** → filter by `http.route = POST /api/v1/orders`
3. Run `mage loadgen:slow` and open a slow trace — you'll see:
   `POST /api/v1/orders` → `OrderService.CreateOrder` → `sqlite.INSERT orders`
   with almost all latency on the last span.

### Metrics

- **Services** overview shows p50/p90/p99 latency, RPS, and error rate
  (derived from spans by the collector's `signozspanmetrics` processor).
- Custom application metrics (Dashboards → New Panel → Metrics):
  - `orders_created_total`
  - `order_create_duration_seconds`
  - `db_operation_duration_seconds` (filter by `db.operation`)
  - `order_errors_total`

### Logs

**Logs Explorer** → filter `service.name = signoz-demo-order-service`.  
Every request logs one structured `http request` line; failures also log an
`unhandled service error` line.

### Trace ↔ Log correlation

Every log record emitted while a span is active automatically carries
`trace_id` and `span_id` attributes (see `pkg/observability/logger.go`'s
`traceContextHandler`).  In the SigNoz UI:
- Open a trace → **Related logs** → jump straight to that request's log lines.
- Open a log line → click the `trace_id` value → jump to the trace.

---

## Troubleshooting

| Symptom | Likely cause & fix |
| --- | --- |
| `docker compose up` fails with `network signoz-network not found` | Foundry isn't running yet. Run `docker compose -f pours/deployment/compose.yaml up -d` from the foundry root first. |
| App starts but nothing appears in SigNoz | Check `OTEL_EXPORTER_OTLP_ENDPOINT`. The OTel SDK buffers and retries silently if the endpoint is wrong. Check app logs: `docker compose logs app`. |
| SigNoz UI shows no service after sending requests | The ingester may still be starting. Wait ~30 s and resend a request. Check ingester logs: `docker compose -f pours/deployment/compose.yaml logs ingester`. |
| `mage: command not found` | Run `go install github.com/magefile/mage@latest` and ensure `$(go env GOPATH)/bin` is on `PATH`. |
| `curl: (7) Failed to connect to localhost port 8090` | The app container isn't up. Check `docker compose ps` and `docker compose logs app`. |
| SigNoz UI not reachable at `:8080` | Check Foundry: `docker compose -f pours/deployment/compose.yaml ps signoz-signoz-0`. It may still be doing its 60 s startup health check. |

---

## Cleanup

```bash
# Stop the Order Service
docker compose down

# Wipe the app's SQLite data volume
docker volume rm signoz-demo_app-data

# Stop SigNoz (Foundry) — run from the foundry root
docker compose -f pours/deployment/compose.yaml down

# Wipe SigNoz data volumes (destructive — removes all traces/metrics/logs)
docker volume rm signoz-metastore-postgres-0-data \
                 signoz-telemetrykeeper-0-data \
                 signoz-telemetrystore-0-0-data \
                 signoz-telemetrystore-user-scripts

# Local (non-Docker) build artefacts
rm -rf dist/ data/
```

---

## Simplifications (vs. a real production service)

- Single SQLite file — no connection pool tuning, no read replicas.
- No authentication or authorization on the HTTP API.
- No pagination on `GET /api/v1/orders`.
- `cmd/loadgen` is a demo driver, not a load-testing framework — no ramp-up,
  no percentile reporting beyond what SigNoz shows.
- TLS is disabled for OTLP (`OTEL_EXPORTER_OTLP_INSECURE=true`) — always use
  TLS in production.

---

## Further reading

- [`docs/FULL_ARCHITECTURE.md`](docs/FULL_ARCHITECTURE.md) — full SigNoz
  architecture breakdown, all five Mermaid diagrams in one place.
- [`docs/signoz-architecture.md`](docs/signoz-architecture.md) — detailed
  component-by-component breakdown.
- [`docs/AI_AGENT_IDEAS_FOR_SIGNOZ.md`](docs/AI_AGENT_IDEAS_FOR_SIGNOZ.md) —
  brainstorm ideas for AI agents integrating with SigNoz.
- [`docs/PRIORITY_AGENTS_FOR_K0S_SIGNOZ_CLI.md`](docs/PRIORITY_AGENTS_FOR_K0S_SIGNOZ_CLI.md) —
  prioritized shortlist of agents to build with a Golang CLI + k0s.
