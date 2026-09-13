# ADR-091: Repository-Level Epoch Fencing

## Status

Accepted

## Context

ADR-006 requires stale execution writers to be rejected before they can
overwrite durable state. The Job lifecycle model already rejects stale epochs
when callers use `Job.Advance`, but repository `Update` methods only checked the
expected resource version.

That left a gap: a buggy or stale writer that supplied the current version
with an older Job snapshot could write the old epoch back into the repository.
Optimistic version checking was not sufficient when the stale writer presented
the latest version while carrying obsolete epoch state.

## Decision

Enforce epoch monotonicity at every Job repository update boundary:

1. Add `job.ValidateEpochMonotonicity(current, candidate)` as the shared
   domain rule. A candidate epoch lower than the stored epoch returns
   `job.ErrStaleEpoch`.
2. Apply the rule in the in-memory repository before storing the candidate.
3. Apply the rule inside the PostgreSQL `UPDATE` predicate:

   ```sql
   AND (status->>'epoch')::bigint <= $candidate_epoch
   ```

   This makes the epoch check and write one atomic database operation.
4. If the conditional update affects no row, classify the failure by reading
   the current row: version or identity mismatch is `ErrConflict`, a higher
   stored epoch is `ErrStaleEpoch`.

The rule permits equal or increasing epoch values and rejects only a
backwards epoch transition.

## Consequences

- Stale writers cannot overwrite a newer epoch even when they present the
  current resource version.
- Memory and PostgreSQL adapters share the same domain rule and rejection
  behavior.
- PostgreSQL performs the guard atomically without a separate transaction or
  advisory lock.
- Existing optimistic concurrency behavior is unchanged for ordinary version
  conflicts.
- No schema migration, protocol field, or dependency is required.

## Rollback

Remove the shared epoch guard and restore repository `Update` methods to
version-only checks. The removed guard would reopen the ADR-006 stale-writer
gap.
