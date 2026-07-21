# Debugging a Slow Golang Microservice with SigNoz and OpenTelemetry

*How I traced a 2-second API call from HTTP handler to SQLite — and pinpointed the exact layer that caused it.*

---

A request is slow in production.

Your logs say it happened. Your metrics say the p99 spiked. But neither tells you *where the time went* — was it the HTTP layer, your business logic, or the database call underneath it?

That's the exact question distributed tracing was built to answer. And instead of reading about it, I decided to build a small Golang service, instrument it properly with OpenTelemetry, deliberately inject a slowdown at one specific layer, and use a self-hosted SigNoz instance to find it — step by step, visually, in under a minute.

This post is the full walkthrough.

---

## What I Built

An **Order Service** in Go with three endpoints:
- `POST /api/v1/orders` — create an order
- `GET /api/v1/orders/{id}` — get a single order
- `GET /api/v1/orders` — list orders

Plus `/health` and `/ready`.

The architecture is deliberately **hexagonal**: business logic (`internal/domain`) has zero knowledge of HTTP, databases, or OpenTelemetry. Only the adapters around it do. This matters for instrumentation — because it forces a clean answer to the question "where does each span belong?"

```
internal/adapters/http   → otelhttp middleware, HTTP-shaped spans
internal/application     → the "use case" span (OrderService.CreateOrder)
internal/adapters/sqlite → the "repository" span (sqlite.INSERT orders)
internal/domain          → no tracing code at all
```

The stack:
- **Golang** service with OpenTelemetry SDK
- **SigNoz** (self-hosted, running in k3d via Helm)
- **ClickHouse** as the telemetry backend
- **k8s-infra** chart for Kubernetes pod log collection

---

## Self-Hosting SigNoz in 2026

Every tutorial I found said: clone the SigNoz repo, `cd deploy/docker`, `docker compose up`.

That doesn't work anymore. The current `deploy/install.sh` prints:

```
⚠️  This install script has been deprecated and is no longer maintained.
⚠️  Please see https://github.com/SigNoz/signoz/blob/main/deploy/README.md
    for new installation and migrations to Foundry.
```

The current path is **Foundry** — SigNoz's Helm-based deployment tool. That's what this project uses.

```bash
helm repo add signoz https://charts.signoz.io
helm upgrade --install signoz signoz/signoz \
  --namespace signoz --create-namespace \
  --values apps/local/signoz/values.yaml
```

The OTLP endpoint inside the cluster:
```
signoz-ingester.signoz.svc.cluster.local:4317  (gRPC)
signoz-ingester.signoz.svc.cluster.local:4318  (HTTP)
```

---

## How SigNoz Works (the non-obvious parts)

Before jumping to the demo, three things that surprised me reading the SigNoz source:

**1. One binary.** The querier, API, alerting, auth, and dashboards all compile into a single Go binary. Community vs. Enterprise is a compile-time factory swap.

**2. Two separate databases.** ClickHouse holds telemetry (traces/metrics/logs). A second store (SQLite or Postgres) holds metadata — users, dashboards, alert rules. A "save this dashboard" request and a "run this dashboard's query" request hit two different databases.

**3. RED metrics are free.** Rate/Error/Duration numbers in the Services tab don't require you to emit any metrics. The collector's `signozspanmetrics` processor derives them from the spans your app already sends for tracing.

---

## Instrumenting the Service

### SDK Setup

`pkg/observability/otel.go` sets up all three providers at startup:

```go
res, _ := resource.New(ctx,
    resource.WithAttributes(
        semconv.ServiceName(cfg.ServiceName),
        semconv.ServiceVersion(cfg.ServiceVersion),
        semconv.DeploymentEnvironmentNameKey.String(cfg.Environment),
    ),
    resource.WithFromEnv(), resource.WithHost(), resource.WithProcess(),
)

traceExporter, _ := otlptracegrpc.New(ctx,
    otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
    otlptracegrpc.WithInsecure(),
)
sdk.TracerProvider = sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(traceExporter, sdktrace.WithBatchTimeout(5*time.Second)),
    sdktrace.WithResource(res),
)
otel.SetTracerProvider(sdk.TracerProvider)
```

The `MeterProvider` and `LoggerProvider` follow the same pattern.

### Context Propagation — The Whole Mechanism

All three spans link into one trace because `ctx` is threaded through every function signature:

