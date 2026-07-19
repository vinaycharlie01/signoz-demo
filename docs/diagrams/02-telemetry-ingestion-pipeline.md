# Diagram 2 — Telemetry Ingestion Pipeline (Traces / Metrics / Logs)

How each signal travels from the Golang Order Service into SigNoz's storage,
shown as three parallel lanes through the same collector.

```mermaid
flowchart TB
    APP["Golang Order Service"]

    APP -->|"spans via OTLP/gRPC :4317"| RT["receiver: otlp (traces)"]
    APP -->|"metrics via OTLP/gRPC :4317"| RM["receiver: otlp (metrics)"]
    APP -->|"logs via OTLP/gRPC :4317"| RL["receiver: otlp (logs)"]

    subgraph TraceLane["Trace pipeline"]
        RT --> PT1["processor: signozspanmetrics/delta<br/>(derives RED metrics from spans)"]
        PT1 --> PT2["processor: batch"]
        PT2 --> ET["exporter: clickhousetraces"]
    end

    subgraph MetricLane["Metric pipeline"]
        RM --> PM1["processor: batch"]
        PM1 --> EM["exporter: signozclickhousemetrics"]
        PT1 -.->|"span-derived exemplars"| EM
    end

    subgraph LogLane["Log pipeline"]
        RL --> PL1["processor: batch"]
        PL1 --> EL["exporter: clickhouselogsexporter"]
    end

    ET --> CHT[("signoz_traces")]
    EM --> CHM[("signoz_metrics")]
    EL --> CHL[("signoz_logs")]

    style TraceLane fill:#1f2937,color:#fff,stroke:#4b5563
    style MetricLane fill:#1f2937,color:#fff,stroke:#4b5563
    style LogLane fill:#1f2937,color:#fff,stroke:#4b5563
```

**Notes (verified from `.devenv/docker/signoz-otel-collector/otel-collector-config.yaml`)**

- All three signals arrive over the **same** OTLP receiver (`grpc: 0.0.0.0:4317`,
  `http: 0.0.0.0:4318`) — the "lanes" above are pipeline names in the
  collector's `service.pipelines` config, not separate network listeners.
- `signozspanmetrics/delta` is the one processor that bridges traces →
  metrics: it computes latency histograms and request/error counts from
  spans and feeds them into the metrics exporter — this is how SigNoz gets
  RED-method service metrics without the app emitting them directly.
- A `prometheus` receiver also feeds the metrics pipeline (self-scraping the
  collector's own `:8888` metrics) — omitted above since it's not part of
  the application's data path.
