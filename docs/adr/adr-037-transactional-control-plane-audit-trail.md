# ADR-037: Transactional Control-plane Audit Trail

## Status

Accepted

## Context

Authentication and RBAC answer whether an action is allowed, but operators also need durable
evidence of who attempted or changed control-plane state. Logging alone is insufficient: logs can
be sampled, reformatted, or lost independently from PostgreSQL state. Writing an audit event after
a successful mutation also leaves a crash window in which the state changes without evidence.

Audit payloads can themselves become a security problem if bearer tokens, connector credentials,
or complete JobSpecs are copied into them.

## Decision

Store append-only audit events in PostgreSQL. Every successful Job or authorization mutation writes
its audit event in the same database transaction as the state change. A mutation fails when its
required audit event cannot be persisted. Membership and role administration follow the same rule.

Authentication/session events and denied authorization attempts have no domain mutation with which
to share a transaction; they are written synchronously through a bounded audit API. A denied action
remains denied if audit persistence fails, and the persistence failure is emitted as a security
alert without changing the external denial result. Successful read auditing is enabled for
administration and audit-query APIs and can be policy-controlled for high-volume Job reads.

Audit records contain stable actor, tenant, action, resource, decision, outcome, request/trace ID,
version transition, and timestamp fields. They never contain credentials, tokens, raw connector
options, full JobSpecs, or response bodies. The application role can insert and query within its
retention policy but cannot update audit rows. Administrative purge uses a separate database role
and records a retention summary outside the purged partition.

## Consequences

Mutation repositories need a unit-of-work boundary that includes audit insertion. Audit storage is
part of control-plane write availability and capacity planning. PostgreSQL partitioning and
retention become operational requirements. The design provides stronger evidence than application
logs, while external immutable export or SIEM integration remains a later hardening step.