```go
// HTTP adapter creates the root span (otelhttp does this automatically)

// Application layer creates its own child span:
func (s *OrderService) CreateOrder(ctx context.Context, in ports.CreateOrderInput) (*domain.Order, error) {
    ctx, span := otel.Tracer(tracerName).Start(ctx, "OrderService.CreateOrder")
    defer span.End()
    // Pass ctx (now carrying this span) down to the repository:
    return s.repo.Save(ctx, newOrder)
}

// Repository layer creates its own child span from the same ctx:
func (r *Repository) Save(ctx context.Context, order *domain.Order) (*domain.Order, error) {
    ctx, span := otel.Tracer(tracerName).Start(ctx, "sqlite.INSERT orders")
    defer span.End()
    // ... SQLite INSERT
}
```

Drop `ctx` anywhere in that chain — call `context.Background()` inside the repository instead of using the one passed in — and the trace silently splits into two unrelated traces. Worth deliberately trying once to see what a broken trace looks like.

### Custom Metrics

`pkg/observability/metrics.go` adds what `otelhttp`'s automatic HTTP metrics don't cover:

- `orders_created_total` — counter
- `order_create_duration_seconds` — histogram
- `db_operation_duration_seconds` — histogram, tagged by `db.operation`
- `order_errors_total` — counter, tagged by `error_type`

### Logs with Trace Correlation

Logs fan out to two handlers: JSON stdout and the `otelslog` bridge (OTLP export). A small `traceContextHandler` wrapper reads the active span from `ctx` and stamps `trace_id`/`span_id` onto the stdout copy. The OTLP copy gets this automatically. Same IDs on both = trace↔log navigation works in SigNoz.

---

## The Four Load Scenarios

`cmd/loadgen` drives four scenarios via an `X-Demo-Scenario` HTTP header:

| Scenario | Layer affected | What it produces |
|----------|---------------|-----------------|
| `normal` | — | Fast, clean trace (~5ms) |
| `slow` | `sqlite` adapter, before INSERT | ~1.8s latency isolated to the DB span |
| `error` | Application layer, before repo call | Error span at use-case layer, **no** DB span |
| `db-fail` | `sqlite` adapter, instead of INSERT | Error span specifically at the DB layer |

```bash
mage loadgen:normal       # 20 healthy requests
mage loadgen:slow         # requests with injected SQLite latency
mage loadgen:errors       # mixed use-case and DB-layer failures
mage loadgen:concurrent   # 60 requests, 10 concurrent workers
```

---

## The Debugging Story — Step by Step

Here's what the investigation looks like after running all four load generators. This is the actual path an on-call engineer would follow.

### Step 1 — The Services Overview: "Something is wrong"

The first screen you see in SigNoz after logging in.

![SigNoz Services Overview — signoz-demo-order-service with visible p99 spike and non-zero error rate](Screenshot 2026-07-19 at 4.45.39 PM.png)
*SigNoz Services tab after running all four load scenarios. The `signoz-demo-order-service` row shows a p99 latency well above its p50, and a non-zero error rate — the "something is wrong" signal.*

![Services Overview — latency column detail](Screenshot 2026-07-19 at 4.45.50 PM.png)
*Zooming in on the latency columns: p50 is in single-digit milliseconds; p99 is orders of magnitude higher. This tells you the problem is not "all requests are slow" — it's a subset.*

![Services Overview — error rate column](Screenshot 2026-07-19 at 4.45.53 PM.png)
*The error rate column confirms failures occurred. At this point you know: latency problem AND error problem, both on the same service.*

### Step 2 — Drilling Into Traces: "Which request is responsible?"

Click the service name to jump into the Traces explorer.

![Traces list — sorted by duration descending](Screenshot 2026-07-19 at 4.45.56 PM.png)
*Traces sorted by duration descending. One `POST /api/v1/orders` entry sits at ~1.8–2.0s. Everything else is in the tens of milliseconds. This is the request to open.*

![Traces list — duration distribution visible](Screenshot 2026-07-19 at 4.45.59 PM.png)
*The duration distribution makes the outlier visually obvious. This is not a gradual degradation — it's a specific scenario that produced a specific slow request.*

![Traces list — filter by service](Screenshot 2026-07-19 at 4.46.11 PM.png)
*Filtering to `signoz-demo-order-service` and `POST /api/v1/orders` narrows the list to just the relevant traces.*

### Step 3 — Opening the Slow Trace: "Where did the time go?"

Click the slow trace to open the waterfall view.

![Trace waterfall — full view](Screenshot 2026-07-19 at 4.46.15 PM.png)
*The waterfall view for the slow request. Three spans stacked: `POST /api/v1/orders` at the top, `OrderService.CreateOrder` beneath it, and `sqlite.INSERT orders` at the bottom. All three are nearly the same width.*

