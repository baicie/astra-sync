# Phase 12 Slice 29: Implementation Plan

## Dependencies

- Phase 9 two-region Docker Compose acceptance framework
- Phase 10 multi-region observability integration
- Phase 11 disaster recovery drill

## Tasks

- [x] Export Java and multi-region change scopes from the workflow filter.
- [x] Select the acceptance job for multi-region source and deployment input
  changes.
- [x] Keep the Makefile acceptance target as the CI entry point.
- [x] Collect and upload Compose logs on every job outcome.
- [x] Run an unconditional Compose cleanup step after the log upload.
- [x] Pin existing third-party actions to immutable commit SHAs.
- [x] Add workflow contract tests and Phase 12 records.

## Implementation Boundary

The change is limited to `.github/workflows/ci.yml`, a Python workflow
contract test, and Phase 12 documentation. It adds no runtime code, generated
file, dependency, protobuf field, migration, or deployment configuration.

## Rollback

Reverting the Phase 12 change restores the prior workflow and removes only CI
selection, diagnostics, cleanup, and action pinning behavior. It requires no
runtime or data rollback.
