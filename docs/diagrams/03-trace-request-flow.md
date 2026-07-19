# Diagram 3 — Trace Request Flow (this demo's Order Service)

A concrete `POST /api/v1/orders` request, showing the hexagonal layers each
span represents and how the trace context propagates to SigNoz.

```mermaid
sequenceDiagram
    participant C as Client
    participant H as HTTP Adapter<br/>(internal/adapters/http)
    participant U as Use Case<br/>(internal/application)
    participant R as Repository Adapter<br/>(internal/adapters/sqlite)
    participant DB as SQLite
    participant OTel as OTel SDK<br/>(TracerProvider)
    participant Col as SigNoz OTel Collector

    C->>H: POST /api/v1/orders
    activate H
    H->>OTel: start span "POST /api/v1/orders"<br/>(root, http.method, http.route)
    H->>U: CreateOrder(ctx, cmd)
    activate U
    U->>OTel: start child span "OrderService.CreateOrder"<br/>(order attributes, no PII)
    U->>R: Save(ctx, order)
    activate R
    R->>OTel: start child span "sqlite.INSERT orders"<br/>(db.system=sqlite, db.operation=INSERT)
    R->>DB: INSERT INTO orders (...)
    DB-->>R: rows affected / error
    R->>OTel: end span (status: OK or ERROR)
    deactivate R
    R-->>U: order, err
    U->>OTel: end span
    deactivate U
    U-->>H: result, err
    H->>OTel: end root span (http.status_code)
    deactivate H
    H-->>C: 201 Created / 4xx / 5xx

    OTel-->>Col: batched spans exported via OTLP/gRPC :4317
    Note over OTel,Col: context propagation keeps all 3 spans<br/>under one trace_id; parent/child links<br/>let SigNoz render the waterfall.
```

**What this demonstrates**

- Parent/child spans: HTTP → Use Case → Repository, one `trace_id`.
- Context propagation: `context.Context` carrying the active span is passed
  explicitly through every layer — the domain layer itself never imports
  `context` for tracing purposes, only the adapters do (see
  `docs/diagrams/04-golang-hexagonal-architecture.md`).
- Error spans: a failed `INSERT` (e.g. duplicate ID, closed DB) sets
  `span.SetStatus(codes.Error, ...)` and records the error as a span event.
- Slow requests / DB latency: an artificial `internal/adapters/sqlite`
  delay (used only by the `slow` demo scenario) shows up as extra duration
  on the `sqlite.INSERT orders` span specifically — not the HTTP span —
  which is exactly the debugging signal the blog post walks through.
