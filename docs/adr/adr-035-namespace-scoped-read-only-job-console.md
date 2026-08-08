# ADR-035: Namespace-scoped Read-only Job Console

## Status

Accepted

## Context

The control plane already exposes a versioned `JobService` over gRPC and has a generated JSON
gateway, but operators do not yet have a browser workflow for inspecting jobs. Authentication,
authorization, and a complete multi-tenant identity model are platform work that has not been
implemented yet. Exposing the full lifecycle API from a first console would make that boundary
ambiguous and would risk allowing a browser to select a namespace outside its intended scope.

## Decision

Add a small Go Console process that serves an embedded static web application and a read-only HTTP
adapter over the existing `JobService` client. The adapter exposes job listing, job details, and
job status only. It uses a namespace fixed at process startup by `CONSOLE_NAMESPACE`; request
parameters cannot change that scope. The Console does not implement authentication or claim to be
an authorization service, so production deployments must place it behind an identity-aware edge
until the platform authentication/RBAC slice is delivered.

The Console owns no job state and does not cache responses. gRPC status codes are translated to
stable HTTP responses, and readiness performs a bounded namespace-scoped list request against the
control plane.

## Consequences

The first operator workflow is small, deployable, and reuses the canonical Job model without
duplicating lifecycle logic. A separate process boundary keeps browser concerns out of the API
server. The fixed namespace is safe for a single-tenant/operator deployment but requires one
Console instance per namespace until authenticated tenant selection and RBAC are implemented.
Mutation controls, audit history, and richer operational telemetry remain follow-up work.
