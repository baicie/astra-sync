# Slice 02 Implementation Plan

## Preconditions

- [x] Start from verified slice 01 commit `ec1e7d2`.
- [x] Re-read the supplied architecture, Phase 0 plan, and ADR-003/004/008/009/011.
- [x] Inventory compiled and uncompiled connector/API sketches.
- [x] Record public contracts, strict parsing rules, compilation order, and acceptance tests.
- [x] Add ADR-012 and ADR-013 before changing public APIs or planning behavior.

## Steps

### 1. Establish the Public Data and Connector Contract

- Add immutable `Row`, `RowBatch`, connector configuration, roles, descriptors, and factory SPI.
- Add batch Source/Sink lifecycle interfaces with resource-free construction.
- Replace superseded Arrow/mutable-record and uncompiled connector sketches.
- Gate: connector-api contract tests pass without Arrow or runtime dependencies.

### 2. Implement Strict JobSpec v1

- Add immutable JobSpec values and strict YAML/JSON tree parsing.
- Reject duplicate/unknown fields, scalar coercion, unsupported versions, and invalid names.
- Normalize defaults and option ordering deterministically.
- Gate: valid parity and negative path-aware parser tests pass.

### 3. Add Registry and Descriptor-only Compilation

- Add deterministic registry registration, snapshots, and role lookup.
- Add compilation error codes and value-only compiled plans.
- Check role, batch capability, transforms, and delivery in fixed order.
- Gate: rejected plans call no factory creation or connector open method.

### 4. Migrate the Single-node Kernel

- Replace internal record/batch and Source/Sink contracts with the public SPI.
- Write one bounded transformed batch before the next Source poll.
- Retain lifecycle, failure stages, metrics, and suppressed close behavior.
- Gate: slice-01 behavioral tests pass against the public contract.

### 5. Verify and Archive

- Run focused connector-api and Engine verification, full Reactor tests, and formatting checks.
- Inspect public API references, final diff, and worktree status.
- Record commands, counts, coverage, limitations, and implementation commit.
- Update slice and Phase 0 status before creating slice 03.

## Change Control

Any lenient parsing, automatic guarantee downgrade, resource acquisition during compilation,
unbounded batch/queue, Arrow ownership, split/enumerator, or committer behavior requires a design
update and a new or superseding ADR before implementation.
