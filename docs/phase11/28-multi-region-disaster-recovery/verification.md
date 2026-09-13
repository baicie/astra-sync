# Phase 11 Slice 28 Verification

## Verified Scope

The acceptance drill covers the current two-region runtime boundary:

- a checkpoint is acknowledged before the primary fault;
- the primary HTTP and gRPC endpoints become unavailable after its API
  Server is stopped;
- the secondary remains able to return topology and replication status;
- secondary self-promotion is rejected with `FailedPrecondition`;
- recovery without a local checkpoint manifest is rejected with `NotFound`;
- the primary endpoints become ready again after restart.

Unit tests cover domain error mapping, sanitized internal messages, existing
status preservation, and context cancellation preservation.

## Evidence

| Check | Result |
|-------|--------|
| `go test ./internal/replication/...` from `control-plane/api-server` | passed on 2026-09-06 |
| `go test ./multi-region/framework` from `tests/integration` | passed on 2026-09-06 |
| `go test -tags=integration -run '^$' ./multi-region/acceptance` from `tests/integration` | passed on 2026-09-06 |
| `git diff --check` | passed on 2026-09-06 |
| `make check` | passed on 2026-09-06 |
| `make test-go` | passed on 2026-09-06 |
| `make test-scripts` | passed on 2026-09-06 |
| `make test-java` | passed on 2026-09-06 |
| `make test-integration-multi-region` | passed on 2026-09-06; both acceptance tests passed |

## Acceptance

The repository gates and Docker Compose acceptance are green. Phase 11 is
accepted. The drill preserves the fail-closed outcomes described above; a
successful promotion or recovery against this topology would indicate an
invalid test or an unintended runtime boundary change.

## References

- [Phase 11 README](../README.md)
- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Disaster recovery drill template](../../runbooks/multi-region-disaster-recovery-drill-template.md)
