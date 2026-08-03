# Phase 0 MVP Delivery Plan

## Status

Slice 04 is verified on `mvp/04-jdbc-vertical-slice`; slices 01 through 04 are complete and
Phase 0 remains in progress for the final hardening slice.

## Goal

Prove a complete, honest, and bounded synchronization path in one Java 21 process:

```text
versioned JobSpec -> validation and capability negotiation
                  -> Source -> bounded batches -> Sink
                  -> terminal result and metrics
```

The completed Phase 0 must run file and JDBC full-load jobs without requiring the Go control
plane, Kafka, etcd, object storage, RocksDB, Kubernetes, or CDC infrastructure.

## Baseline

- Base commit: `main@8b237b7` (`chore(repo): initialize astrasync project`).
- No `develop` branch or remote tracking reference is currently available.
- The initial commit contains architectural API sketches that do not compile. Restoring a
  reproducible Java build is therefore an entry gate, not optional cleanup.
- The repository already had an untracked single-node prototype when planning began. It is
  treated as input and is brought under the design and tests in slice 01.

## Scope

- Strict, versioned JobSpec parsing and deterministic validation.
- Connector discovery and capability negotiation before any connector is opened.
- A public Java in-process Source/Sink SPI with bounded row batches.
- A logical row representation that supports nulls and common JDBC/CSV values.
- A synchronous single-node direct pipeline with natural backpressure.
- CSV file source/sink and generic JDBC source/sink.
- Basic records, batches, duration, and failure-stage metrics.
- A CLI entry point and executable end-to-end examples/tests.
- Explicit rejection of unsupported delivery guarantees, especially exactly-once before
  checkpointing and transactional commit exist.

## Non-goals

- CDC or snapshot-to-CDC handoff.
- Distributed coordinator/worker scheduling, network exchange, or dynamic splits.
- Checkpoints, restart recovery, savepoints, RocksDB, object-state storage, or two-phase commit.
- End-to-end exactly-once.
- Durable relay, batch materialization, Arrow, spill, or adaptive parallelism.
- Go control-plane services, HA, Kubernetes operation, console, RBAC, or multi-tenancy.
- Out-of-process or multi-language connectors.

## Branch Slices

The slices are stacked while Phase 0 is being developed. A new branch is created only after its
predecessor passes its verification gate. After a slice is merged, the next branch can be
rebased onto the updated `main` without changing its scope.

| Order | Branch | Parent | Deliverable | Status |
|---:|---|---|---|---|
| 01 | `mvp/01-single-node-kernel` | `main@8b237b7` | Reproducible build, bounded pull runtime, lifecycle, failure stage, metrics, unit tests | Complete |
| 02 | `mvp/02-job-spec-connector-spi` | accepted 01 | Row/batch contract, Source/Sink SPI, JobSpec v1, registry, capability compiler | Complete |
| 03 | `mvp/03-file-vertical-slice` | accepted 02 | CSV source/sink, file-to-file CLI job, malformed-row policy | Complete |
| 04 | `mvp/04-jdbc-vertical-slice` | accepted 03 | Generic JDBC source/sink, type mapping, transaction boundaries, integration tests | Complete |
| 05 | `mvp/05-phase0-hardening` | accepted 04 | Metrics output, cancellation/resource tests, examples, acceptance and release evidence | Planned |

Branch names intentionally use the user-requested `mvp/*` namespace. The repository's generic
`feature/*` guidance predates this delivery plan and refers to a nonexistent `develop` branch.

## Cross-slice Design Rules

1. Design and ADR changes are committed before or with the first implementation that depends on
   them.
2. Source reads and sink writes are batch-bounded. No unbounded collection or executor queue is
   permitted in the data path.
3. The runtime does not prefetch the next batch while the current batch is being written. This
   supplies deterministic in-process backpressure.
4. JobSpec and capability errors fail before Source or Sink `open`.
5. Connector resources have explicit open/close ownership. A primary failure is preserved and
   close failures are attached as suppressed exceptions.
6. Unknown configuration fields and unsupported guarantees are errors, not warnings.
7. Tests must prove behavior and resource bounds; a successful compilation alone is not a gate.

## Phase 0 Acceptance

- A valid CSV-to-CSV JobSpec runs through the CLI and produces deterministic output.
- JDBC-to-JDBC full load is exercised against an isolated test database and preserves null,
  Unicode, decimal, timestamp, binary, and large text values in the documented support matrix.
- File-to-JDBC and JDBC-to-file paths use the same runtime and connector SPI.
- Invalid JobSpec, missing connector, and capability conflicts fail before either connector opens.
- Requesting `EXACTLY_ONCE` is rejected. The MVP reports its actual at-most/at-least-once
  behavior and partial-write risk.
- With a deliberately slow Sink and a source larger than one batch, the configured batch limit
  is never exceeded and the Source does not run ahead of the Sink.
- Source, transform, and Sink failures identify the stage, close every opened resource, and
  preserve partial counters.
- A connector can be registered without modifying the execution loop.
- `mvn test` succeeds from a clean checkout with no external production service.

## Definition of Done for a Slice

- Scope and non-goals are linked from the slice README.
- Design and relevant ADRs are present and internally consistent.
- Implementation and tests satisfy every slice acceptance item.
- Formatting/static checks and the documented test command pass.
- `verification.md` records commands, environment, results, limitations, and the resulting commit.
- The next branch is not created until these gates pass.

## Risk Register

| Risk | Control |
|---|---|
| Long-term distributed abstractions overwhelm the first usable path | Keep Phase 0 single-process and linear while preserving connector boundaries |
| Fake exactly-once promise | Reject it until checkpoint plus replayable source and transactional/idempotent sink are implemented |
| Source overloads a production database | Bound fetch size, batch size, connection count, query timeout, and document read-only usage |
| JDBC type corruption | Maintain an explicit type matrix and integration fixtures for time, decimal, binary, null, and LOB values |
| Memory grows with input size | Pull one bounded batch at a time and prove no prefetch with tests |
| Partial writes are mistaken for recovery | Report failure stage/counters and document restart duplicate risk |
| Initial scaffolding hides build failures | Make a clean reactor build the first slice gate |

## Records

- [Architecture baseline](../architecture.md)
- [ADR index](../adr/README.md)
- [Slice 01](01-single-node-kernel/README.md)
- [Slice 02](02-job-spec-connector-spi/README.md)
- [Slice 03](03-file-vertical-slice/README.md)
