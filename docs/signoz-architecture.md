# SigNoz Architecture — Verified from Source

This document is Phase 1/2 of the hackathon research: a component-by-component
breakdown of how SigNoz actually works, verified against the repository at
`github.com/SigNoz/signoz` (module `github.com/SigNoz/signoz`, `go.mod` pinned to
Go 1.25.7, commit checked out locally at the time of writing).

Every claim below is tagged:

- **[SOURCE]** — verified directly by reading the file/directory named.
- **[DEMO]** — how *our* `signoz-demo` project uses/simplifies this.
- **[SIMPLIFICATION]** — a deliberate simplification for teaching purposes.

> **This is not the SigNoz architecture from older blog posts.** Older articles
> describe a `docker-compose.yaml` at the repo root with a standalone
> `query-service` container, a separate `frontend` (nginx) container, and a
> separate `alertmanager` container. That layout is gone from the current
> repository. Self-hosting today is documented as **Foundry, Docker,
> Kubernetes, or Linux** (`README.md`, "Self-host SigNoz" section), and the
> Go side has been consolidated into **one binary** (`pkg/signoz`) built from
> `cmd/community` (open-source) or `cmd/enterprise` (adds licensed modules
> from `ee/`). The OTel Collector remains a separate process/container — it
> is not embedded in the Go binary. **[SOURCE]**

---

## 1. Repository directory map

