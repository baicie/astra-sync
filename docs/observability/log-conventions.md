# Log Conventions

The log conventions are the source of truth for the logger name,
the structured fields, and the log level guidance that every
AstraSync component follows. The conventions lock the shape so the
SLO handbook and the dashboard recipes can join the log records to
the metrics and the audit rows.

## Logger naming

### Control plane Go

The Go control plane uses the standard library `log/slog` package or
`go.uber.org/zap`. The choice is split:

- The Controller uses `sigs.k8s.io/controller-runtime/pkg/log/zap`
  because the controller-runtime convention expects `zap`. The
  logger name matches the component: `controller-runtime` for the
  framework, `controller` for the AstraSync-specific calls.
- The API Server, the Console, the Scheduler, the Connection Test
  Executor, and the auth admin CLI use the standard library `log/slog`
  package. Each executable owns a module-local `newComponentLogger`
  constructor; there is no cross-module helper module or local Go
  `replace`. The constructor installs a JSON handler that emits the
  `component` field on every record. The component value matches the
  deployment name: `apiserver`, `console`, `scheduler`,
  `connection-test-executor`, `astra-auth-admin`.

The migration to `slog` is recorded in [`changelog.md`](changelog.md)
as F3.

### Java data plane

The Java data plane uses SLF4J 2.0.13 with Logback 1.5.6. The
logger name is the fully qualified class name of the calling
class. The convention is the SLF4J default; the handbook records
the rule so the migration follow-up can adopt it without overriding
the SLF4J default.

The Java executables (Coordinator, Worker) retain selected
`System.out.printf` and `System.err.printf` calls for stable CLI output
and Kubernetes liveness probes. As of Phase 7 Slice 26 follow-up
(F2), the error path of `CoordinatorApplication`,
`WorkerApplication`, and `ExecutionHeartbeat` is routed through SLF4J
`Logger.error(...)` calls; the stable output calls are preserved. The
Logback configuration file shipped under
`engine/coordinator/src/main/resources/logback.xml` and
`engine/worker/src/main/resources/logback.xml` installs the
`LogstashEncoder` from `logstash-logback-encoder` so every record
emits the `component` field described in §"Structured fields". The
migration is recorded in [`changelog.md`](changelog.md) together
with the source commits `a188011`, `7047479`, `b2a9bbe`, `070bad2`,
and `7556e0a` (F1 and F2).

## Structured fields

The table below is the stable field contract. A call site emits a field
only when that context is available; the current closeout guarantees
`component` at each migrated entry point but does not yet propagate all
request-, tenant-, job-, epoch-, or stage-scoped fields.

| Field | Type | Description |
|---|---|---|
| `request_id` | UUID | The request ID assigned by the API Server or the data plane. Joins the record to the audit table and to a Prometheus exemplar. |
| `tenant_id` | UUID | Tenant UUID. Dropped on logs that are not tenant-scoped. |
| `job_id` | UUID | Job UUID. Dropped on logs that are not job-scoped. |
| `component` | string | Component name (`apiserver`, `controller`, `coordinator`, etc.). |
| `epoch` | integer | Execution epoch of the Job. Set on every Coordinator and Worker log. |
| `stage` | string | Lifecycle stage (`read`, `transform`, `write`, `checkpoint`). Used by the dashboard recipes. |
| `outcome` | string | Outcome label (`success`, `failure`, `rejected`). Used by the dashboard recipes. |

Structured fields are emitted in addition to the message. The deployment
log store (Loki, Elasticsearch, etc.) can ingest them as labels or columns.
Direct `request_id` correlation becomes available only after the relevant
request interceptors and business call sites attach that field.

The fields are stable. A change to a field name or type is a
breaking change and requires a corresponding update to the
[`metrics-catalog.md`](metrics-catalog.md) and the
[`audit-correlation.md`](audit-correlation.md) documents.

## Log levels

The control plane and the data plane use the standard log levels:

- `DEBUG` — per-record diagnostic. Disabled in production.
- `INFO` — lifecycle event. The default production level.
- `WARN` — recoverable fault. The operator notices but does not
  page.
- `ERROR` — fault that requires operator attention. The page
  threshold.

Process-terminating faults are logged at `ERROR` before exit; neither
SLF4J nor `slog` defines a portable `FATAL` level.

The Helm chart exposes `logging.level: INFO` as the default. The
operator can override the level per component through the chart
values. The handbook does not record the chart values; the chart
records the values.

## Message format

The migrated Go entry points use `slog.NewJSONHandler`. Coordinator and
Worker use Logback's `LogstashEncoder`; both defaults are line-delimited
JSON. `logging.level` is wired to `LOG_LEVEL` independently of the metrics
toggle. The chart's legacy `logging.pattern` value is not consumed by these
entry points. A Java deployment can replace the Logback configuration with
`-Dlogback.configurationFile=<path>`; changing the Go format requires a
code or log-shipping configuration change.

