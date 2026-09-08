# ADR-079: Phase 31 Implementation Corrections — MutationRepository, Metadata Key, Migration Fallback

## Status

Proposed

## Context

After landing ADR-076 (Phase 31 entry package) and ADR-077 + 078
(Phase 27 / 28 / 29 audit backfill), Phase 31 implementation
requires writing `tests/cross-module/chain-tenant-id/chain_test.go`.
A pre-implementation audit of the assumptions in ADR-076 §2 / §4
/ §7 against the actual `control-plane/job`,
`control-plane/api-server/internal/authn`, and the existing
`job_mutation_service_test.go` test surface revealed three
corrections.

### Correction 1: `Memory.Repository` does not implement `MutationRepository`

ADR-076 §2.3 states:

> Uses an in-memory `Memory.Repository` (existing project pattern)
> for the api-server's PostgreSQL dependency, so the mutation
> repository writes into `Memory` rather than a real PostgreSQL.

`control-plane/job/memory/repository.go` defines:

```go
type Repository struct {
    mu   sync.RWMutex
    jobs map[job.Key]job.Job
}
```

with methods `Create`, `Get`, `List`, `Update`, `Delete`. It does
**not** implement:

```go
type MutationRepository interface {
    Repository
    ReplayMutation(context.Context, Mutation) (MutationResult, bool, error)
    ApplyMutation(context.Context, Mutation) (MutationResult, error)
}
```

(from `control-plane/job/mutation.go`).

The existing `job_mutation_service_test.go` (the project's
established test fake pattern) handles this by **embedding**
`job.Repository` (an interface, not a struct) and **overriding**
`ApplyMutation` / `ReplayMutation` directly:

```go
type recordingJobMutationRepository struct {
    job.Repository
    mutation    job.Mutation
    replay      job.MutationResult
    replayFound bool
    applyCalls  int
}

func (r *recordingJobMutationRepository) ApplyMutation(
    _ context.Context, mutation job.Mutation,
) (job.MutationResult, error) {
    r.mutation = mutation
    // ...
}
```

The cross-module fixture in `tests/cross-module/chain-tenant-id/`
must adopt the same pattern, not the ADR-076 wording.

### Correction 2: `x-astra-tenant-id` metadata key is canonical, lower-case

ADR-076 §4.2 implicitly assumes the metadata key without stating
the canonical form. The constant is defined in
`control-plane/api-server/internal/authn/tenant_metadata.go`:

```go
const TenantMetadataKey = "x-astra-tenant-id"
```

Note that **gRPC metadata keys are case-insensitive but stored
lowercase** in the canonical form returned by
`metadata.ValueFromIncomingContext`. The fixture must use
`metadata.AppendToOutgoingContext(ctx, "x-astra-tenant-id", ...)`
verbatim.

### Correction 3: Migration dry-run fallback is necessary, not optional

ADR-076 §7 says:

> If neither library is acceptable (binary-size, CVE surface), the
> test falls back to a migration SQL parse + dry-run assertion that
> verifies the migration is **applied** at test setup time using
> the project's existing `migrations.Manager.Apply` interface
> against the in-memory adapter.

The "in-memory adapter" referenced here does not exist today. The
api-server consumes `control-plane/job/postgres/mutation_repository.go`
directly against a `*sql.DB`. The fixture therefore has two options:

1. **Spin up a real PostgreSQL via testcontainers** — adds the
   `github.com/testcontainers/testcontainers-go/modules/postgres`
   dependency to the cross-module fixture module, which doubles
   the module's dependency surface and pulls in Docker as a
   runtime requirement.
2. **Verify the migration is parseable and contains the expected
   column + index DDL** — uses only `os.ReadFile` and string
   matching; this is what ADR-076 §7's dry-run fallback actually
   describes.

The fixture cannot exercise the live migration because the
in-memory adapter does not exist. The dry-run fallback is
therefore the **only** viable option in this slice. ADR-076 §7
is correctly written but reads as if testcontainers is the
default and dry-run is the fallback; the truth is the reverse.

## Decision

### 1. Adopt the recording-fake pattern for `MutationRepository`

