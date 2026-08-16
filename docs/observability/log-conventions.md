# Log Conventions

The log conventions are the source of truth for the logger name,
the structured fields, and the log level guidance that every
AstraSync component follows. The conventions lock the shape so the
SLO handbook and the dashboard recipes can join the log records to
the metrics and the audit rows.

## Logger naming

### Control plane Go

The Go control plane uses the standard library `log` package or
`go.uber.org/zap`. The choice is split:

- The Controller uses `sigs.k8s.io/controller-runtime/pkg/log/zap`
  because the controller-runtime convention expects `zap`. The
  logger name matches the component: `controller-runtime` for the
  framework, `controller` for the AstraSync-specific calls.
- The API Server, the Console, and the auth module use the standard
  library `log` package. The logger name is the package path; the
  follow-up migration to zap will preserve the package path as the
  logger name.

The follow-up slice that migrates the API Server, the Console, and
the auth module to zap records the convention. The handbook
references the convention today so the dashboard recipes can be
authored against the eventual shape.

### Java data plane

The Java data plane uses SLF4J 2.0.13 with Logback 1.5.6. The
logger name is the fully qualified class name of the calling
class. The convention is the SLF4J default; the handbook records
the rule so the migration follow-up can adopt it without overriding
the SLF4J default.

The current state is that the Java executables (Coordinator, Worker)
use `System.out.printf` and `System.err.printf`. The migration to
SLF4J is a follow-up slice. The migration follow-up records the
logger name once the first class is migrated; the handbook
documents the convention so the migration is mechanical.

## Structured fields

Every log record carries the following structured fields where
applicable:

| Field | Type | Description |
|---|---|---|
| `request_id` | UUID | The request ID assigned by the API Server or the data plane. Joins the record to the audit table and to a Prometheus exemplar. |
| `tenant_id` | UUID | Tenant UUID. Dropped on logs that are not tenant-scoped. |
| `job_id` | UUID | Job UUID. Dropped on logs that are not job-scoped. |
| `component` | string | Component name (`apiserver`, `controller`, `coordinator`, etc.). |
| `epoch` | integer | Execution epoch of the Job. Set on every Coordinator and Worker log. |
| `stage` | string | Lifecycle stage (`read`, `transform`, `write`, `checkpoint`). Used by the dashboard recipes. |
| `outcome` | string | Outcome label (`success`, `failure`, `rejected`). Used by the dashboard recipes. |

The structured fields are emitted in addition to the unstructured
message. The deployment log store (Loki, Elasticsearch, etc.)
ingests the structured fields as labels or columns; the populated
dashboard joins the fields to the Prometheus metrics by the
`request_id` key.

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
- `FATAL` — fault that requires the process to terminate. The
  page-after-restart threshold.

The Helm chart exposes `logging.level: INFO` as the default. The
operator can override the level per component through the chart
values. The handbook does not record the chart values; the chart
records the values.

## Message format

The default message format is line-delimited JSON. The Helm chart
exposes `logging.pattern` for the operator to override the format
to a human-readable template. The data plane Java executables
follow the SLF4J + Logback default; the JSON layout is the
`logstash-logback-encoder` default.

The handbook rule is: a deployment that uses a structured log store
(Loki, Elasticsearch) must override the pattern to JSON. A
deployment that uses a human-readable store (plain text files,
syslog) can keep the human-readable pattern.

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

The two records share the `request_id` field. The
[`audit-correlation.md`](audit-correlation.md) document records the
join procedure.

## Follow-up migration

The follow-up slice that migrates the Java data plane to SLF4J is
expected to:

1. Replace every `System.out.printf` and `System.err.printf` call
   with a SLF4J `Logger` call. The logger name is the calling
   class's fully qualified name.
2. Add the Logback configuration that emits the line-delimited
   JSON layout by default.
3. Replace every `System.out.printf` and `System.err.printf` call
   in the test code with the SLF4J + Logback test layout that the
   test framework expects.

The follow-up slice must update the
[`metrics-catalog.md`](metrics-catalog.md) document to record the
Prometheus client registration in the Java module.

The follow-up slice that migrates the API Server, the Console, and
the auth module to zap is expected to:

1. Replace every `log.Printf` call with a `zap.Logger` call.
2. Add the structured field emitters listed in the §"Structured
   fields" table.
3. Wire the `request_id` key through the gRPC interceptor and the
   HTTP handler.

The follow-up slices are recorded in the Phase 7 ADR-047
Consequences section so the boundary between the documentation-only
Slice 26 and the code-migration follow-ups stays clear.

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