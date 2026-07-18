# Diagram 4 — SigNoz Internal Component Architecture (current repo)

Transcribed directly from `pkg/signoz/signoz.go`'s `SigNoz` struct and the
factories it wires in `signoz.New(...)`. Component names are the actual Go
package/struct names — nothing invented.

```mermaid
flowchart TB
    subgraph Binary["cmd/community (single Go binary, EE via cmd/enterprise)"]
        SN["pkg/signoz.SigNoz<br/>(DI root, factory.Registry)"]
    end

    SN --> INSTR["Instrumentation<br/>(pkg/instrumentation)<br/>logger/tracer/meter for SigNoz itself"]
    SN --> APISRV["APIServer<br/>(pkg/apiserver)<br/>timeout + logging middleware"]
    SN --> WEB["Web<br/>(pkg/web)<br/>serves frontend/ SPA"]
    SN --> SQLS["SQLStore<br/>(pkg/sqlstore: sqlite | ee: postgres)<br/>bun ORM"]
    SN --> TS["TelemetryStore<br/>(pkg/telemetrystore/clickhousetelemetrystore)"]
    SN --> META["TelemetryMetadataStore<br/>(pkg/types/telemetrytypes)"]
    SN --> Q["Querier<br/>(pkg/querier)<br/>builder-query + PromQL -> ClickHouse SQL"]
    SN --> RULER["Ruler<br/>(pkg/ruler)"]
    SN --> AM["Alertmanager<br/>(pkg/alertmanager + nfmanager)"]
    SN --> AUTHN["Authn<br/>(pkg/authn)"]
    SN --> AUTHZ["Authz<br/>(pkg/authz)"]
    SN --> LIC["Licensing<br/>(pkg/licensing, EE-gated)"]
    SN --> GW["Gateway<br/>(pkg/gateway)<br/>SigNoz Cloud ingestion-key proxy"]
    SN --> MODS["Modules<br/>(pkg/modules/*)<br/>dashboard, user, organization,<br/>tag, retention, savedview, ..."]
    SN --> CACHE["Cache<br/>(pkg/cache)"]
    SN --> PROM["Prometheus<br/>(pkg/prometheus)"]
    SN --> ZEUS["Zeus<br/>(pkg/zeus)<br/>SigNoz Cloud control-plane client"]

    RULER -->|"evaluates rule queries via"| Q
    AM -->|"rule/route config persisted in"| SQLS
    Q -->|"reads"| TS
    MODS -->|"dashboard/alert JSON persisted in"| SQLS
    APISRV -->|"mounts routes from"| MODS
    WEB -->|"same process, same :8080"| APISRV

    TS -->|"tcp :9000 (native)"| CH[("ClickHouse<br/>signoz_traces / signoz_metrics / signoz_logs")]
    SQLS -->|"file (sqlite) or tcp :5432 (postgres)"| METADB[("Metadata DB")]

    OTELCOL["signoz-otel-collector<br/>(separate process/container,<br/>own image + version)"]
    OTELCOL -->|"writes via native protocol"| CH

    APP["Instrumented apps<br/>(e.g. this demo's Golang service)"]
    APP -->|"OTLP :4317/:4318"| OTELCOL
```

**Why this matters for the blog**

- There is **no separate "query-service" microservice anymore** — that
  legacy package path (`pkg/query-service/app`) still contains some
  handlers (e.g. Service Map), but it's compiled into the same binary as
  everything else, not deployed separately.
- The **Collector is architecturally decoupled** from the SigNoz binary —
  different repo entirely (`signoz/signoz-otel-collector`), different
  release cadence, connected only via ClickHouse and OTLP.
- **Licensing/Gateway/Zeus** are SaaS-integration surfaces present even in
  the OSS binary (feature-gated), which is why `cmd/enterprise` is a thin
  wrapper rather than a separate codebase.