The cross-module fixture introduces a test fake
`recordingCrossModuleMutationRepository` whose shape mirrors
`recordingJobMutationRepository`:

```go
type recordingCrossModuleMutationRepository struct {
    job.Repository
    mu        sync.Mutex
    captured  []job.Mutation
    applyErr  error
}
```

It embeds the **interface** `job.Repository` (not a concrete
`*memory.Repository`) so the override of `ApplyMutation` does
not conflict with the embedded methods. This matches the
existing pattern in
`control-plane/api-server/internal/service/job_mutation_service_test.go`.

### 2. Use `TenantMetadataKey` constant from `authn`

The fixture imports
`io.astrasync/control-plane/api-server/internal/authn` and uses
the exported `authn.TenantMetadataKey` constant rather than a
literal string. This guarantees the fixture stays in sync with
the canonical key.

### 3. Migration test = dry-run SQL parse

The fifth test case,
`TestCrossModule_APIServerMigration003PersistsVerifiedTenantID`,
becomes a two-part assertion:

- **Part A (SQL parse)**: `os.ReadFile` reads the migration file
  and asserts (a) it contains `ALTER TABLE astrasync_control_jobs
  ADD COLUMN IF NOT EXISTS tenant_id UUID` and (b) it creates the
  index `astrasync_control_jobs_tenant_id_idx`.
- **Part B (insert path)**: the fixture issues one mutation
  through the gRPC server and asserts
  `repo.captured[0].TenantID == metadata.declaredTenantID`. This
  pins the write path. (The schema-side coverage is part A only;
  a future slice that adopts testcontainers can extend part B to
  hit a real PostgreSQL without changing the test contract.)

The dry-run is the **default**, not a fallback. ADR-076 §7
wording is amended by this ADR to read:

> The default implementation uses a migration SQL parse + insert-
> path assertion. Live testcontainers is a future-slice extension.

### 4. ADR-076 §2.3 wording amendment

The §2.3 sentence is replaced with:

> 3. The fixture wraps a recording `MutationRepository` test
>    fake (mirroring
>    `control-plane/api-server/internal/service/job_mutation_service_test.go`'s
>    `recordingJobMutationRepository` pattern). The fake records
>    the `Mutation` it receives so the test asserts on
>    `repo.captured[len(repo.captured)-1].TenantID` directly.
>    No real `MutationRepository` implementation is consumed.

### 5. New ADR row

`docs/adr/README.md` adds an ADR-079 row marked Proposed. The
CHANGELOG Unreleased / Added entry summarises the three
corrections.

## Consequences

### Positive

- The fixture implementation can now proceed without ambiguity.
  Each of the three corrections closes a gap between the design
  and the existing test patterns.
- The recording-fake pattern is reusable for future chain tests
  (Phase 32 cross-module controller emission).
- The migration dry-run is deterministic, fast (<10ms), and
  requires no Docker / testcontainers dependency.
- The fixture module's `go.mod` does not need to import
  testcontainers, keeping its dependency surface small.

### Negative

- The migration test's schema-side coverage is parse-based, not
  execution-based. A future typo in `003_jobs_tenant_id.sql`
  that passes the string match (e.g. a UUID-with-lowercase)
  would not be caught. This is acceptable because the migration
  file is small (27 lines), peer-reviewed, and already covered
  by the existing postgres integration tests when those run
  against a real database.
- The fixture does not catch a regression where the production
  path stops calling `applyJobMutation` (the INSERT path). This
  is caught by the existing
  `job_mutation_service_test.go::TestTransactionalJobCreatePersistsCanonicalFence`
  in the api-server module.

## Follow-ups (out of scope for this ADR)

- **Phase 31 implementation**: write
  `tests/cross-module/chain-tenant-id/go.mod` and
  `chain_test.go` with the five cases described in ADR-076 §4,
  applying corrections 1 / 2 / 3 from this ADR.
- **Future slice**: adopt `testcontainers-go` for live
  PostgreSQL coverage of the migration (ADR-076 Follow-ups
  bullet 2). Not in this slice.
- **Phase 32**: extend the fixture to cover Controller
  emission, applying the same recording-fake pattern to the
  controller's `Emitter` interface.