## Sensitive fields

The log conventions forbid the following values in any log record:

- Credentials, secrets, or tokens. The structured fields are the
  only allowed place to record a credential reference; the secret
  value itself is never logged.
- SQL statements, file paths, or connector option values. The
  structured fields record the table name or the connector name;
  the SQL text and the option map are not logged.
- Free-form user input. The structured fields record the parsed
  value; the raw text is not logged.

The forbidden values are recorded in the security audit event
allowlist (ADR-042). The audit event allowlist is the source of
truth for the values the operator can see in the audit table; the
log conventions extend the allowlist to the log records.

## Security headers

The Slice 22 transport hardening (ADR-043) added security response
headers to the API Server and the Console. The log conventions
record the rule that the security header values are not logged.
The structured fields record the header name; the header value is
not logged.

## Example records

The following example records show the conventions applied to a
real event. The records are illustrative; the production code emits
the same fields with the same types.

```json
{
  "ts": "2026-08-15T10:00:00.000Z",
  "level": "INFO",
  "logger": "io.astrasync.engine.coordinator.CoordinatorApplication",
  "message": "checkpoint completed",
  "request_id": "f75f2e09-5d60-4647-be5d-7688e2b54909",
  "tenant_id": "11111111-1111-4111-8111-111111111111",
  "job_id": "22222222-2222-4222-8222-222222222222",
  "component": "coordinator",
  "epoch": 1,
  "stage": "checkpoint",
  "outcome": "success"
}
```

```json
{
  "ts": "2026-08-15T10:00:00.000Z",
  "level": "ERROR",
  "logger": "io.astrasync.controlplane.apiServer.accessService",
  "message": "authentication failed",
  "request_id": "f75f2e09-5d60-4647-be5d-7688e2b54909",
  "tenant_id": "11111111-1111-4111-8111-111111111111",
  "component": "apiserver",
  "outcome": "failure"
}
```

```json
{
  "ts": "2026-08-15T10:00:00.000Z",
  "level": "INFO",
  "logger": "io.astrasync.engine.coordinator.CoordinatorApplication",
  "message": "coordinator started",
  "component": "coordinator"
}
```

The two records share the `request_id` field. The
[`audit-correlation.md`](audit-correlation.md) document records the
join procedure.

## Follow-up migration

The Java data plane error-path migration to SLF4J is complete as of
Phase 7 Slice 26 follow-up F2:

1. Failures in `CoordinatorApplication`, `WorkerApplication`, and
   `ExecutionHeartbeat` emit SLF4J `Logger.error(...)` events. Stable CLI
   summaries and liveness output remain on `stdout`/`stderr` for
   compatibility. The logger name is the calling class's fully qualified
   name.
2. The Logback configuration in
   `engine/{coordinator,worker}/src/main/resources/logback.xml`
   installs the `LogstashEncoder` from `logstash-logback-encoder`
   so every record emits line-delimited JSON with the `component`
   field.
3. The new test classes (`CoordinatorLogbackConfigurationTest`,
   `CoordinatorApplicationLogbackTest`) drive the configuration
   through the SLF4J + Logback test layout that the JUnit 5 test
   framework expects.

The Go entry-point migration to `slog` is complete as of F3. Existing
startup, shutdown, and error records in `api-server`, `console`,
`scheduler`, `connection-test-executor`, and `astra-auth-admin` use JSON
loggers with `component`. Request-scoped `request_id`, `tenant_id`, and
`job_id` propagation remains follow-up instrumentation; logger tests only
verify that supplied structured fields are preserved.

The implementation commits are recorded in
[`changelog.md`](changelog.md).

## What the conventions do not record

- The exact log pattern. The pattern is the deployment's
  configuration; the conventions record the format choice.
- The log retention window. The retention window is the
  deployment's storage policy; the conventions record the rule that
  the retention window must not be shorter than the audit
  retention window in the same PostgreSQL database.
- The log shipping pipeline. The pipeline is the deployment's
  choice; the conventions record the format choice.

## Inline placeholders for the populated handbook

The operator populates the conventions by replacing every `<placeholder>`
value with the environment-specific value. The conventions record the
following inline placeholders that the populated deployment needs:

- `<log-level>` — the log level that the operator configures for the
  control plane and the data plane. The default is `INFO`.
- `<log-format>` — the log format that the operator configures for the
  deployment log store. The default is line-delimited JSON.
- `<retention-window-days>` — the log retention window in days. The
  retention window must not be shorter than the audit retention
  window in the same PostgreSQL database.

The conventions do not record the populated values. The populated
deployment is a deployment-owned artefact.
