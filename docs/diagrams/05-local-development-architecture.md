# Diagram 5 — Local Development Architecture (this repo)

What actually runs on a laptop when you follow this repo's README: the demo
API + its own SQLite business database, plus a self-hosted SigNoz stack
modeled on `.devenv/docker/` from the SigNoz repo.

```mermaid
flowchart LR
    Client["Client<br/>(curl / cmd/loadgen)"]

    subgraph DemoApp["signoz-demo (this repo)"]
        API["Golang Demo API<br/>:8090"]
        SQLITE[("SQLite<br/>orders.db")]
        API <-->|"database/sql"| SQLITE
    end

    Client -->|"HTTP :8090"| API

    subgraph Observability["Self-hosted SigNoz (docker-compose)"]
        OTELSDK["OTel SDK<br/>(inside the Go process)"]
        COL["SigNoz OTel Collector<br/>:4317 gRPC / :4318 HTTP"]
        ZK["Zookeeper<br/>:2181"]
        CH[("ClickHouse<br/>:8123 / :9000")]
        SN["SigNoz Backend + UI<br/>:8080"]
        META[("SQLite metadata<br/>(SigNoz's own, separate<br/>from the demo's orders.db)")]
    end

    API --> OTELSDK
    OTELSDK -->|"OTLP/gRPC"| COL
    COL --> CH
    ZK --- CH
    CH --> SN
    META --- SN

    Dev["Developer's Browser"]
    Dev -->|"HTTP :8080"| SN
```

**Simplification notes [SIMPLIFICATION]**

- The demo API's `orders.db` (business data) and SigNoz's own metadata
  SQLite file are two unrelated SQLite databases — they are drawn separately
  on purpose so they are never confused.
- This diagram matches `docker-compose.yml` in this repo: `app`,
  `signoz-otel-collector`, `clickhouse`, `zookeeper`, `signoz` services.
- A production SigNoz deployment would use Foundry/Kubernetes and typically
  Postgres for metadata + a multi-node ClickHouse cluster — not shown here,
  out of scope for a local demo.
- The `SigNoz Backend + UI` box is provisioned via **Foundry**
  (`signoz.io/docs/install/docker/`), not a hand-rolled compose service —
  see `docs/signoz-architecture.md` §2.10 for why: the classic root
  `docker-compose.yaml` self-host method was deprecated by SigNoz itself.
  `docker-compose.yml` in this repo provisions everything *except* that box
  (ClickHouse, Zookeeper, the collector, and our own app).
