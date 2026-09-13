# Phase 41 - Repository-Level Epoch Fencing

## Status

**Complete.**

Phase 41 closes the Phase 36 stale-writer follow-up by enforcing epoch
monotonicity at both the memory and PostgreSQL Job repository update
boundaries.

ADR: [ADR-091](../adr/adr-091-repository-epoch-fencing.md)

---

## Goal

Reject a writer that:

- holds an older execution snapshot;
- supplies the current resource version; and
- attempts to persist a lower Job epoch.

This prevents version checking from being bypassed by a stale writer while
preserving normal optimistic concurrency conflicts.

---

## Delivered Files

```text
control-plane/job/
├── lifecycle.go
├── memory/
│   ├── repository.go
│   └── repository_test.go
└── postgres/
    ├── repository.go
    └── repository_integration_test.go

docs/adr/
└── adr-091-repository-epoch-fencing.md

docs/phase41/
└── README.md
```

---

## Fence Contract

`job.ValidateEpochMonotonicity` returns `job.ErrStaleEpoch` when:

```text
candidate.status.epoch < stored.status.epoch
```

Equal epochs are accepted. Higher epochs are accepted for restart and
execution takeover. The PostgreSQL repository embeds the same check in the
conditional `UPDATE` statement so the check and mutation are atomic.

---

## Test Coverage

### Memory repository

`TestRepositoryRejectsStaleEpochWriter`:

1. progress a Job through epoch 1 and a terminal state;
2. restart it into epoch 2;
3. replay the epoch-1 snapshot with the current version;
4. assert `ErrStaleEpoch`;
5. assert the stored epoch, state, and version remain at epoch 2.

### PostgreSQL repository

`TestRepositoryFencesStaleEpochWriter` performs the same sequence against a
real PostgreSQL container and verifies that the conditional update rejects
the stale snapshot without changing durable state.

---

## Acceptance Criteria

- [x] Epoch monotonicity is enforced by a shared Job domain helper.
- [x] The memory repository rejects a lower epoch.
- [x] PostgreSQL rejects a lower epoch atomically with the write.
- [x] Equal and increasing epochs remain valid.
- [x] Integration coverage uses the real PostgreSQL adapter.
- [x] No schema migration or new dependency is introduced.
- [x] ADR-091 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 41 entry.

---

## Non-Goals

- No Scheduler or Controller state-machine change.
- No checkpoint or WAL protocol change.
- No new epoch allocation mechanism.
- No change to optimistic version conflict semantics.

---

## Rollback

Remove the shared monotonicity helper, the memory repository guard, the
PostgreSQL guard predicate, and the Phase 41 docs/index entries. Production
state-machine behavior remains otherwise unchanged.
