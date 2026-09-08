# Phase 22: Emission Sub-Slice 43.2.5 — auth_session_revoke_total via admin CLI

## Status

**In Progress.** — see Slices below.

## Theme

Complete the `auth_session_revoke_total` emission (Phase 17 activation
half → Phase 22+ emission half). Phase 22 closes slice 43.2.5:
wires the admin CLI `revoke-session` success boundary to the
authmetrics Recorder and establishes the log-dump emission pattern
for one-shot CLI tools.

## Scope

### Slices

| Slice | Owner | Description | Status |
|---|---|---|---|
| 48.0 | agent | ADR-065 + ADR index + exemption bump (if needed) | **Done** |
| 48.1 | agent | Repository: `LoadTenantIDsForPrincipal` + extend `RevokeSessionsForPrincipal` to return `(count, tenantIDs, err)` | **Done** |
| 48.2 | agent | Admin CLI: wire `authmetrics.Recorder`, observe per tenant, log-dump emission | **Done** |
| 48.3 | agent | Update `metrics-catalog.md` `auth_session_revoke_total` row status | **Done** |
| 48.4 | agent | Gate: `go vet`, `go test`, `check-changelog`, `release-dry-run` | pending |

## Acceptance Criteria

- [x] ADR-065 written and indexed in `docs/adr/README.md`
- [x] `LoadTenantIDsForPrincipal` added to `authpostgres.Repository`
- [x] `RevokeSessionsForPrincipal` returns `(int64, []string, error)` with serializable transaction
- [x] Admin CLI `revoke-session` creates `prometheus.Registry` + `authmetrics.Recorder` and observes per active tenant
- [x] `dumpMetrics` logs one structured JSON line per metric family
- [x] `TestDumpMetricsLogsOneLinePerFamilyForRevokeSession` passes
- [x] `TestDumpMetricsIsNoopForNonMetricOperations` passes
- [x] `go vet ./control-plane/auth/...` exits 0
- [x] `go test ./control-plane/auth/...` exits 0
- [ ] `python scripts/check-changelog.py` exits 0
- [ ] `python scripts/release-dry-run.py` exits 0
- [ ] `docs/observability/metrics-catalog.md` `auth_session_revoke_total` row updated
- [ ] CHANGELOG `[Unreleased]` contains Phase 22 entry

## Non-Goals (Phase 22+)

- Slice 43.1.5: API Server sign-in handler emission (needs new RPC + RBAC role — AGENTS.md §8 decision gate)
- Slice 43.3.5: Controller reconcile-path emission (needs durable commit decision — ADR-053/ADR-026)
- Java data-plane emission (ADR-051 §7 `26.F9`) — Phase 23+ candidate

## ADR Cross-References

- [ADR-058](adr-058-observability-catalog-backlog.md) — Phase 17 activation: recorder owner, call site, label normalization contract
- [ADR-061](adr-061-phase19-connection-test-recorder-migrate.md) — Phase 19 closeout: emission sub-slice open questions
- [ADR-063](adr-063-phase21-freetext-replication-recorder-migrate.md) — Phase 21: FreeText helper + replication Recorder migrate
- [ADR-065](adr-065-phase22-auth-session-revoke-emission.md) — Phase 22: this phase's umbrella decision

## Key Design Decisions

### Log-dump emission for one-shot CLI

The admin CLI is a one-shot offline utility that exits after each
operation. It cannot serve a `/metrics` HTTP endpoint in the traditional
Prometheus pull model. Instead, Phase 22 uses a **log-dump emission
pattern**: after the operation succeeds, the CLI logs one structured
JSON info line per metric family in the registry. Grafana Agent or
Prometheus log-based service discovery can parse these lines and
translate them into Prometheus samples. This is documented in ADR-065
§"Alternatives Considered".

### Tenant ID derivation

The sessions table (`astrasync_auth_sessions`) has no `tenant_id`
column. A principal can hold memberships in multiple tenants
(`astrasync_auth_memberships` PK = `(tenant_id, principal_id)`).
`RevokeSessionsForPrincipal` derives `tenant_id` by joining sessions
to memberships in a serializable transaction, returning one
`tenant_id` per unique active membership. The CLI observes once per
unique tenant, so dashboards that query `rate(auth_session_revoke_total)`
must account for one-sample-per-tenant (not one-sample-total).

### Transaction scope

`RevokeSessionsForPrincipal` uses `sql.LevelSerializable` to ensure
the session count and the tenant ID list are from the same snapshot.
This prevents a race where a new membership is added between
counting tenants and revoking sessions, which would cause the
emission to be inconsistent with the actual revoke outcome.
