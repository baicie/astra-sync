# Phase 6: Platform

Phase 6 turns the control plane into an operator-facing platform. The first slice establishes a
usable Web Console while keeping authentication and mutation permissions explicit follow-up work.

## Roadmap

| Slice | Focus | Status |
|---|---|---|
| Slice 17 | Web Console and namespace-scoped read-only Job operations | Complete |
| Slice 18 | Authentication, tenant identity, RBAC, and audit policy | Planning |
| Slice 19 | Job mutation workflows and operational actions | Planning |

## Records

- [Slice 17 README](17-web-console-job-readonly/README.md)
- [Slice 17 Design](17-web-console-job-readonly/design.md)
- [Slice 17 Implementation Plan](17-web-console-job-readonly/implementation-plan.md)
- [Slice 17 Verification](17-web-console-job-readonly/verification.md)
- [ADR-035: Namespace-scoped Read-only Job Console](../adr/adr-035-namespace-scoped-read-only-job-console.md)

## Boundary

Slice 17 provides inspection only. The Console fixes its namespace at startup and delegates all
job reads to the existing control-plane `JobService`. It does not authenticate users, select a
tenant from browser input, or expose lifecycle mutation methods.
