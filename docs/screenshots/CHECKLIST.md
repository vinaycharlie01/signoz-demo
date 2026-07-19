# Screenshot Checklist (for the blog post)

Capture these once the stack is up (`mage docker:up`, SigNoz installed via
Foundry per the README) and after running all four `mage loadgen:*`
targets at least once. Save into this directory as
`01-services-overview.png`, `02-slow-trace-list.png`, etc.

1. **Services Overview** — SigNoz → Services tab. Show
   `signoz-demo-order-service` with a visible p99 latency bump and non-zero
   error rate.
2. **Slow trace list** — Traces tab, filtered by
   `serviceName=signoz-demo-order-service`, sorted by duration descending.
   Show the one `POST /api/v1/orders` entry sitting well above the rest.
3. **Trace waterfall** — Open that trace. Show all three spans stacked:
   `POST /api/v1/orders` → `OrderService.CreateOrder` →
   `sqlite.INSERT orders`.
4. **SQLite span attributes** — Click the `sqlite.INSERT orders` span.
   Show `db.system`, `db.operation`, `db.sql.table`, and its isolated
   duration.
5. **Correlated logs** — From the same trace, open "Related logs" (or
   search Logs Explorer by `trace_id`). Show the `http request` log line
   with matching `trace_id`/`span_id`.
6. **(Optional) Error trace** — After `mage loadgen:errors`, capture one
   `error` scenario trace (fails at the use-case span, no DB span) next to
   one `db-fail` scenario trace (fails at the DB span) side by side, to
   show the visual difference between "failed before touching the DB" and
   "failed inside the DB call."
7. **(Optional) Metrics panel** — Dashboards → New Panel → Query Builder,
   graphing `order_create_duration_seconds` and `db_operation_duration_seconds`
   on the same panel, showing the DB histogram tracking the use-case
   histogram closely (proving the DB is the dominant cost).

## Pre-publication checklist

- [ ] All five Mermaid diagrams in `docs/diagrams/` render correctly
      (verified in this repo via GitHub's built-in Mermaid renderer or
      `mage`-free local preview).
- [ ] `mage go:test` passes.
- [ ] `mage go:vet` passes.
- [ ] `mage docker:up` brings up ClickHouse + the collector cleanly; SigNoz
      (via Foundry) points at the same ClickHouse and shows the service.
- [ ] All four `mage loadgen:*` scenarios produce visibly distinct traces
      in the UI (normal / slow / error / db-fail).
- [ ] Screenshots 1–5 above captured and embedded in the blog draft,
      replacing the `[SCREENSHOT: ...]` placeholders.
- [ ] Blog reviewed for accuracy against `docs/signoz-architecture.md` —
      no claim in the blog should contradict what's marked `[SOURCE]` there.
- [ ] No secrets, tokens, or internal hostnames in any committed file.
- [ ] README's Quick Start commands (`mage go:setup`, `mage docker:up`,
      `mage loadgen:normal`) work on a clean clone.
