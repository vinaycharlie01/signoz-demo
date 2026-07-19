# signoz-demo

A production-shaped Golang **Order Service** built with **Hexagonal Architecture** (Ports & Adapters), instrumented end-to-end with **OpenTelemetry** (traces, metrics, and logs), and wired into a self-hosted **SigNoz** telemetry backend.

This repo ships two deployment modes:

| Mode | How | When to use |
|------|-----|-------------|
| **Docker Compose** | `mage docker:up` | Fast local dev — SigNoz runs via Foundry |
| **Kubernetes (k3d)** | `kubectl apply -k` | Demo / staging — full k8s stack on your laptop |

---

## Table of Contents

1. [Project overview](#project-overview)
2. [Architecture](#architecture)
3. [Repository layout](#repository-layout)
4. [Prerequisites](#prerequisites)
5. [Quick start — Docker Compose](#quick-start--docker-compose)
6. [Quick start — Kubernetes on k3d](#quick-start--kubernetes-on-k3d)
   - [Step 1 — Create the k3d cluster (eh-fleets)](#step-1--create-the-k3d-cluster-eh-fleets)
   - [Step 2 — Deploy SigNoz (foundry)](#step-2--deploy-signoz-foundry)
   - [Step 3 — Deploy signoz-demo](#step-3--deploy-signoz-demo)
   - [Step 4 — Access the services](#step-4--access-the-services)
7. [Kubernetes manifests (`deploy/`)](#kubernetes-manifests-deploy)
8. [CI workflow — build & publish image](#ci-workflow--build--publish-image)
9. [Environment variables](#environment-variables)
10. [Sending test requests](#sending-test-requests)
11. [Load generation](#load-generation)
12. [Viewing telemetry in SigNoz](#viewing-telemetry-in-signoz)
13. [Troubleshooting](#troubleshooting)
14. [Cleanup](#cleanup)

---

## Project overview

| | |
|---|---|
| Domain | Orders (`POST` / `GET /api/v1/orders`) |
| Architecture | Hexagonal — `internal/domain` has zero framework imports |
| Database | SQLite (`modernc.org/sqlite`, pure Go, no cgo) |
| Observability | OpenTelemetry SDK — **traces + metrics + logs**, exported over OTLP/gRPC |
| Telemetry backend | Self-hosted **SigNoz** (Docker Compose via Foundry **or** Kubernetes via kustomize) |
| Build tooling | [Mage](https://magefile.org/) — no Makefile, no shell scripts |
| Container image | `ghcr.io/vinaycharlie01/signoz-demo` (multi-arch: `linux/amd64`, `linux/arm64`) |

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

### Telemetry signal flow

```
Order Service (Go)
  │
  │  OTLP/gRPC  :4317   (traces + metrics + logs — single endpoint)
  ▼
SigNoz ingester (signoz-otel-collector)
  │
  ├─ traces   → ClickHouse (signoz_traces)
  ├─ metrics  → ClickHouse (signoz_metrics / signoz_meter)
  └─ logs     → ClickHouse (signoz_logs)
                    │
                    ▼
             SigNoz UI  :8080
```

---

## Repository layout

```
signoz-demo/
├── cmd/
│   ├── api/           # HTTP server entry point
│   └── loadgen/       # Load generator CLI
├── deploy/            # Kubernetes manifests (kustomize)
│   ├── base/          # Namespace, Deployment, Service, PVC, Ingress
│   └── overlays/
│       └── local/     # k3d overrides (nip.io host, image tag)
├── internal/          # Hexagonal application core
│   ├── adapters/      # HTTP + SQLite adapters
│   ├── application/   # Use cases
│   ├── domain/        # Pure domain (no framework imports)
│   └── ports/         # Interface definitions
├── pkg/
│   └── observability/ # OTel SDK setup (traces + metrics + logs)
├── migrations/        # SQLite schema
├── Dockerfile         # Alpine image (copies prebuilt binary from dist/)
├── docker-compose.yml # Local dev stack (app only — SigNoz via Foundry)
├── magefile.go        # Mage entry point
├── go.yaml            # Mage Go build config
└── docker.yaml        # Mage Docker build config
```

---

## Prerequisites

### All modes

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.25+ | https://go.dev/dl/ |
| Mage | latest | `go install github.com/magefile/mage@latest` |
| Docker + Docker Compose | v2.x | https://docs.docker.com/get-docker/ |

### Kubernetes mode (additional)

| Tool | Version | Install |
|------|---------|---------|
| k3d | latest | `curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh \| bash` |
| kubectl | 1.29+ | https://kubernetes.io/docs/tasks/tools/ |
| kustomize | 5.x | `brew install kustomize` or https://kubectl.docs.kubernetes.io/installation/kustomize/ |

---

## Quick start — Docker Compose

This is the fastest way to run the Order Service locally. SigNoz runs separately via Foundry.

```bash
# 1. Start SigNoz (from the foundry repo root — one level up)
cd ../foundry
docker compose -f pours/deployment/compose.yaml up -d
# Wait ~60 s for signoz-signoz-0 to become healthy
docker compose -f pours/deployment/compose.yaml ps

# 2. Start the Order Service
cd ../signoz-demo
mage docker:up

# 3. Verify
curl http://localhost:8090/health
# → {"status":"ok"}

# 4. Open SigNoz UI
open http://localhost:8080
```

---

## Quick start — Kubernetes on k3d

This mode runs everything (ingress-nginx, SigNoz, and the Order Service) inside a local k3d cluster. No ArgoCD — all deployments are done with plain `kubectl apply -k`.

### Repo layout on disk

The three repos are expected as siblings:

```
~/path/to/
├── eh-fleets/      ← cluster creation (k3d + mage)
├── foundry/        ← SigNoz kustomize manifests
└── signoz-demo/    ← Order Service app + deploy/ manifests
```

> You can override paths with `FOUNDRY_PATH` and `SIGNOZ_DEMO_PATH` env vars when running `mage` targets.

---

### Step 1 — Create the k3d cluster (eh-fleets)

```bash
cd eh-fleets

# Create the k3d cluster, create namespaces, and install ingress-nginx
mage up

# Verify
kubectl get nodes
kubectl get pods -n ingress-nginx
```

`mage up` does exactly three things in order:
1. `k3d cluster create` — 1 server + 1 agent, ports 80/443 forwarded to the loadbalancer
2. `kubectl apply -k infrastructure/base/observability` — creates `monitoring` and `ingress-nginx` namespaces
3. `helm install ingress-nginx` — installs the ingress controller

> **Find the loadbalancer IP** (needed for nip.io URLs later):
> ```bash
> kubectl get svc -n ingress-nginx ingress-nginx-controller \
>   -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
> ```
> On k3d this is usually `127.0.0.1` on macOS/Linux — traffic is forwarded through `localhost:80`.

---

### Step 2 — Deploy SigNoz (foundry)

SigNoz is deployed using the kustomize manifests inside the `foundry` repo.

```bash
# From eh-fleets — uses FOUNDRY_PATH (default: ../foundry)
mage deploySignoz

# ── OR run manually ──
kubectl apply --server-side -k \
  ../foundry/docs/examples/kubernetes/kustomize/pours/deployment
```

Wait for all SigNoz pods to be ready (~3–5 minutes on first pull):

```bash
kubectl get pods -n signoz -w
# Wait until all pods are Running/Completed
```

Check the SigNoz ingress:

```bash
kubectl get ingress -n signoz
```

SigNoz UI will be available at `http://signoz.127.0.0.1.nip.io` (or the IP from step 1).

> **First-time setup**: SigNoz prompts you to create an admin account on the first visit.

---

### Step 3 — Deploy signoz-demo

The Order Service image is pulled from GHCR. The CI workflow publishes it automatically on every push to `main`/`dev`.

#### Option A — Using mage (from eh-fleets)

```bash
# From eh-fleets — uses SIGNOZ_DEMO_PATH (default: ../signoz-demo)
mage deploySignozDemo

# Wait for rollout
mage deploySignozDemoRollout
```

#### Option B — Using kubectl directly (from signoz-demo)

```bash
cd signoz-demo

# Optional: pin a specific image tag
# (default overlay uses :latest — fine for demos)
# cd deploy/overlays/local
# kustomize edit set image ghcr.io/vinaycharlie01/signoz-demo=ghcr.io/vinaycharlie01/signoz-demo:sha-abc1234

# Apply
kubectl apply -k deploy/overlays/local

# Wait for rollout
kubectl rollout status deployment/signoz-demo -n signoz-demo --timeout=120s
```

Verify the pod is running:

```bash
kubectl get pods -n signoz-demo
kubectl get ingress -n signoz-demo
```

---

### Step 4 — Access the services

By default the local overlay uses `172.21.189.76` as the nip.io IP. Update it to match your actual loadbalancer IP:

```bash
# Get the real IP
LB_IP=$(kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "LoadBalancer IP: $LB_IP"
```

On most k3d setups traffic goes through `localhost` (`127.0.0.1`). Edit `deploy/overlays/local/kustomization.yaml` and replace `172.21.189.76` with your IP, then re-apply.

| Service | URL |
|---------|-----|
| Order Service | `http://signoz-demo.127.0.0.1.nip.io` |
| SigNoz UI | `http://signoz.127.0.0.1.nip.io` |

```bash
curl http://signoz-demo.127.0.0.1.nip.io/health
# → {"status":"ok"}
```

---

## Kubernetes manifests (`deploy/`)

```
deploy/
├── base/
│   ├── kustomization.yaml   # ties all base resources together
│   ├── namespace.yaml       # Namespace: signoz-demo
│   ├── deployment.yaml      # Deployment (1 replica, liveness + readiness probes)
│   ├── service.yaml         # ClusterIP Service on port 80 → container :8090
│   ├── pvc.yaml             # 1 Gi PVC for SQLite data volume
│   └── ingress.yaml         # Ingress (nginx) — host: signoz-demo.example.local
└── overlays/
    └── local/
        └── kustomization.yaml  # Patches ingress host to nip.io, pins image tag
```

### Updating the image tag

```bash
cd deploy/overlays/local

# Pin to a specific SHA tag published by CI
kustomize edit set image \
  ghcr.io/vinaycharlie01/signoz-demo=ghcr.io/vinaycharlie01/signoz-demo:sha-abc1234

kubectl apply -k .
kubectl rollout status deployment/signoz-demo -n signoz-demo --timeout=120s
```

---

## CI workflow — build & publish image

The workflow at `.github/workflows/ci.yml` runs on every push to `main`, `dev`, and `claude/**` branches (and on PRs as a build-only check).

### What it does

```
push to main/dev
       │
       ▼
  Checkout + Setup Go
       │
       ▼
  mage go:crossBuild          ← compile linux/amd64 + linux/arm64 binaries
       │
       ▼
  docker/setup-qemu-action    ← multi-arch emulation
  docker/setup-buildx-action
       │
       ▼
  docker/login-action         ← log in to ghcr.io (GITHUB_TOKEN)
       │
       ▼
  docker/metadata-action      ← compute tags:
                                  :main / :dev       (branch)
                                  :sha-abc1234       (short SHA)
                                  :1.2.3             (semver tag)
       │
       ▼
  docker/build-push-action    ← build linux/amd64 + linux/arm64 image
                                 push to ghcr.io/vinaycharlie01/signoz-demo
```

### Tags produced

| Trigger | Tags |
|---------|------|
| Push to `main` | `:main`, `:sha-abc1234` |
| Push to `dev` | `:dev`, `:sha-abc1234` |
| Semver tag `v1.2.3` | `:1.2.3`, `:1.2` |
| Pull Request | build-only, **no push** |

### Required permissions

The workflow uses `GITHUB_TOKEN` (automatically provided by GitHub Actions) to push to GHCR. No extra secrets needed for publishing.

To make the package public, go to: **GitHub → Packages → signoz-demo → Package settings → Change visibility → Public**.

---

## Environment variables

| Variable | Default (k3d) | Meaning |
|----------|--------------|---------|
| `HTTP_ADDR` | `:8090` | Address the HTTP API listens on |
| `DB_PATH` | `/data/orders.db` | SQLite file path |
| `OTEL_SERVICE_NAME` | `signoz-demo-order-service` | `service.name` resource attribute |
| `DEPLOYMENT_ENVIRONMENT` | `k3d-local` | `deployment.environment.name` attribute |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `signoz-ingester.signoz.svc.cluster.local:4317` | OTLP/gRPC endpoint (all three signals) |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Skip TLS — fine for local dev only |

---

## Sending test requests

```bash
# Health / readiness
curl http://signoz-demo.127.0.0.1.nip.io/health
curl http://signoz-demo.127.0.0.1.nip.io/ready

# Create an order
curl -s -X POST http://signoz-demo.127.0.0.1.nip.io/api/v1/orders \
  -H 'Content-Type: application/json' \
  -d '{"customer_name":"Alice","item":"widget","quantity":2,"amount_cents":1999}' | jq

# List all orders
curl -s http://signoz-demo.127.0.0.1.nip.io/api/v1/orders | jq

# Get a specific order (replace <id>)
curl -s http://signoz-demo.127.0.0.1.nip.io/api/v1/orders/<id> | jq
```

> For Docker Compose mode replace the host with `localhost:8090`.

---

## Load generation

```bash
# Run from signoz-demo/ — these target localhost:8090 (Docker Compose)
mage loadgen:normal       # 20 healthy sequential requests
mage loadgen:slow         # requests that hit the injected ~1.8 s SQLite latency
mage loadgen:errors       # mix of use-case + repository-level failures
mage loadgen:concurrent   # 60 requests, 10 concurrent workers, mixed scenarios
```

Each target sets the `X-Demo-Scenario` header. You can also trigger a scenario manually:

```bash
curl -s -X POST http://signoz-demo.127.0.0.1.nip.io/api/v1/orders \
  -H 'Content-Type: application/json' \
  -H 'X-Demo-Scenario: slow' \
  -d '{"customer_name":"Bob","item":"gadget","quantity":1,"amount_cents":999}'
```

---

## Viewing telemetry in SigNoz

Open **http://signoz.127.0.0.1.nip.io** (k3d) or **http://localhost:8080** (Docker Compose).

### Traces

1. **Services** → `signoz-demo-order-service`
2. **Traces** → filter by `http.route = POST /api/v1/orders`
3. Run `mage loadgen:slow` and open a slow trace — you'll see the full span tree:
   `POST /api/v1/orders` → `OrderService.CreateOrder` → `sqlite.INSERT orders`

### Metrics

- **Services** overview shows p50/p90/p99 latency, RPS, and error rate
- Custom application metrics (Dashboards → New Panel → Metrics):
  - `orders_created_total`
  - `order_create_duration_seconds`
  - `db_operation_duration_seconds`
  - `order_errors_total`

### Logs

**Logs Explorer** → filter `service.name = signoz-demo-order-service`.

### Trace ↔ Log correlation

Every log record emitted while a span is active carries `trace_id` and `span_id`.
- Open a trace → **Related logs** → jump to the request's log lines.
- Open a log line → click `trace_id` → jump to the trace.

---

## Troubleshooting

| Symptom | Likely cause & fix |
|---------|-------------------|
| `kubectl apply -k` fails with `namespace not found` | k3d cluster not up. Run `mage up` in `eh-fleets` first. |
| Pod stuck in `ImagePullBackOff` | Image not published yet. Push to `main`/`dev` to trigger CI, or run `mage docker:buildxBuild` + `mage docker:push` locally. |
| Pod stuck in `CrashLoopBackOff` | Check logs: `kubectl logs -n signoz-demo deploy/signoz-demo`. Usually a bad `OTEL_EXPORTER_OTLP_ENDPOINT` — verify SigNoz is running. |
| No data in SigNoz UI | The ingester may still be starting. Wait ~60 s and send a request. Check: `kubectl get pods -n signoz`. |
| `signoz-demo.127.0.0.1.nip.io` not reachable | ingress-nginx not ready, or wrong IP in overlay. Run `kubectl get svc -n ingress-nginx` to find the real IP. |
| `mage: command not found` | Run `go install github.com/magefile/mage@latest` and ensure `$(go env GOPATH)/bin` is on `PATH`. |
| Docker Compose: `network signoz-network not found` | Start SigNoz first: `docker compose -f ../foundry/pours/deployment/compose.yaml up -d`. |

---

## Cleanup

### Kubernetes (k3d)

```bash
# Delete the Order Service
kubectl delete -k signoz-demo/deploy/overlays/local

# Delete SigNoz
kubectl delete -k foundry/docs/examples/kubernetes/kustomize/pours/deployment

# Tear down the entire cluster (from eh-fleets)
cd eh-fleets
mage down
```

### Docker Compose

```bash
# Stop the Order Service
cd signoz-demo
docker compose down

# Wipe app data volume
docker volume rm signoz-demo_app-data

# Stop SigNoz (from foundry root)
cd ../foundry
docker compose -f pours/deployment/compose.yaml down

# Wipe SigNoz volumes (destructive)
docker volume rm \
  signoz-metastore-postgres-0-data \
  signoz-telemetrykeeper-0-data \
  signoz-telemetrystore-0-0-data \
  signoz-telemetrystore-user-scripts
```

---

## Further reading

- [`deploy/`](deploy/) — Kubernetes manifests for the Order Service
- [`docs/FULL_ARCHITECTURE.md`](docs/FULL_ARCHITECTURE.md) — full SigNoz architecture breakdown
- [`docs/signoz-architecture.md`](docs/signoz-architecture.md) — component-by-component breakdown
- [`eh-fleets/README.md`](../eh-fleets/README.md) — k3d cluster management with Mage
- [`foundry/README.md`](../foundry/README.md) — SigNoz installation with Foundry