| Path | What it is | Why it matters |
| --- | --- | --- |
| `cmd/community/main.go`, `server.go` | Entry point for the OSS binary. Registers the `server`, `generate`, and `metastore` CLI commands, then calls `cmd.Execute`. | Confirms SigNoz OSS ships as **one Go binary**, not micro-per-signal services. |
| `cmd/enterprise/` | Entry point for the licensed build; wires the same `pkg/signoz.New(...)` constructor with EE-only factory callbacks (`ee/modules/*`, `ee/sqlstore/postgressqlstore`, `ee/licensing`, …). | Shows OSS vs. Enterprise is a **compile-time factory swap**, not a different codebase fork. |
| `pkg/signoz/signoz.go` | The `SigNoz` struct — the dependency-injection root. Every subsystem (`SQLStore`, `TelemetryStore`, `Querier`, `Alertmanager`, `Ruler`, `Gateway`, `Web`, `APIServer`, …) is a field populated by a `factory.ProviderFactory`. | This **is** the architecture diagram in code form — Diagram 4 below is a direct transcription of this struct's fields and their constructors. |
| `pkg/factory/` | The provider/factory DI framework used everywhere (`NewProviderFromNamedMap`, `ProviderSettings`, `ConfigFactory`). | Explains *how* SigNoz wires ~40 subsystems without a global `init()` soup — each subsystem is registered by name and constructed lazily from config. |
| `pkg/querier/` | Query engine: `builder_query.go` (SigNoz's own query-builder DSL → ClickHouse SQL), `promql_query.go` (PromQL → ClickHouse translation), `postprocess_test.go`. | This is what dashboards, alerts, and the Query Builder UI actually call — one query engine serving all three signal types. |
| `pkg/telemetrystore/` + `pkg/telemetrystore/clickhousetelemetrystore/` | Abstraction over the **telemetry** database (ClickHouse). Separate from `pkg/sqlstore`. | Confirms the two-database split: metadata vs. telemetry (Diagram 4). |
| `pkg/sqlstore/` (`bun.go`, `sqlitesqlstore/`) + `ee/sqlstore/postgressqlstore/` | Abstraction over the **metadata** database (users, orgs, dashboards, alert rules, API keys, saved views) using the `bun` ORM. SQLite ships in OSS; Postgres is an Enterprise/self-hosted option. | This is *not* where traces/metrics/logs live — a common point of confusion. |
| `pkg/telemetrytraces/`, `pkg/telemetrymetrics/`, `pkg/telemetrylogs/`, `pkg/telemetrymeter/` | Per-signal field mappers and query-builder glue (how a logical field like `http.status_code` maps to a physical ClickHouse column). | Where a new attribute/semantic-convention field gets wired into the Query Builder. |
| `pkg/ruler/` + `pkg/alertmanager/` | `ruler` evaluates alert rules (reads from `querier`), `alertmanager` (+ `nfmanager` for notification routing) sends notifications. Rule/route config is persisted via `sqlalertmanagerstore` / `sqlroutingstore` — i.e., in the **metadata** store, not ClickHouse. | Alerting is two cooperating subsystems, not one. |
| `pkg/authn/`, `pkg/authz/`, `pkg/modules/user`, `pkg/modules/organization` | Authentication (password/SSO providers), authorization (RBAC), user/org domain modules. | Auth is modular — `authNsCallback`/`authzCallback` in `signoz.New(...)` let Enterprise swap in SSO/RBAC providers without touching OSS code. |
| `pkg/web/` (`routerweb/`) | Serves the built `frontend/` static assets and mounts them behind `apiserver`'s router; `Config.Directory`/`Config.Index` point at the compiled SPA. | Frontend and API are served from the **same process/port** in production, not two containers. |
| `pkg/apiserver/` | Cross-cutting HTTP middleware: per-route request timeout (`config.Timeout`, default 60s / max 600s, with `/logs/tail` etc. excluded for streaming) and request logging exclusions. | Explains why `/api/v1/logs/tail` and `/api/v3/logs/livetail` behave differently (they're SSE/long-poll, exempted from the timeout). |
| `pkg/gateway/` | Proxies **ingestion-key** management calls to SigNoz Cloud's Zeus backend — this is a SaaS-integration surface, not a general request gateway for self-hosted OSS. | Don't confuse this with an API gateway pattern — self-hosted OSS doesn't need it for basic ingestion. |
| `pkg/query-service/app/services/map.go` | Service Map computation — still lives under the legacy `query-service` package path even though the binary itself is unified. | Service Map is derived from span parent/child + `service.name`/`peer.service` relationships already in ClickHouse traces, not a separately stored graph. |
| `pkg/instrumentation/` | SigNoz's *own* OpenTelemetry setup (it dogfoods itself) — logger, tracer, meter construction from `instrumentation.Config`. | Directly comparable to what our Go demo does in `pkg/observability` — same OTel SDK initialization pattern. |
| `conf/example.yaml` | The single source of truth for every subsystem's config keys (`global.external_url`, `instrumentation.*`, `sqlstore.sqlite.*`, `statsreporter.url`, …). Confirms **default `external_url: http://localhost:8080`** — i.e., the unified binary listens on **8080** by default. | Use this file to find any config key without guessing. |
| `.devenv/docker/` (`clickhouse/`, `postgres/`, `signoz-otel-collector/`) | **This is the real current local-dev stack**, used by SigNoz's own contributors — not the old root `docker-compose.yaml`. Three `compose.yaml` fragments: ClickHouse + Zookeeper + a migrator job, Postgres (optional metadata store for enterprise-style dev), and the `signoz-otel-collector` container with its `otel-collector-config.yaml`. | Our `docker-compose.yml` (Phase 3) deliberately mirrors this shape for the demo's SigNoz-side services. |
| `deploy/` | Only `install.sh`, `README.md`, `MIGRATION.md` today — no bundled compose file. **[SOURCE]** `deploy/README.md` states verbatim: *"the `install.sh` script and the `docker-compose` manifests have been deprecated... SigNoz now installs and runs through [Foundry]."* Running `deploy/install.sh` itself does nothing but print that deprecation notice and exit 0 — it no longer installs anything. | This is a genuine, hands-on discovery, not something carried over from older tutorials: **the classic root `docker-compose.yaml` self-host method is gone**, replaced by a separate tool, Foundry (`github.com/SigNoz/foundry`). Any blog/tutorial (including older ones) instructing `git clone signoz && cd deploy/docker && docker compose up` is describing a deprecated flow. |

---

## 2. Component-by-component

### 2.1 signoz-otel-collector (ingestion)

- **What it does**: receives OTLP traces/metrics/logs, computes span→metrics
  (RED metrics: rate/errors/duration) via the `signozspanmetrics` processor,
  batches, and writes to ClickHouse. **[SOURCE: `.devenv/docker/signoz-otel-collector/otel-collector-config.yaml`]**
- **Where**: not in this Go module at all — it's a separate binary/image
  (`signoz/signoz-otel-collector:v0.14x`), a SigNoz-maintained distribution of
  the upstream OpenTelemetry Collector with custom exporters
  (`clickhousetraces`, `signozclickhousemetrics`, `clickhouselogsexporter`)
  and a custom processor (`signozspanmetrics`).
- **Protocols/ports**: OTLP/gRPC `4317`, OTLP/HTTP `4318`, health-check
  extension `13133`, pprof `1777`; also self-scrapes its own Prometheus
  metrics on `8888` via the `prometheus` receiver.
- **Reads/writes**: reads OTLP from instrumented apps; writes to three
  ClickHouse databases — `signoz_traces`, `signoz_metrics`, `signoz_logs`.
- **Sync/async**: the OTLP receiver ack's synchronously per batch (gRPC/HTTP
  request-response), but the pipeline itself is async/buffered (`batch`
  processor, `send_batch_size: 10000`, `timeout: 10s`).
- **Schema migrations**: the same collector binary doubles as a migration
  tool — `migrate bootstrap && migrate sync up && migrate async up` — run
  once against ClickHouse before the collector starts serving traffic
  (`telemetrystore-migrator` service in `.devenv/docker/clickhouse/compose.yaml`).

### 2.2 ClickHouse (telemetry storage)

- **What it does**: columnar store for all telemetry. Chosen for
  high-cardinality, high-volume analytical queries (README's own comparison
  to Elastic/Loki cites this). **[SOURCE: root `README.md`]**
- **Where**: `pkg/telemetrystore/clickhousetelemetrystore/`.
- **Ports**: HTTP `8123`, native protocol `9000`.
- **Coordination**: `zookeeper` (image `signoz/zookeeper:3.7.1`) is a
  dependency of ClickHouse for replicated/clustered tables even in the
  single-node dev compose (`SIGNOZ_OTEL_COLLECTOR_CLICKHOUSE_REPLICATION=true`).
- **Extra**: a `histogramQuantile` user-defined function binary is fetched
  and mounted into ClickHouse's `user_scripts` — used for latency
  percentile queries server-side.

### 2.3 SQLStore (metadata storage)

- **What it does**: stores everything that is *not* telemetry — users,
  organizations, dashboards (definitions, not data), alert rules,
  notification routes, API keys, saved views, service accounts.
  **[SOURCE: `pkg/sqlstore/bun.go`, `pkg/sqlstore/sqlitesqlstore/`, `ee/sqlstore/postgressqlstore/`]**
- **Where**: SQLite by default in the OSS binary; Postgres available as the
  Enterprise/self-hosted metadata backend (`.devenv/docker/postgres/compose.yaml`
  runs Postgres 15 for that dev path).
- **This split is easy to miss**: a request that "saves a dashboard" writes
  JSON into SQLStore; a request that "runs a dashboard panel" queries
  TelemetryStore (ClickHouse) through `pkg/querier`. Two different
  databases, one HTTP request.

### 2.4 Querier (query engine)

- **What it does**: single query engine behind dashboards, alerts, and the
  Query Builder UI. Accepts either SigNoz's builder-query DSL
  (`builder_query.go`) or raw PromQL (`promql_query.go`, `promql_query_parser.go`)
  and translates both into ClickHouse SQL against the three telemetry
  databases. **[SOURCE: `pkg/querier/`]**
- **Field mapping**: `pkg/telemetrytraces`, `pkg/telemetrymetrics`,
  `pkg/telemetrylogs` each provide a `field_mapper.go` translating a
  logical/semantic field name to the physical ClickHouse column + table for
  that signal.

### 2.5 Alerting (Ruler + Alertmanager)

- **Ruler** (`pkg/ruler/`) periodically evaluates alert rule queries through
  `Querier` and fires alert state transitions.
- **Alertmanager** (`pkg/alertmanager/` + `nfmanager` for notification
  routing) receives fired alerts and dispatches notifications (email, Slack,
  webhook, etc.); its config/state is persisted via `sqlalertmanagerstore`
  and `sqlroutingstore` — i.e. in **SQLStore**, not ClickHouse.
- **Sync/async**: rule evaluation is a background scheduler (async, polling
  cadence); notification dispatch is fire-and-forget async delivery.

### 2.6 Dashboards, Service Map, Exceptions

- **Dashboards**: `pkg/modules/dashboard` (module) stores dashboard JSON via
  SQLStore; rendering panel data goes through `Querier` → ClickHouse.
- **Service Map**: `pkg/query-service/app/services/map.go` derives the
  service dependency graph from span `service.name` and parent/child/peer
  relationships already present in `signoz_traces` — it is a *query*, not a
  separately maintained graph store.
- **Exceptions**: modeled as span events on traces (OTel's own
  `exception.type`/`exception.message`/`exception.stacktrace` semantic
  conventions) queried back out via `pkg/querier` — there is no separate
  "exceptions" database table family; it rides on the traces pipeline.

### 2.7 Auth & Authorization

- `pkg/authn` — pluggable authentication providers (`authNsCallback` lets
  Enterprise register SSO providers on top of OSS password auth).
- `pkg/authz` — RBAC provider, also swappable via a callback in
  `signoz.New(...)` for Enterprise policy engines.
- `pkg/modules/user`, `pkg/modules/organization` — the domain modules for
  user/org lifecycle that authn/authz operate over.

### 2.8 API server + Web (frontend)

- `pkg/apiserver` is cross-cutting HTTP middleware only (timeout policy,
  logging exclusions) — the actual route handlers live per-module
  (`pkg/modules/*`, `pkg/query-service/app`).
- `pkg/web` serves the compiled `frontend/` SPA (`Config.Directory`,
  `Config.Index`) from the **same process** as the API, behind the same
  port. Default external URL: `http://localhost:8080`
  (`conf/example.yaml`, `global.external_url`).
- **Sync**: this is a conventional synchronous HTTP request/response
  server, except for long-lived routes explicitly excluded from the
  timeout middleware (`/api/v1/logs/tail`, `/api/v3/logs/livetail`,
  `/api/v1/export_raw_data`) which stream.

### 2.9 Deployment topology (current)

For the OSS binary itself: `cmd/community` → `pkg/signoz.New(...)` wires one
process exposing API + Web on `:8080`, talking to:

1. **SQLStore** (SQLite file, or Postgres) for metadata — synchronous, direct
   DB driver calls.
2. **TelemetryStore** (ClickHouse over the native `9000` protocol) for
   telemetry queries — synchronous per-request, but the *ingestion* side
   (via the separate collector) is async/batched.

The **signoz-otel-collector** is a separate deployable unit in every
topology (Docker, Kubernetes, Linux, or local dev) — it is never compiled
into the `signoz` binary. **[SOURCE: confirmed by `go.mod` having no
opentelemetry-collector-core dependency wired into `cmd/community`, and by
`.devenv/docker/signoz-otel-collector/compose.yaml` running it as its own
container with its own versioned image tag, decoupled from the SigNoz app
binary's version.]**

---

## 2.10 The self-hosting method changed under our feet — a real finding

While building this demo we initially expected (like most existing SigNoz
blog posts) to `docker compose up` a root-level compose file bundling
ClickHouse + the collector + the SigNoz app + the frontend. That file does
not exist in the current repository. Instead:

- `deploy/install.sh` — the script every older tutorial tells you to
  `curl | bash` — **now only prints a deprecation notice and exits**:
  *"This install script has been deprecated and is no longer maintained...
  Please follow the latest installation instructions here:
  https://signoz.io/docs/install/docker/"* **[SOURCE: `deploy/install.sh`]**
- `deploy/README.md` confirms the replacement is **Foundry**
  (`github.com/SigNoz/foundry`), with a `MIGRATION.md` in the same folder
  for teams moving an existing Compose deployment over. **[SOURCE]**
- The **only** docker-compose fragments still living in this repository are
  under `.devenv/docker/` — and those are explicitly the **contributor dev
  environment**: they stand up ClickHouse + Zookeeper + the
  signoz-otel-collector in Docker, while the SigNoz Go binary itself is run
  natively on the host (`go run ./cmd/community server`) so a contributor
  can iterate on it with a debugger attached. There is no `.devenv` service
  for the SigNoz app/UI itself. **[SOURCE]**

**What this means for our demo**: this repo's own `docker-compose.yml`
honestly mirrors what actually exists in the SigNoz source today — it
stands up ClickHouse + Zookeeper + the signoz-otel-collector (copied
faithfully from `.devenv/docker/`) as the *telemetry backend* for our
Golang service to send data to. For the SigNoz **application** (querier +
API + UI) itself, we point readers at the current official method —
Foundry, per `signoz.io/docs/install/docker/` — rather than inventing an
unverified Docker image name/tag for it. This is the one place in this
project where we deliberately stop short of fully automating the stack,
specifically because doing otherwise would mean silently reproducing a
deprecated pattern. **[DEMO]**

---

## 3. What's DEMO vs. SIMPLIFICATION in this repository

- **[DEMO]** We point our Go service's OTel SDK at an OTLP endpoint exactly
  like `.devenv/docker/signoz-otel-collector/compose.yaml` does — same ports
  (4317/4318), same collector image family.
- **[SIMPLIFICATION]** Our app's own persistence (the `orders` table) uses
  **SQLite**, per this project's own scope decision — this has nothing to do
  with SigNoz's SQLite metadata store; it is our demo microservice's business
  data, chosen for zero external DB dependency in a teaching repo. Don't
  conflate the two SQLite usages.
- **[SIMPLIFICATION]** We run a single self-hosted SigNoz instance
  (community, no auth SSO, no clustering) — production deployments would use
  Foundry/Kubernetes/Enterprise features not exercised here.
