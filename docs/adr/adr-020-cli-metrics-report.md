# ADR-020: CLI Metrics Report Contract

## Status

Accepted

## Date

2026-08-03

## Context

The CLI currently emits a concise human-readable success line, but automation and release
acceptance need stable counters and failure-stage evidence without parsing prose. Metrics must not
include connector option values, credentials, SQL, or file paths. The report must also preserve the
existing text default for operators using the Slice 03 command.

## Decision

`run` accepts `--metrics text|json`, defaulting to `text`. Text output remains the existing
single-line `SUCCEEDED`/`FAILED` format. JSON mode emits exactly one object on the selected stream:

```json
{"status":"SUCCEEDED","job":"csv-file-copy","deliveryGuarantee":"at-most-once","recordsRead":2,"recordsWritten":2,"batches":2,"maxBatchRecords":2,"elapsedMillis":17}
```

Successful reports contain only stable plan identity, actual delivery guarantee, bounded counters,
and elapsed duration. Runtime failures contain `status`, `category`, `stage`, `message`,
`recordsRead`, and `recordsWritten`; input/validation failures contain `status`, `category`, and
one sanitized message. New fields are additive and their names use lower camel case. JSON strings
are escaped by a standard Jackson serializer, not hand-built concatenation.

The selected format also applies when Picocli rejects the command before invoking `RunCommand`,
including missing parameters and unknown options. The parameter-error path emits one sanitized
report and never echoes the rejected argument values. When an unexpected runtime failure has no
Engine result, it uses stage `UNKNOWN` with zero read/write counters; these values mean execution
metrics were unavailable, not that a completed job processed zero records.

Bootstrap format detection follows command-line token boundaries: `--` ends option recognition,
and a valueless `--metrics` does not consume a following option token. If more than one complete
selector occurs before that boundary, the last selector determines the parameter-error format.

Cancellation uses the same report contract with category `cancelled` and exit code 5. Exit codes
0, 2, 3, and 4 retain their Slice 03 meanings. Reports never serialize connector option maps or
exception causes.

## Consequences

### Positive

- Scripts can consume deterministic JSON without scraping human prose.
- The default CLI behavior remains backwards compatible.
- Reports expose partial counters and failure stages while avoiding secret-bearing values.
- Automation receives the selected output format for parse-time and execution-time failures.

### Negative

- The JSON schema is intentionally small and does not persist historical metrics.
- Duration is informative and not a performance SLA; field values vary by run.
- A connector error message may still be implementation-specific, so only its boundary fields are
  stable.

## Alternatives Considered

### Always Replace Text with JSON

Rejected because existing operators and Slice 03 examples depend on concise text output.

### Write Metrics to a Sidecar File

Deferred because file publication and overwrite semantics would add another lifecycle contract;
stdout/stderr is sufficient for Phase 0 acceptance.

### Serialize Full JobSpec or Connector Options

Rejected because reports must not leak credentials, SQL, paths, or other option values.

## Related Decisions

- ADR-011: Bounded Pull-based Single-node Runtime
- ADR-014: Local Runner and CLI Boundary
- ADR-019: Cooperative Cancellation Boundary
