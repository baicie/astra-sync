# Phase 0 Hardening Design

## Context

Slices 01-04 establish the bounded synchronous path, strict JobSpec, CSV/JDBC connectors, and a
runnable CLI. The remaining production-facing gaps are control at lifecycle boundaries and
machine-readable evidence. Hardening must not smuggle in an async runtime, stronger delivery
guarantee, or persistent observability subsystem.

## Cancellation Flow

```text
embedding caller -> CancellationToken
                         |
                         v
JobCompiler -> materialize -> open Source -> [check] -> open Sink
                                      -> read batch -> transform -> [check] -> write batch
                                      -> close Sink -> close Source
```

The token is queried only by the synchronous Engine. A cancellation observation becomes a
`SyncJobException(CANCELLED, partialResult)` and follows the existing close path. The token is
not passed into connector APIs, so a connector call already in progress remains bounded by its
own timeout/driver behavior. The `LocalJobRunner` overload makes cancellation available to
embedding code while the CLI retains the never-cancelled default.

## Metrics Report Flow

`RunCommand` selects a `ReportFormat` enum from `--metrics`. It delegates plan and terminal metrics
to a report serializer that writes one line/object. Text mode preserves the Slice 03 line. JSON
mode uses a Jackson `ObjectMapper` and an insertion-ordered map with only the ADR-020 fields. The
same formatter handles success, validation/input failures, runtime stages, and cancellation; it
never serializes the JobSpec or connector options.

## Resource and Failure Test Matrix

| Boundary | Evidence |
|---|---|
| cancellation before Source open | factory/lifecycle counters stay zero |
| cancellation after Source open | Source closes; Sink never opens |
| cancellation before Sink write | Source and Sink close; read counter is partial |
| Source/Sink close failure | primary stage remains; close failure is suppressed |
| JDBC write failure | current batch rolls back; prior commits remain |
| JSON report | parser accepts exactly one object and forbidden values are absent |
| boundedness | slow Sink sees no next Source pull before current write returns |

## Examples and Evidence

The examples index distinguishes runnable CSV packaging from H2-backed connector tests. It states
that H2 is test-only, output files use create-new semantics, JDBC schemas must pre-exist, and the
runtime is at-most-once with possible partial output. Verification records exact commands and
commit IDs; release evidence is repository documentation, not a generated binary or persistent
metrics store.

## Change Control

Adding forced interruption, signal handling, async execution, persistent metrics, report version
negotiation, retries, checkpoints, or stronger delivery semantics requires a new design and ADR.
