# Slice 24: Operational Runbook Templates

## Summary

Slice 24 closes the Phase 7 entry criterion that ADR-044 §"Phase 7
entry criteria" §2 recorded:

> Operational runbook templates. The Slice 18 implementation plan
> tracked IdP registration, key rotation, session revocation, audit
> retention, backup, and rollback runbooks as deployment artefacts.
> Phase 7 supplies a template under `docs/runbooks/` that an operator
> populates per environment. The template is the only
> repository-side contribution; the populated versions stay out of
> source control.

The slice adds six templates under `docs/runbooks/`, an index that
links them to the Phase 6 artefacts the templates cite, and a CI guard
that rejects populated runbooks before they land. The slice does not
ship populated runbooks; the populated versions live in each
operator's deployment-side store.

## Boundary

This slice:

- Adds `docs/runbooks/README.md` and six runbook templates under
  `docs/runbooks/`. The templates are parameterised by
  `<placeholder>` values.
- Adds a CI check that rejects any Markdown file in `docs/runbooks/`
  that lacks a placeholder or that contains a known production
  hostname pattern.
- Records the boundary between template and populated runbook in
  ADR-046 so the operator's deployment-side ownership stays clear.

This slice does not:

- Ship populated runbooks for any environment.
- Add a runbook generator, a deployment-side wiki, or a ticketing
  integration.
- Change the production gates recorded by ADR-043, ADR-044, or
  ADR-045.
- Add Helm templates or `make` targets for the runbooks. The
  templates are documentation; the deployment surface is owned by the
  operator.

## Records

- [Slice 24 Design](design.md)
- [Slice 24 Implementation Plan](implementation-plan.md)
- [Slice 24 Verification](verification.md)
- [ADR-046: Operational Runbook Templates](../../adr/adr-046-operational-runbook-templates.md)
- [ADR-044: Phase 6 Closeout and Phase 7 Entry Criteria](../../adr/adr-044-phase6-closeout-and-phase7-entry-criteria.md)
- [Slice 18 admin runbook](../../phase6/18-auth-rbac-audit/admin-runbook.md)