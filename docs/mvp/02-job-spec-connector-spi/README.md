# Slice 02: JobSpec and Connector SPI

## Status

Design complete; implementation pending.

## Objective

Turn the slice-01 execution invariant into a usable public boundary: a strict versioned JobSpec
is parsed and compiled against registered connector descriptors before any connector instance or
external resource can be created.

## Included

- Immutable null-safe public `Row` and bounded `RowBatch` contracts.
- Public in-process batch Source/Sink SPI and immutable connector descriptors.
- A deterministic, duplicate-safe in-memory connector registry.
- Strict YAML/JSON parsing for `sync.astrasync.io/v1` `SyncJob` documents.
- Deterministic JobSpec validation and compilation to a value-only plan.
- Capability and role checks before connector materialization.
- Explicit Phase 0 rejection of at-least-once and exactly-once requests.
- Kernel migration from private record/batch values to the public connector contract.
- Removal of uncompiled connector/API sketches superseded by this slice.

## Excluded

- Concrete file or JDBC connectors, CLI execution, connection tests, and service discovery.
- Checkpoints, retries, committers, splits, CDC, Arrow, or distributed execution.
- Secret resolution and production connection catalogs.
- Executable transforms; a non-empty transform list is rejected rather than ignored.

## Acceptance

1. Valid YAML and JSON v1 documents produce the same immutable JobSpec.
2. Unknown or duplicate fields, unsupported versions/kinds, malformed values, and non-string
   connector options fail with a precise document path.
3. Rows preserve null values and defensively copy containers; batches reject null rows and empty
   non-terminal batches.
4. Connector registration rejects duplicate names and inconsistent role/capability descriptors.
5. Missing connectors, unsupported roles, missing batch capabilities, transforms, and unsupported
   guarantees fail during compilation.
6. Even connectors advertising replay and transactional commit cannot enable exactly-once while
   the Phase 0 runtime lacks checkpoint/commit coordination.
7. No rejected plan invokes a connector factory or opens a connector resource.
8. Equivalent input and registry descriptors produce equal compiled plans with stable option
   ordering.
9. The kernel still proves bounded pull, no prefetch, lifecycle ownership, and failure evidence
   while using the public row/batch SPI.
10. Focused and full Maven verification commands pass.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-012](../../adr/adr-012-strict-versioned-job-spec.md)
- [ADR-013](../../adr/adr-013-descriptor-first-connector-planning.md)
