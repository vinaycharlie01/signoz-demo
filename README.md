# signoz-demo

A production-shaped Golang **Order Service** built with **Hexagonal Architecture** (Ports & Adapters), instrumented end-to-end with **OpenTelemetry** (traces, metrics, and logs), and wired into a self-hosted **SigNoz** telemetry backend.

This repo ships two deployment modes:

| Mode | How | When to use |
|------|-----|-------------|
| **Docker Compose** | `mage docker:up` | Fast local dev — SigNoz runs via Foundry |
| **Kubernetes (k3d)** | `mage cluster:up` + `mage helm:installSignoz` | Self-contained demo — full k8s stack on your laptop |

---

## Table of Contents

1. [Project overview](#project-overview)
2. [Architecture](#architecture)
3. [Repository layout](#repository-layout)
4. [Prerequisites](#prerequisites)
5. [Quick start — Docker Compose](#quick-start--docker-compose)
6. [Quick start — Kubernetes on k3d (self-hosted)](#quick-start--kubernetes-on-k3d-self-hosted)
   - [Step 1 — Create the k3d cluster](#step-1--create-the-k3d-cluster)
   - [Step 2 — Install SigNoz](#step-2--install-signoz)
   - [Step 3 — Install k8s-infra (optional)](#step-3--install-k8s-infra-optional)
   - [Step 4 — Deploy the Order Service](#step-4--deploy-the-order-service)
   - [Step 5 — Access the services](#step-5--access-the-services)
7. [All mage targets](#all-mage-targets)
8. [Kubernetes manifests (`deploy/`)](#kubernetes-manifests-deploy)
9. [CI workflow — build & publish image](#ci-workflow--build--publish-image)
10. [Environment variables](#environment-variables)
11. [Sending test requests](#sending-test-requests)
12. [Load generation](#load-generation)
13. [Viewing telemetry in SigNoz](#viewing-telemetry-in-signoz)
14. [Troubleshooting](#troubleshooting)
15. [Cleanup](#cleanup)

---

## Project overview

| | |
|---|---|
| Domain | Orders (`POST` / `GET /api/v1/orders`) |
| Architecture | Hexagonal — `internal/domain` has zero framework imports |
| Database | SQLite (`modernc.org/sqlite`, pure Go, no cgo) |
| Observability | OpenTelemetry SDK — **traces + metrics + logs**, exported over OTLP/gRPC |
| Telemetry backend | Self-hosted **SigNoz** (Docker Compose via Foundry **or** Kubernetes via `self-hosted/`) |
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
├── self-hosted/       # Everything needed to run SigNoz on k3d locally
│   ├── k3d.yaml       # Cluster + Helm config (read by mage targets)
│   ├── apps/
│   │   ├── base/observability/k8s-infra/values.yaml
│   │   └── local/
│   │       ├── signoz/values.yaml        # SigNoz Helm values
│   │       ├── signoz/ingress.yaml       # SigNoz ingress (nip.io)
│   │       └── observability/k8s-infra/  # k8s-infra Helm values
│   ├── clusters/      # ArgoCD app-of-apps (optional GitOps path)
│   └── infrastructure/
│       └── base/observability/  # Namespaces + base resources
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
├── magefile.go        # All mage targets (Go, Docker, Loadgen, Cluster, Helm, Gitops, Sops, Deploy)
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
| helm | 3.x | `brew install helm` or https://helm.sh/docs/intro/install/ |
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

## Quick start — Kubernetes on k3d (self-hosted)

Everything is self-contained in this repo. All commands are run from the `signoz-demo/` root — no sibling repos required.

```
signoz-demo/
└── self-hosted/   ← cluster config, Helm values, infra manifests
```

### Step 1 — Create the k3d cluster

```bash
cd signoz-demo

# Create the k3d cluster, create namespaces, and install ingress-nginx
mage cluster:up
```

`mage cluster:up` does four things in order:
1. `k3d cluster create observability` — 1 server + 1 agent, ports 80/443 forwarded to the loadbalancer, Traefik disabled
2. Adds all configured Helm repos (`ingress-nginx`, `signoz`)
3. `kubectl apply -k self-hosted/infrastructure/base/observability` — creates namespaces
4. `helm upgrade --install ingress-nginx` — installs the ingress controller

Verify:

```bash
kubectl get nodes
kubectl get pods -n ingress-nginx
```

> **Tip:** You can also run each step individually:
> ```bash
> mage cluster:create     # k3d cluster create
> mage helm:repos         # add + update Helm repos
> mage cluster:bootstrap  # apply infra kustomize (namespaces)
> ```

---

### Step 2 — Install SigNoz

```bash
# Install SigNoz via Helm (uses self-hosted/apps/local/signoz/values.yaml)
mage helm:installSignoz
```

This runs `helm upgrade --install signoz signoz/signoz` into the `signoz` namespace with a 15-minute timeout. Wait for all pods to be ready (~3–5 minutes on first pull):

```bash
kubectl get pods -n signoz -w
# Wait until all pods are Running/Completed

# Or use the mage target:
mage deploy:signozStatus
```

> **Override values file:** Set `SIGNOZ_VALUES` env var to point to a different values file:
> ```bash
> SIGNOZ_VALUES=self-hosted/apps/local/signoz/values.yaml mage helm:installSignoz
> ```

> **First-time setup:** SigNoz prompts you to create an admin account on the first visit.

---

### Step 3 — Install k8s-infra (optional)

`k8s-infra` deploys an OpenTelemetry Collector DaemonSet that collects pod logs, host metrics, kubelet metrics, cluster metrics, and Kubernetes events — forwarding them all to SigNoz.

Run this **after** SigNoz is ready:

```bash
mage helm:installK8sInfra
```

Verify:

```bash
mage deploy:k8sInfraStatus
# or: kubectl get pods -n k8s-infra
```

---

### Step 4 — Deploy the Order Service

The Order Service image is pulled from GHCR. CI publishes it automatically on every push to `main`/`dev`.

```bash
# Apply the kustomize overlay (deploy/overlays/local)
mage deploy:signozDemo

# Wait for the rollout to complete
mage deploy:signozDemoRollout
```

Or use kubectl directly:

```bash
kubectl apply -k deploy/overlays/local
kubectl rollout status deployment/signoz-demo -n signoz-demo --timeout=120s
```

Verify:

```bash
kubectl get pods -n signoz-demo
kubectl get ingress -n signoz-demo
```

---

### Step 5 — Access the services

Print the access URLs:

```bash
mage cluster:hosts
```

| Service | URL |
|---------|-----|
| SigNoz UI | `http://signoz.127.0.0.1.nip.io` |
| Order Service | `http://signoz-demo.127.0.0.1.nip.io` |

```bash
curl http://signoz-demo.127.0.0.1.nip.io/health
# → {"status":"ok"}

open http://signoz.127.0.0.1.nip.io
```

> **Using a different IP?** On some setups the loadbalancer IP may not be `127.0.0.1`. Find it and update `deploy/overlays/local/kustomization.yaml`:
> ```bash
> kubectl get svc -n ingress-nginx ingress-nginx-controller \
>   -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
> ```

---

## All mage targets

Run `mage -l` to see every target. They are organized into namespaces:

```
mage -l
```

### `go:` — local Go developer workflow

| Target | Description |
|--------|-------------|
| `mage go:setup` | Download and tidy Go module dependencies |
| `mage go:build` | Compile `cmd/api` → `bin/api` |
| `mage go:run` | Run `cmd/api` locally with `go run` |
| `mage go:test` | Run the full unit test suite |
| `mage go:vet` | Run `go vet ./...` |
| `mage go:crossBuild` | Cross-compile for `linux/amd64` + `linux/arm64` → `dist/` |

### `docker:` — Docker Compose stack

| Target | Description |
|--------|-------------|
| `mage docker:up` | Cross-build + `docker compose up --build -d` |
| `mage docker:down` | Stop and remove containers (volumes kept) |
| `mage docker:build` | Rebuild app image without starting |
| `mage docker:buildxBuild` | Build multi-arch publishable image |
| `mage docker:push` | Push image to registry |
| `mage docker:login` | Log in to container registry |

### `loadgen:` — load generation scenarios

| Target | Description |
|--------|-------------|
| `mage loadgen:normal` | 20 sequential healthy requests |
| `mage loadgen:slow` | Requests that hit injected SQLite latency |
| `mage loadgen:errors` | Mix of use-case + repository-level failures |
| `mage loadgen:concurrent` | 10-worker burst of mixed traffic |
| `mage loadgen:full` | All scenarios back-to-back |

### `cluster:` — k3d cluster lifecycle

| Target | Description |
|--------|-------------|
| `mage cluster:up` | Create cluster + bootstrap + install releases |
| `mage cluster:down` | Delete the k3d cluster |
| `mage cluster:create` | `k3d cluster create` only |
| `mage cluster:delete` | `k3d cluster delete` only |
| `mage cluster:list` | List k3d clusters |
| `mage cluster:bootstrap` | Apply infra kustomize (namespaces) |
| `mage cluster:status` | Show pods, PVCs, ingresses |
| `mage cluster:hosts` | Print service access URLs |

### `helm:` — Helm chart management

| Target | Description |
|--------|-------------|
| `mage helm:repos` | Add + update all Helm repos |
| `mage helm:installSignoz` | Install SigNoz via Helm |
| `mage helm:uninstallSignoz` | Uninstall SigNoz Helm release |
| `mage helm:installK8sInfra` | Install k8s-infra (pod logs + host metrics) |
| `mage helm:uninstallK8sInfra` | Uninstall k8s-infra |
| `mage helm:installIngressNginx` | Install ingress-nginx |
| `mage helm:installGrafana` | Install Grafana (optional) |
| `mage helm:installArgoCD` | Install ArgoCD (optional GitOps path) |
| `mage helm:createRepoSecret` | Create ArgoCD SSH repo secret |

### `deploy:` — Kubernetes deployments

| Target | Description |
|--------|-------------|
| `mage deploy:signozDemo` | `kubectl apply -k deploy/overlays/local` |
| `mage deploy:signozDemoRollout` | Wait for rollout to complete |
| `mage deploy:signozStatus` | Show SigNoz pod status |
| `mage deploy:k8sInfraStatus` | Show k8s-infra pod status |

### `gitops:` — ArgoCD GitOps (optional)

| Target | Description |
|--------|-------------|
| `mage gitops:bootstrap` | Create cluster + install ArgoCD + apply app-of-apps |
| `mage gitops:apply` | Apply the ArgoCD app-of-apps |
| `mage gitops:patchPrune` | Enable pruning on ArgoCD applications |

### `sops:` — secret management

| Target | Description |
|--------|-------------|
| `mage sops:init` | Install sops+age, generate key, encrypt secrets |
| `mage sops:encrypt` | Encrypt all `.dec.yaml` → `.enc.yaml` |
| `mage sops:decrypt` | Decrypt all `.enc.yaml` → `.dec.yaml` |

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
| `SIGNOZ_VALUES` | `self-hosted/apps/local/signoz/values.yaml` | Override Helm values file for `mage helm:installSignoz` |

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
mage loadgen:full         # all scenarios back-to-back
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
| `mage cluster:up` fails | Docker not running, or k3d not installed. Check `k3d version` and `docker info`. |
| `kubectl apply -k` fails with `namespace not found` | Cluster not up. Run `mage cluster:up` first. |
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
kubectl delete -k deploy/overlays/local

# Uninstall k8s-infra (if installed)
mage helm:uninstallK8sInfra

# Uninstall SigNoz
mage helm:uninstallSignoz

# Tear down the entire cluster (deletes everything)
mage cluster:down
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
- [`self-hosted/`](self-hosted/) — k3d cluster config, Helm values, infra manifests
- [`self-hosted/k3d.yaml`](self-hosted/k3d.yaml) — cluster + Helm release configuration
- [`magefile.go`](magefile.go) — all Mage targets in one file
