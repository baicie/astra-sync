# ADR-099: Coordinator Trusted Tenant Binding

## Status

Accepted

## Context

ADR-098 added `tenant_id` and `job_id` fields to the Worker protocol and
applied them to Java data-plane metrics. The operational Coordinator had no
trusted tenant source, so it continued to send `_unknown`.

Using the user-supplied JobSpec as the source would not be a trusted binding:
the job document describes desired data movement and is not an authenticated
tenant assertion.

## Decision

Bind tenant identity at the Coordinator process configuration boundary:

1. Read optional `ASTRASYNC_COORDINATOR_TENANT_ID` from the Coordinator
   environment.
2. Treat an absent or blank value as `_unknown`.
3. Require an explicit value to be a canonical lowercase UUID.
4. Reject invalid explicit values during Coordinator startup.
5. Pass the configured tenant to `RemoteTaskFactory`, which sends it through
   the ADR-098 Worker identity fields.
6. Leave `job_id` sourced from the compiled JobSpec identity and keep the
   existing metric label normalization.

The environment value is a process-owner assertion. It does not grant
authorization and does not replace control-plane tenant validation.

## Consequences

- Coordinator runs can emit Worker metrics with a real `tenant_id` when the
  process owner supplies one.
- Legacy/local runs without the environment variable retain the bounded
  `_unknown` behavior.
- Misconfigured tenant values fail closed at startup instead of producing
  misleading metric labels.
- No protobuf, JobSpec, Worker execution, or metric-family change is needed.
- The Helm coordinator Job template does not inject the variable in this
  phase; chart wiring is a separate deployment-owned change.

## Rollback

Remove the environment binding and pass `_unknown` from the Coordinator. The
Worker protocol fields and metric normalization remain otherwise unchanged.