![Trace waterfall — span durations annotated](Screenshot 2026-07-19 at 4.46.27 PM.png)
*This is the key view. The `sqlite.INSERT orders` span is 1.8 seconds wide — and it has no children. Its own duration, not a child's, accounts for essentially all of the total request time.*

![Trace waterfall — HTTP and use-case spans](Screenshot 2026-07-19 at 4.46.34 PM.png)
*The HTTP span and use-case span appear slow only because they're waiting on their child. Their own execution time (exclusive duration) is negligible. This is the distinction a waterfall gives you that a single "request took 2s" log cannot.*

### Step 4 — Clicking the SQLite Span: "Exactly what was slow?"

Click the `sqlite.INSERT orders` span.

![SQLite span — attributes panel](Screenshot 2026-07-19 at 4.46.36 PM.png)
*The span attributes panel for `sqlite.INSERT orders`. Shows `db.system=sqlite`, `db.operation=INSERT`, `db.sql.table=orders`, and the span's own isolated duration.*

![SQLite span — duration isolated](Screenshot 2026-07-19 at 4.46.41 PM.png)
*The span duration is isolated from its parent. This confirms: the slowness is in the database adapter specifically, not in the HTTP handler or the business logic that wraps it.*

![SQLite span — full attributes list](Screenshot 2026-07-19 at 4.46.45 PM.png)
*Additional attributes on the span: `order.quantity`, `db.sql.table`. Low-cardinality by design — no customer PII, no free-text fields. Keeping attributes low-cardinality is what makes Query Builder aggregations usable.*

### Step 5 — Error Traces: "What does a failure look like?"

Back to the traces list, this time filtering for error spans.

![Error trace — use-case layer error](Screenshot 2026-07-19 at 4.47.50 PM.png)
*An `error` scenario trace. The error span is at the `OrderService.CreateOrder` level — notice there is **no** `sqlite.INSERT orders` span beneath it. The failure happened before the database was touched.*

![Error trace — DB-fail scenario](Screenshot 2026-07-19 at 4.48.00 PM.png)
*A `db-fail` scenario trace. This time the error span IS at the `sqlite.INSERT orders` level. The use-case span completed successfully and delegated to the repository before the failure occurred.*

![Error traces — side by side comparison](Screenshot 2026-07-19 at 4.48.23 PM.png)
*This distinction — error at the use-case layer vs. error at the DB layer — is a production-critical question: did the failure happen before or after we touched the dependency? The answer determines whether you look at your own code or the dependency's status page first.*

### Step 6 — The Custom Dashboard: "Business metrics at a glance"

The provisioned Order Service Overview dashboard.

![Order Service Overview dashboard](Screenshot 2026-07-19 at 4.48.32 PM.png)
*The provisioned dashboard showing: request rate (RPS), error rate (%), P95 latency, P50 latency, orders created total, order errors by type, and SQLite operation duration by operation type.*

![Dashboard — latency panels](Screenshot 2026-07-19 at 4.48.34 PM.png)
*P95 vs P50 latency side by side. The spike from the slow scenario is clearly visible in P95 while P50 barely moves — confirming the problem affects a minority of requests, not all of them.*

### Step 7 — Correlated Logs: "The same request, in logs"

From the slow trace, use "Related Logs" to jump to the log line for that exact request.

![Logs Explorer — trace_id search](Screenshot 2026-07-19 at 4.49.01 PM.png)
*The Logs Explorer, filtered by the `trace_id` from the slow trace. One `http request` log line appears.*

![Logs Explorer — log line with trace context](Screenshot 2026-07-19 at 4.49.03 PM.png)
*The log line shows `http.status_code=201`, `duration` matching the trace (~1.8s), and the same `trace_id`/`span_id` as the span. This confirms — rather than guesses — that this log line and this trace describe the same request.*

![Logs Explorer — attributes panel](Screenshot 2026-07-19 at 4.49.09 PM.png)
*Expanding the log line's attributes. The structured fields include request metadata, response code, duration, and the trace correlation IDs that link it back to the waterfall.*

![Logs Explorer — Kubernetes attributes](Screenshot 2026-07-19 at 4.49.11 PM.png)
*The k8s-infra chart enriches every log with Kubernetes metadata: `k8s.namespace.name`, `k8s.pod.name`, `k8s.container.name`, `k8s.deployment.name`. These come from the `k8sattributes` processor — no code changes required in the application.*

