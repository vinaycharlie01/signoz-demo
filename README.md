# signoz-demo

A small, production-shaped Golang **Order Service**, built with **Hexagonal
Architecture** (Ports & Adapters), instrumented end-to-end with
**OpenTelemetry**, and paired with a self-hosted **SigNoz** telemetry
backend — built as a hands-on companion project for studying SigNoz's
current architecture (see `docs/signoz-architecture.md`).

This is a teaching/demo repository, not a template for a real production
service — see "Simplifications" below for what was intentionally cut.

Also in this repo:

- [`docs/AI_AGENT_IDEAS_FOR_SIGNOZ.md`](docs/AI_AGENT_IDEAS_FOR_SIGNOZ.md) —
  1000 brainstorm ideas for AI agents that integrate with SigNoz, for the
  **Agents of SigNoz** hackathon.
- [`docs/PRIORITY_AGENTS_FOR_K0S_SIGNOZ_CLI.md`](docs/PRIORITY_AGENTS_FOR_K0S_SIGNOZ_CLI.md) —
  a narrow, prioritized shortlist of which agents to actually build first,
  scoped to a Golang CLI that deploys SigNoz on top of k0s with one command.

## Project overview

| | |
| --- | --- |
| Domain | Orders (`POST`/`GET /api/v1/orders`) |
| Architecture | Hexagonal — `internal/domain` has zero framework imports |
| Database | SQLite (`modernc.org/sqlite`, pure Go, no cgo) |
| Observability | OpenTelemetry traces, metrics, and logs, exported over OTLP/gRPC |
| Telemetry backend | Self-hosted SigNoz (ClickHouse + signoz-otel-collector + SigNoz app via Foundry) |
| Build tooling | [Mage](https://magefile.org/) + [nava](https://github.com/nirantaraai/nava) — **no Makefile, no shell scripts** |

## Architecture

```
Client
  │  HTTP :8090
  ▼
internal/adapters/http   (driving adapter — net/http + otelhttp)
  │  ports.OrderService
  ▼
internal/application     (use cases — OrderService)
  │  ports.OrderRepository
  ▼
internal/adapters/sqlite (driven adapter — database/sql + modernc.org/sqlite)
  │
  ▼
SQLite (orders.db)
```

`internal/domain` sits behind both ports and imports neither `net/http`,
`database/sql`, nor OpenTelemetry.

**Start here for the full SigNoz architecture**: [`docs/FULL_ARCHITECTURE.md`](docs/FULL_ARCHITECTURE.md)
is the single-document version — directory map, all five diagrams, and the
complete component-by-component breakdown in one place. The same material
also exists split across [`docs/signoz-architecture.md`](docs/signoz-architecture.md)
and the individual diagram files below, if you want the narrower version of
one piece.

All five Mermaid diagrams (standalone files):

- [`docs/diagrams/01-high-level-architecture.md`](docs/diagrams/01-high-level-architecture.md)
- [`docs/diagrams/02-telemetry-ingestion-pipeline.md`](docs/diagrams/02-telemetry-ingestion-pipeline.md)
- [`docs/diagrams/03-trace-request-flow.md`](docs/diagrams/03-trace-request-flow.md)
- [`docs/diagrams/04-signoz-internal-components.md`](docs/diagrams/04-signoz-internal-components.md)
- [`docs/diagrams/05-local-development-architecture.md`](docs/diagrams/05-local-development-architecture.md)

## Prerequisites

- Go 1.24+
- Docker + Docker Compose (for ClickHouse + the SigNoz OTel Collector)
- [Mage](https://magefile.org/): `go install github.com/magefile/mage@latest`
  (make sure `$(go env GOPATH)/bin` is on your `PATH`)
- **SigNoz itself** (the querier/API/UI), installed separately via
  [Foundry](https://signoz.io/docs/install/docker/) — see "SigNoz setup"
  below for why this isn't bundled into `docker-compose.yml`.

There is no Makefile in this repo. Every command below is a Mage target;
run `mage -l` at any time to see the full list.

## SigNoz setup

**Important, hands-on finding**: the classic "clone signoz, `docker compose
up` a root compose file" method you may know from older tutorials is
**deprecated**. SigNoz's own `deploy/install.sh` now just prints a
deprecation notice pointing at Foundry. See
`docs/signoz-architecture.md` §2.10 for the full story.

This repo's `docker-compose.yml` provisions the telemetry **backend**
(ClickHouse + the signoz-otel-collector) that our app sends data to. To get
the actual SigNoz UI on top of that data:

1. Install SigNoz via [Foundry](https://signoz.io/docs/install/docker/),
   following the current instructions for your platform.
2. Point it at the same ClickHouse this repo starts
   (`tcp://localhost:9000` once `mage docker:up` has run) instead of
   letting it create its own.
3. Open the SigNoz UI (default `http://localhost:8080`, per
   `conf/example.yaml`'s `global.external_url` in the SigNoz repo) once it's
   up.

If you'd rather not install Foundry, you can instead clone
[`SigNoz/signoz`](https://github.com/SigNoz/signoz) and run the backend
natively against the same ClickHouse, exactly like SigNoz's own
`.devenv/docker/` contributor workflow does:

```bash
git clone https://github.com/SigNoz/signoz
cd signoz
go run ./cmd/community server --config conf/example.yaml
# ensure sqlstore/telemetrystore config in your local conf points at
# tcp://localhost:9000 (the ClickHouse this repo's docker-compose.yml starts)
```

## Application setup

```bash
git clone <this-repo-url> signoz-demo
cd signoz-demo
go install github.com/magefile/mage@latest   # once, if you don't have mage
mage go:setup                                 # go mod download && go mod tidy
```

### Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `HTTP_ADDR` | `:8090` | Address the API listens on |
| `DB_PATH` | `./data/orders.db` | SQLite file path |
| `OTEL_SERVICE_NAME` | `signoz-demo-order-service` | `service.name` resource attribute |
| `SERVICE_VERSION` | `0.1.0` | `service.version` resource attribute |
| `DEPLOYMENT_ENVIRONMENT` | `local` | `deployment.environment.name` resource attribute |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTLP/gRPC endpoint (traces, metrics, and logs) |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Skip TLS for the OTLP connection (local dev only) |

## Running the stack

`Dockerfile` is runtime-only — it copies a prebuilt binary rather than
running `go build` itself, matching the convention this org's other Go
CLIs use (e.g. `sh-mcp-go`'s Dockerfile: UBI9-minimal base, non-root,
`COPY dist/linux_${TARGETARCH}/<binary>`). `mage docker:up`/`docker:build`
depend on `mage go:crossBuild`, which produces
`dist/linux_{amd64,arm64}/api` automatically — you don't need to run it
separately.

### Option A — everything in Docker

```bash
mage docker:up      # cross-builds the binary, builds the app image, starts clickhouse + signoz-otel-collector + app
curl http://localhost:8090/health
mage docker:down    # stop everything (data volumes are kept)
```

### Option B — app on the host, backend in Docker

```bash
mage docker:up                        # just clickhouse + signoz-otel-collector, if you edit docker-compose.yml to drop `app`
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 mage go:run
```

## Sending test requests

```bash
curl -X POST http://localhost:8090/api/v1/orders \
  -H 'Content-Type: application/json' \
  -d '{"customer_name":"Alice","item":"widget","quantity":2,"amount_cents":1999}'

curl http://localhost:8090/api/v1/orders
curl http://localhost:8090/api/v1/orders/<id-from-above>
curl http://localhost:8090/health
curl http://localhost:8090/ready
```

## Generating load / slow requests / errors

`cmd/loadgen` is this project's replacement for the usual `make load*`
shell scripts — a small Go program, wired into Mage via `loadgen-*.yaml`:

```bash
mage loadgen:normal       # 20 healthy sequential requests
mage loadgen:slow         # requests that hit the injected ~1.8s SQLite latency
mage loadgen:errors       # a mix of use-case-level and repository-level failures
mage loadgen:concurrent   # 60 requests, 10 concurrent workers, mixed scenarios
```

Under the hood these set the `X-Demo-Scenario` header
(`slow` / `error` / `db-fail`) — see `internal/demoscenario` and
`docs/diagrams/03-trace-request-flow.md` for exactly where each one takes
effect. You can also trigger a single one manually:

```bash
curl -X POST http://localhost:8090/api/v1/orders \
  -H 'Content-Type: application/json' -H 'X-Demo-Scenario: slow' \
  -d '{"customer_name":"Bob","item":"gadget","quantity":1,"amount_cents":999}'
```

## Viewing traces in SigNoz

1. Open the SigNoz UI → **Services** → `signoz-demo-order-service`.
2. Click into **Traces** and filter by `http.route=POST /api/v1/orders`.
3. Open a slow trace (run `mage loadgen:slow` first) and inspect the
   waterfall — you should see `POST /api/v1/orders` → `OrderService.CreateOrder`
   → `sqlite.INSERT orders`, with almost all the extra latency on the last
   span. This is the debugging story walked through in
   `docs/blog/debugging-slow-golang-api-with-signoz.md`.

## Viewing metrics

- **Services** overview shows p50/p90/p99 latency, request rate, and error
  rate — derived by the collector's `signozspanmetrics` processor directly
  from spans, no extra code needed.
- Custom metrics (Dashboards → New Panel → Metrics, or Query Builder):
  - `orders_created_total`
  - `order_create_duration_seconds`
  - `db_operation_duration_seconds` (filter by `db.operation`)
  - `order_errors_total`

## Viewing logs

Logs Explorer → filter `service.name = signoz-demo-order-service`. Every
request logs one structured `http request` line; failures also log an
`unhandled service error` line.

## Trace/log correlation

Every log record written while a span is active gets `trace_id`/`span_id`
attributes attached — see `pkg/observability/logger.go`'s
`traceContextHandler` for the stdout copy, and the `otelslog` bridge for the
OTLP-exported copy (same context, same IDs). In the SigNoz UI, open a trace
and use "Related logs" (or search Logs Explorer by that `trace_id`) to jump
straight to the log lines from that exact request — and vice versa from a
log line back to its trace.

## Troubleshooting

- **`mage: command not found`** — `go install github.com/magefile/mage@latest`
  and make sure `$(go env GOPATH)/bin` is on `PATH`.
- **App starts but nothing shows up in SigNoz** — check
  `OTEL_EXPORTER_OTLP_ENDPOINT` actually points at a reachable collector;
  the app itself never fails to start if the collector is unreachable (the
  OTLP exporter buffers/retries in the background), so a wrong endpoint
  fails silently unless you check the collector's own logs
  (`docker compose logs signoz-otel-collector`).
- **`signoz-otel-collector` container keeps restarting** — it runs
  `migrate sync check` before serving traffic; if ClickHouse isn't healthy
  yet or the `otel-collector-migrator` service hasn't completed, it will
  fail and retry. `docker compose logs otel-collector-migrator` first.
- **No SigNoz UI at `:8080`** — that's expected; this repo's
  `docker-compose.yml` only starts the telemetry backend, not the SigNoz
  app itself. See "SigNoz setup" above.

## Cleanup

```bash
mage docker:down                 # stop containers
docker volume rm signoz-demo_clickhouse-data signoz-demo_app-data  # wipe all data
rm -rf data/ bin/                # local (non-Docker) run artifacts
```

## Simplifications (vs. a real production service)

- Single SQLite file, no connection pool tuning, no read replicas.
- No authentication/authorization on the API.
- No pagination on `GET /api/v1/orders`.
- Single-node, non-replicated ClickHouse (no Zookeeper) — see
  `otel-collector-config.yaml`'s header comment.
- `cmd/loadgen` is a demo tool, not a load-testing framework — no ramp-up,
  no percentile reporting beyond what SigNoz itself shows you.

See `docs/signoz-architecture.md` for the full breakdown of what's verified
from SigNoz's source vs. what's a deliberate simplification in this repo.
