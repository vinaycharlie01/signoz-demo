# Most Important Agents to Build — Golang CLI that Deploys SigNoz on k0s

Context: the product is a **Golang CLI** that stands up **k0s** (the
single-binary, zero-dependency Kubernetes distribution — control plane +
worker in one process, commonly used for edge/on-prem/resource-constrained
single-node deployments) and installs **SigNoz** on top of it with one
command. That's a materially different starting point than the 1000-idea
brainstorm in `docs/AI_AGENT_IDEAS_FOR_SIGNOZ.md` — here, you *own the
whole stack end to end* (the k0s node, the SigNoz install, the upgrade
path), which unlocks a class of agents that can't exist for a generic
"point an agent at someone else's SigNoz" tool. This file is the narrow,
prioritized list for *this specific product*, not another giant brainstorm.

**Why this matters**: a "single command" installer's reputation lives or
dies on what happens when that one command *doesn't* just work — wrong
kernel version, port already bound, not enough disk, flaky network mid-pull,
ClickHouse migration hanging. The single most valuable thing you can build
first is not a flashy AI SRE — it's the boring agent that makes the
one-command promise actually true.

---

## If you build only one thing: the Install Doctor Agent

**Preflight + Postflight Doctor Agent** — runs *before* `k0s` and SigNoz are
installed to catch environment problems early (kernel/cgroup support, free
disk/memory, required ports 6443/4317/4318/8080/2379 free, DNS resolution,
clock skew, existing conflicting k0s/containerd state), and runs *after*
install to verify the whole stack is actually healthy (k0s node `Ready`,
every SigNoz-related pod `Running`, the OTel collector's `migrate sync
check` succeeded, ClickHouse responds on `:8123/ping`, the SigNoz API
responds on `:8080`, a synthetic OTLP span round-trips and is queryable
within N seconds).

Why this is #1: every other agent below assumes a healthy install exists.
Without this one, "single command" quietly becomes "single command, then
open 6 kubectl tabs to figure out why it's stuck at `k0s status`." It's
also the most demoable agent in a hackathon — you can show a real failure
(port conflict, low disk) being caught with a clear fix suggestion in
seconds, which judges immediately understand the value of.

Concretely, wire it into the CLI as two subcommands:

```bash
your-cli doctor            # preflight — run before install
your-cli verify            # postflight — run right after install, and any time later
```

---

## Tier 1 — Build these next (core reliability of the deployed stack)

### 1. Self-Healing Operator Agent
Watches the k0s node + SigNoz pods (via the Kubernetes API, not just SigNoz's
own telemetry, since if SigNoz itself is down you need a signal that
doesn't depend on it) and auto-remediates the failure modes you'll actually
hit on a single-node k0s box: OOM-killed ClickHouse (bump memory limit and
restart, don't just restart), collector stuck in `migrate` (retry with
backoff, alert if it fails 3x), disk-pressure eviction (prune old
ClickHouse parts per retention policy before it becomes an outage), k0s
control-plane restart after an unclean host reboot. This is the "self-
healing" idea from the 1000-list (#121, #157) but scoped specifically to
the exact failure modes a single-node k0s+SigNoz box hits — a much smaller,
much more buildable target than "self-healing for any Kubernetes cluster."

### 2. Upgrade & Rollback Agent
Single-node installs are the hardest place to get upgrades right — there's
no other node to fail over to. This agent: snapshots SQLStore + ClickHouse
state before upgrading, runs the new SigNoz/k0s version's own health checks
(reuse the Doctor Agent above) after upgrade, and **automatically rolls
back** to the snapshot if health checks don't pass within a timeout. Wire
it as `your-cli upgrade`, and make the rollback automatic-by-default, not
opt-in — that's the actual trust-building feature.

### 3. Backup / Disaster Recovery Agent
For a single-node deployment, "the node dies" is a real, common scenario
(power loss, disk failure, accidental `rm`), not an edge case. This agent
schedules and verifies backups of k0s's datastore (etcd, or SQLite/kine if
you're running k0s in its single-node lightweight datastore mode) and
SigNoz's own SQLStore + ClickHouse data, and — critically — **periodically
tests that a restore actually works** rather than trusting an unverified
backup file. Wire as `your-cli backup` / `your-cli restore` / `your-cli
verify-backup`.

---

## Tier 2 — High-value differentiators (what makes this stand out at the hackathon)

### 4. Self-Observability Meta-Agent ("the installer traces itself")
Instrument the CLI itself with OpenTelemetry, and point its OTLP exporter
at the SigNoz instance it just deployed. The demo: run
`your-cli install`, then open the freshly-deployed SigNoz UI and see the
**installation process itself** as a trace — "download k0s" → "start
control plane" → "wait for node ready" → "install SigNoz Helm-equivalent
resources" → "run collector migration" → "verify" — each step a span, with
real durations and any retries visible as child spans. This is genuinely
the single best hackathon demo moment available to you: it proves the
whole stack works by using the product to observe its own creation. Cheap
to build (it's the same OTel SDK setup pattern already in this repo's
`pkg/observability`) and disproportionately impressive live.

### 5. Resource Right-Sizing / Node Capacity Advisor
k0s's whole reason for existing is running well on modest hardware
(edge boxes, small VMs, on-prem servers) — so "will this fit" is a much
more real question here than on a big cloud cluster. This agent watches
actual CPU/memory/disk usage of ClickHouse + the collector post-install and
tells the operator concretely: "ClickHouse is using 3.2GB steady-state on
this 4GB box, you're close to the edge — either add memory or reduce
retention." Directly reuses category-7 ("Capacity Planning") and
category-21 ("Kubernetes-specific") ideas from the 1000-list, scoped
tightly to a single k0s node instead of a cluster.

### 6. Certificate & Ingress Health Agent
TLS/ingress is consistently where "single command" installers actually
break for real users (self-signed cert trust, port 443 conflicts, DNS not
pointed yet). An agent that verifies the ingress path end-to-end (can an
external client actually reach the SigNoz UI over HTTPS with a valid cert
chain) and proactively renews/rotates certs before expiry, rather than
leaving it as a manual step.

### 7. SigNoz-on-k0s SRE Copilot (CLI-native chat/query agent)
A `your-cli ask "why is ingestion slow"` command that queries the
just-deployed SigNoz (via its Query Builder API or MCP server) plus k0s
node metrics, and answers in the terminal. Since you control the whole
stack, this can be much more precise than a generic "SigNoz copilot" — it
already knows there's exactly one node, exactly this collector image
version, exactly this retention config, so its diagnoses can skip a lot of
the "which cluster/which environment" ambiguity a general tool would have
to ask about first.

---

## Tier 3 — Strong stretch goals (build if Tier 1–2 land early)

### 8. Air-Gapped / Constrained-Connectivity Install Agent
k0s is frequently deployed in genuinely disconnected or bandwidth-limited
environments. An agent that detects a slow/no-internet environment during
`your-cli install` and switches to a pre-bundled/offline image cache
automatically, rather than hanging on a registry pull with no explanation.

### 9. Cost/Resource Guardrail Enforcer
For on-prem/edge hardware there's no cloud bill, but there is a hard
physical ceiling — this agent refuses (or clearly warns before) an install
whose SigNoz retention/ingestion settings are configured to require more
disk than the box physically has, rather than letting it run for two weeks
and then silently fail.

### 10. Conversational Setup Assistant
Replaces a wall of CLI flags with a short guided conversation
("how much data do you expect to ingest per day?" → sets sensible
ClickHouse retention/resource defaults automatically). Nice UX polish, but
correctly the *last* thing to build — it's not what makes the tool
trustworthy, it's what makes it pleasant once it already is.

### 11. Multi-Node Growth Path Advisor
For when a single k0s node outgrows itself: an agent that recognizes
sustained resource pressure (from #5 above) and specifically recommends
*when and how* to graduate from a single k0s node to a multi-node k0s
cluster (k0s supports adding worker nodes via `k0sctl`), rather than
leaving that migration path undocumented and manual.

---

## Suggested build order for a hackathon timeline

1. **Doctor Agent** (preflight + postflight) — the credibility foundation.
2. **Self-Observability Meta-Agent** — cheap to add on top of #1's
   instrumentation, and it's your best live demo moment.
3. **Self-Healing Operator** (scoped to just 2–3 real failure modes you can
   actually reproduce and demo, not a general framework).
4. Pick **one** of Tier 2 based on what's most demoable with your remaining
   time — the Upgrade & Rollback Agent demos very well ("watch it break the
   upgrade on purpose, then watch it roll back automatically").

Everything else in this file, and the other 1000 ideas in
`docs/AI_AGENT_IDEAS_FOR_SIGNOZ.md`, is worth keeping as a backlog — but a
judge remembers one thing that clearly worked, not twelve things that
were described.