### Step 8 — Kubernetes Pod Logs: "Infrastructure visibility"

The k8s-infra chart collects pod logs from all namespaces automatically.

![Kubernetes pod logs — namespace filter](Screenshot 2026-07-19 at 4.49.17 PM.png)
*Filtering by `k8s.namespace.name=signoz-demo` in the Logs Explorer. Every container in the `signoz-demo` namespace is visible here, collected by the OTel DaemonSet — no changes to the application's logging setup required.*

![Kubernetes pod logs — container filter](Screenshot 2026-07-19 at 4.49.18 PM.png)
*Drilling down to a specific container via `k8s.container.name`. This per-container view is what was missing before adding `k8s.container.name` to the `kubernetesAttributes.extractMetadatas` list in the k8s-infra values.*

![Kubernetes pod logs — full log stream](Screenshot 2026-07-19 at 4.50.24 PM.png)
*The complete log stream for the Order Service pod, including stdout JSON logs from the application and the OTel-enriched structured records. Both contain matching `trace_id`/`span_id` fields — same request, two sources, unified view.*

---

## What I Learned

**Span boundaries are a design decision.** Putting a span around the use-case that wraps the repository call — not just around the HTTP handler — is what made "HTTP vs. business logic vs. database" a visual distinction in the waterfall. If you only instrument the handler, you get one span that says "this was slow" but not where.

**Context propagation is the whole mechanism.** Every span in this service links to its parent because `ctx` was threaded through function signatures correctly. This isn't magic — it's plumbing. Anywhere you break the chain, the trace breaks silently.

**RED metrics are a side effect of tracing.** The request rate, error rate, and duration percentiles in the Services tab came entirely from the spans that were already being emitted. The `signozspanmetrics` processor did the aggregation. Zero additional metrics code required for the service-level numbers.

**High-cardinality attributes are a real constraint.** It's tempting to add `customer_name` to every span. Keeping it to `order.quantity`, `db.operation`, etc. is what keeps the Query Builder usable. One series per customer is not a dashboard — it's noise.

**Logs are more useful correlated than searched.** Grepping for "slow" in stdout logs tells you it happened. Jumping from the slow trace to its log line via `trace_id` tells you it happened during *this specific request*, with the exact attributes that request carried. That's a different class of information.

---

## The Five-Step Debugging Path

After running through this experiment a few times, the path becomes mechanical — and that's the point:

1. **Services overview** → which service, and is it latency or errors (or both)?
2. **Traces, sorted by duration** → which specific request is the outlier?
3. **Waterfall** → which span is wide? Is the width from the span's own work or a child?
4. **Span attributes** → what was the span doing? What layer, what operation, what parameters?
5. **Related logs** → same `trace_id` → same request → confirm with structured fields

New team members can follow this path without knowing the codebase. That's the actual value of distributed tracing — not "more data," but a repeatable path from "something is slow" to "this layer of this specific request is why."

---

## Architecture Note: Self-Hosting in 2026

If you're writing about SigNoz, verify against the current source. The move from bundled docker-compose to Foundry, and from a separate query-service binary to one unified Go binary, are both real, verifiable changes. Anyone following older tutorials will hit dead ends immediately.

The current installation path:
```bash
helm repo add signoz https://charts.signoz.io
helm upgrade --install signoz signoz/signoz --namespace signoz --create-namespace
```

For Kubernetes pod log collection (the k8s-infra chart):
```bash
helm upgrade --install k8s-infra signoz/k8s-infra \
  --namespace k8s-infra --create-namespace \
  --set otelCollectorEndpoint=http://signoz-ingester.signoz.svc.cluster.local:4318
```

One non-obvious detail: the k8s-infra chart's default pipeline uses the `otlphttp` exporter, which needs **port 4318** (HTTP), not 4317 (gRPC). And add `k8s.container.name` to `presets.kubernetesAttributes.extractMetadatas` — without it, per-container filtering in the Logs dashboard doesn't work.

---

## Source Code

Everything in this post is from a real, runnable project:

- **signoz-demo** — the Order Service, OTel instrumentation, load generators, k8s manifests
- **eh-fleets** — the k3d cluster setup, Helm values, Mage targets for the full demo stack

The full architecture breakdown (component by component, with file paths and ports) is in `docs/signoz-architecture.md`.

---

*If this was useful, the repo is at GitHub. All the Mage targets, Helm values, and OTel setup are there — runnable in a single `mage up && mage installSignoz && mage deploySignozDemo`.*
