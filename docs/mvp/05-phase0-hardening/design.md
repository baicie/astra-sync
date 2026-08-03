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

If the embedding callback throws, the Engine reports `CANCELLATION_CHECK` with partial metrics
instead of allowing an unstructured exception to escape. This keeps reverse close and suppressed
failure behavior intact without misreporting a callback defect as user cancellation.

## Metrics Report Flow

`RunCommand` validates the `--metrics` string as `text` or `json` and writes one line/object through
shared reporting helpers. Text mode preserves the Slice 03 line. JSON mode uses a Jackson
`ObjectMapper` and an insertion-ordered map with only the ADR-020 fields. The report paths handle
success, validation/input failures, runtime stages, and cancellation without serializing the
JobSpec or connector options.

Picocli parameter failures use the same output contract and JSON serializer. A small bootstrap
inspection recognizes only the `--metrics` option and does not include rejected argument text in
diagnostics. Unexpected runtime failures with no `SyncResult` use `stage=UNKNOWN` and zero counters.
Both the root command and `run` subcommand expose the same explicit version string. Bootstrap
inspection stops at the `--` end-of-options marker, does not consume an option token as a missing
metrics value, and uses the last complete selector before that boundary.

## Resource and Failure Test Matrix

| Boundary | Evidence |
|---|---|
| cancellation before Source open | factory/lifecycle counters stay zero |
| cancellation after Source open | Source closes; Sink never opens |
| cancellation before Sink write | Source and Sink close; read counter is partial |
| Source/Sink close failure | primary stage remains; close failure is suppressed |
| JDBC write failure | current batch rolls back; prior commits remain |
| JSON report | parser accepts exactly one object and forbidden values are absent |
| invalid CLI arguments | requested JSON shape is preserved and raw arguments are absent |
| cancellation callback failure | stage/counters survive; close failures are suppressed |
| exception serialization | stage and partial result survive a Java serialization round trip |
| JDBC `TIME_WITH_TIMEZONE` | rejected for populated and empty results with column/type evidence; no offset is discarded |
| boundedness | slow Sink sees no next Source pull before current write returns |

## Examples and Evidence

The examples index distinguishes runnable CSV packaging from H2-backed connector tests. It states
that H2 is test-only, output files use create-new semantics, JDBC schemas must pre-exist, and the
runtime is at-most-once with possible partial output. Verification records exact commands and
commit IDs; release evidence is repository documentation, not a generated binary or persistent
metrics store.

## CI Scope

The Java gate runs the full Maven `verify` lifecycle and Spotless for every change because the
Phase 0 deliverable is Java. Go control-plane and Helm checks run when their owned paths change;
planned, untouched subsystem skeletons do not block a Java-only MVP branch. Protocol validation
continues to run independently of Helm. Repository policy and secret scanning always run with full
Git history so pull-request commit ranges are resolvable.

## Change Control

Adding forced interruption, signal handling, async execution, persistent metrics, report version
negotiation, retries, checkpoints, or stronger delivery semantics requires a new design and ADR.
