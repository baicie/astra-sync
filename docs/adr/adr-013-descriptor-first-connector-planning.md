# ADR-013: Descriptor-first Connector Planning

## Status

Accepted

## Date

2026-08-02

## Context

Capability negotiation is useful only if it happens before a connector opens files, database
connections, transactions, or background threads. A compiler that calls connector factories to
discover capabilities can already cause external effects before it rejects exactly-once, a wrong
role, or an invalid topology.

The Phase 0 registry is in-process, but the same boundary must remain valid when connectors are
isolated or remotely cataloged later.

## Decision

Connector planning is descriptor first:

1. Every registered factory exposes an immutable `ConnectorDescriptor` containing name, version,
   roles, and static capabilities.
2. Registry lookup and compilation inspect descriptors only.
3. Duplicate connector names and descriptor role/capability inconsistencies are registration
   errors.
4. Compilation validates connector existence, role, bounded batch capability, transform support,
   and delivery guarantee in a fixed order.
5. Compilation returns an immutable value-only plan containing selected descriptor versions and
   normalized configuration.
6. Compilation never calls Source/Sink factory methods or connector lifecycle methods.
7. Factory methods run only after successful compilation and must construct resource-free objects;
   external resource ownership begins at `open`.

Live connectivity checks are a separate preflight/runtime action and cannot change the compiled
delivery guarantee silently.

## Consequences

### Positive

- Invalid or unsupported jobs have no connector resource side effects.
- Equivalent specifications and descriptors produce deterministic plans.
- Capability negotiation is cheap and unit-testable.
- Registry metadata can later move out of process without changing the compiler contract.

### Negative

- Descriptor claims can become stale or inaccurate and require connector compatibility tests.
- Some failures, such as credentials or remote schema availability, remain materialization-time
  errors.
- Factory authors must separate object construction from resource acquisition.

## Alternatives Considered

### Instantiate Connectors During Compilation

Rejected because constructors may acquire resources and make rejected jobs externally observable.

### Let Connectors Downgrade Guarantees at Open

Rejected because the submitted and compiled plan would no longer describe actual semantics.

### Probe External Systems for Every Capability

Deferred. Live validation is useful but cannot replace static roles/runtime capability checks and
must remain side-effect controlled.

## Related Decisions

- ADR-003: Source Enumerator/Split/Reader Model
- ADR-004: Sink Writer/Committer Model
- ADR-009: Exactly-once via Capability Negotiation
- ADR-011: Bounded Pull-based Single-node Runtime
- ADR-012: Strict Versioned JobSpec Boundary
