# Phase 12: CI/CD Pipeline Integration for Multi-Region Tests

## Status

**Complete.** Phase 12 connected the two-region Docker Compose acceptance
tests to the repository CI pipeline and recorded the workflow contract on
2026-09-06.

## Goals

1. Run multi-region acceptance tests when their source, framework, Compose,
   script, or related control-plane inputs change.
2. Preserve the Makefile as the CI entry point for the acceptance test.
3. Publish Compose logs when the test fails or the runner cannot start the
   deployment.
4. Remove Compose containers, networks, volumes, and orphaned services after
   every job attempt.
5. Pin every third-party GitHub Action to an immutable commit SHA.

## Non-goals

- Adding a deployment environment or a cloud provider integration.
- Making CI claim a production promotion or recovery.
- Adding new dependencies, protobuf fields, storage backends, or pipeline
  delivery semantics.

## Roadmap

| Slice | Focus | Status |
|-------|-------|--------|
| 29.1 | Multi-region change detection | Complete |
| 29.2 | Acceptance job log and cleanup contract | Complete |
| 29.3 | Immutable action references | Complete |
| 29.4 | Workflow regression tests and verification record | Complete |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Multi-region source and test changes select the acceptance job | Complete |
| Acceptance runs through `make test-integration-multi-region` | Complete |
| Compose logs are collected and uploaded with `always()` | Complete |
| Compose resources are cleaned up with `always()` | Complete |
| Third-party actions use commit SHAs | Complete |
| Existing Java, Go, protocol, security, runbook, and image jobs remain | Complete |

## Records

- [Design](29-multi-region-ci-pipeline/design.md)
- [Implementation plan](29-multi-region-ci-pipeline/implementation-plan.md)
- [Verification](29-multi-region-ci-pipeline/verification.md)
- [Phase 11 README](../phase11/README.md)
- [CI workflow](../../.github/workflows/ci.yml)

## Architectural Boundary

Phase 12 changes CI orchestration only. The acceptance deployment remains the
disposable two-region Compose topology from Phases 9 and 11, and runtime
promotion, recovery, and epoch fencing remain governed by ADR-048 through
ADR-050.
