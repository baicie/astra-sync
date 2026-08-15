# ADR-044: Phase 6 Closeout and Phase 7 Entry Criteria

## Status

Accepted (closes Phase 6; opens Phase 7). Implements the closeout step tracked by
the Slice 18 implementation plan and the Phase 6 acceptance document.

## Context

Phase 6 closed slice by slice through PRs #33, #34, #35, and the new
`phase6/slice22-transport-hardening` branch (PR #36). The Slice 18 implementation
plan §6 ("Transport, Deployment, and Rollout") was the only remaining Phase 6
work in the repository boundary; Slice 22 closed it. The remaining items that
the plan tracks — IdP registration runbooks, key rotation, session revocation,
audit retention, backup, and rollback runbooks — are deployment-owned artefacts
that an operator must own in each environment.

Phase 6 acceptance reads "Phase 6 remains in progress because transport hardening
and production identity rollout are not yet delivered" (after Slice 21) and
"Phase 6 remains in progress because transport hardening and production identity
rollout are not yet delivered" is the last open line. Slice 22 closed the
transport side; the operational runbook side is correctly a deployment
responsibility, not a repository deliverable.

## Decision

### Phase 6 closeout

The repository boundary for Phase 6 is closed once Slice 22 lands. The Phase 6
README and acceptance document move from "in progress" to "Complete". The
remaining rollout work moves to the Phase 7 entry criteria so the boundary
between Phase 6 and Phase 7 is auditable.

The closeout explicitly does NOT claim that production is enabled. Production
remains operator-controlled and gated by `APP_ENV=production`, `AUTH_MODE`,
`TRUSTED_PROXY_CIDRS`, `TLS_CERTIFICATE_FILE`/`TLS_PRIVATE_KEY_FILE`, and the
Console equivalents.

### Phase 7 entry criteria

Phase 7 is admitted by the following items. None is a precondition for the
Phase 6 closeout; each is a precondition for the first Phase 7 PR.

1. **Cross-cluster control-plane mTLS.** ADR-043 left Console → API and
   Scheduler → API as future work. Phase 7 picks one of them as a precondition
   to the first multi-region change, because plain gRPC inside a shared network
   is not a secure baseline.
2. **Operational runbook templates.** The Slice 18 implementation plan tracked
   IdP registration, key rotation, session revocation, audit retention, backup,
   and rollback runbooks as deployment artefacts. Phase 7 supplies a template
   under `docs/runbooks/` that an operator populates per environment. The
   template is the only repository-side contribution; the populated versions
   stay out of source control.
3. **Multi-region standby and failover semantics.** ADR-010 constrained
   AstraSync to one active region per Job. Phase 7 lifts the constraint for
   data-plane jobs whose source and sink are region-pinned, while preserving
   epoch fencing and durable desired state.
4. **Observability consolidation.** Slices 14–16 added bounded Arrow batches,
   adaptive parallelism, spillable exchange, and checkpoint metrics. Phase 7
   unifies the SLF4J and zap logs, the Prometheus metrics, and the audit trail
   into a single operational handbook so that an operator can derive
   per-tenant SLI/SLO from a single dashboard.

### Boundary

Phase 6 closeout does not relax any of the existing production rules. The
`AUTH_MODE`, `OIDC_*`, `TRUSTED_PROXY_CIDRS`, `TLS_*` and `CONSOLE_TLS_*`
checks remain fail-closed. Phase 7 begins from the same starting position
that Slice 22 produced.

## Consequences

- The Phase 6 README updates its status to Complete and removes the "remains
  in progress because transport hardening" line.
- The Phase 6 acceptance document records Slice 22's verification evidence.
- The Phase 7 README and roadmap are introduced in the same PR that moves
  Phase 6 to Complete. Phase 7 has its own README so that the two phases
  remain independently auditable.
- Operators who have not finished their deployment-side IdP, runbook, and
  bootstrap tasks are not blocked by this ADR. The repository boundary is
  closed; the deployment boundary remains owned by each operator.

## Alternatives considered

- **Hold Phase 6 open until the runbooks ship.** Rejected. The runbooks are
  deployment-owned, so holding Phase 6 open would either commit non-portable
  fixtures or block indefinitely. The ADR captures the boundary instead.
- **Merge Phase 6 and Phase 7 into a single "Platform" phase.** Rejected.
  Phase 6 has its own acceptance document and per-slice evidence index that
  reviewers and operators can audit independently. Collapsing them would
  erase the trail.
- **Open Phase 7 with a single theme.** Rejected. Phase 7's first slice
  will pick one of the four entry criteria above. The criteria are
  independently scoped; choosing one now would pre-empt the design review
  the first Phase 7 slice deserves.