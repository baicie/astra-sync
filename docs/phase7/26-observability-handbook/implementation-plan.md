# Slice 26 Implementation Plan

## Work breakdown

The slice is delivered as four commits. Each commit is reviewable
in isolation and passes the local check.

### Commit 1 — ADR

- Add `docs/adr/adr-047-observability-handbook-and-dashboard-consolidation.md`.
- Update `docs/adr/README.md` to index ADR-047.

### Commit 2 — Observability handbook

- Add `docs/observability/README.md`.
- Add `docs/observability/metrics-catalog.md`.
- Add `docs/observability/log-conventions.md`.
- Add `docs/observability/audit-correlation.md`.
- Add `docs/observability/slo-handbook.md`.
- Add `docs/observability/dashboard-recipes.md`.

### Commit 3 — Slice records

- Add `docs/phase7/26-observability-handbook/README.md`.
- Add `docs/phase7/26-observability-handbook/design.md`.
- Add `docs/phase7/26-observability-handbook/implementation-plan.md`.
- Add `docs/phase7/26-observability-handbook/verification.md`.

### Commit 4 — Phase 7 README + index

- Update `docs/phase7/README.md` to mark Slice 26 as Implementation
  Complete and to point at the slice's records.

## F7 work breakdown

The API Server SLO instrumentation follow-up is delivered as one reviewable
slice:

1. Add isolated-registry tests for counter/histogram updates, OpenMetrics
   negotiation, canonical UUID exemplars, and invalid-ID omission.
2. Add authentication interceptor tests for exactly-once
   `success`/`rejected`/`failure` decisions and trusted tenant labels.
3. Add AuditService tests for authorization-gated observations and audit-row
   request ID reuse.
4. Implement and inject the shared recorder, then update the handbook and
   phase records to distinguish active families from pending descriptors.

## Verification steps

After the last commit, the operator runs:

```sh
make check-runbooks
make check
make test-go
make test-java
make check-security
mvn -B -ntp verify
```

The gate walks every Markdown file under `docs/observability/` and
rejects any file that lacks a `<placeholder>` or contains a known
production hostname pattern. The gate is the same one that Slice 24
introduced for the operational runbooks.

The slice is ready to merge when `make check-runbooks` passes
locally and the CI run on the PR passes.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| The handbook documents a metric name that the production code does not emit. | The catalog labels each family as emitted, descriptor-only, or reserved. Descriptor registration is verified separately from future business call-site instrumentation. |
| The follow-up migration breaks the existing production behaviour. | F1/F2 preserve stable CLI and liveness output while routing error paths through SLF4J. F3 uses module-local loggers and F4/F5 keep metrics fail-closed when disabled. |
| The handbook drifts from the Helm chart. | The Helm chart exposes the `monitoring.prometheus.port` and `logging.pattern` knobs; the handbook records the convention that the operator uses to wire the platform's signal store. A change to the chart values requires a corresponding update to the handbook. |
| The check-runbooks guard rejects a populated document. | The guard is best-effort. A false positive is fixable by removing the production hostname pattern or by adding a placeholder. The guard is consistent with the Slice 24 guard. |
| Unauthenticated input creates unbounded tenant or request-ID cardinality. | The interceptor uses only validated membership UUIDs or two fixed tenant values, and the recorder accepts only canonical UUID request IDs as exemplars. |
| Caller rejection traffic consumes the service error budget. | `rejected` is retained for security visibility but the SLO denominator includes only `success` and `failure`. |

## Rollout

The closeout requires no automatic rollout. Helm renders the optional
metrics listeners and ServiceMonitor resources only when enabled; the
default-disabled listener path remains unchanged. The implementation ships
when the closeout PR merges.

## F10 work breakdown

The Connection Test Executor instrumentation is delivered as one focused
follow-up:

1. Add an isolated recorder with a bounded `tenant_id` and `outcome` label
   allowlist.
2. Record exactly once after a claimed operation's durable completion succeeds.
3. Classify egress-policy denial as `rejected`; classify timeout, cancellation,
   credential, transport, and handshake failures as `failure`.
4. Verify that lease loss does not create a business sample.

## F11 work breakdown

The Console BFF instrumentation follow-up is delivered as one focused slice:

1. Add an isolated recorder with bounded tenant, outcome, and handler labels.
2. Observe each completed HTTP request at the BFF boundary and classify 2xx/3xx,
   4xx, and 5xx responses as `success`, `rejected`, and `failure`.
3. Take tenant identity only from the server-written scope response header and
   use `_unknown` for unauthenticated or unscoped responses.
4. Observe HTML response latency only for the fixed static render handler.
5. Verify that incoming tenant headers cannot create a metric tenant series.

## F12 work breakdown

The trusted-proxy HSTS instrumentation follow-up is delivered as one focused
slice:

1. Add an isolated recorder for HSTS responses with bounded tenant labels.
2. Observe only responses for HTTPS requests accepted from a trusted proxy and
   only when the middleware adds the HSTS header.
3. Use the fixed pre-auth `_unknown` tenant value at the API Server boundary.
4. Verify that direct TLS and plaintext requests do not create observations.

## F13 work breakdown

The Controller reconcile instrumentation follow-up is delivered as one focused
slice:

1. Add an isolated recorder for Controller reconcile duration with bounded
   `tenant_id` and `outcome` labels.
2. Observe every completed `SyncJob` reconcile iteration, including failures,
   using `success` and `failure` outcomes.
3. Use the fixed `_unknown` tenant value until a trusted Controller tenant
   binding exists.
4. Verify descriptor registration, label normalization, and the production
   reconcile call path with deterministic unit tests.

## Open questions

F13 completes Controller reconcile-duration observations. Other Go business
call sites and the remaining Controller lifecycle owners remain intentionally
deferred. The boundary is documented in ADR-047 and the observability
handbook.
