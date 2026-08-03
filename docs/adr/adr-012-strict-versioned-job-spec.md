# ADR-012: Strict Versioned JobSpec Boundary

## Status

Accepted

## Date

2026-08-02

## Context

JobSpec is the durable user-to-compiler contract. Lenient object binding can silently ignore a
misspelled field, coerce a scalar into another type, or reinterpret a document after a software
upgrade. For data movement, that can select the wrong connector behavior or delivery semantics
without making the submitted configuration visibly invalid.

The initial repository sketch is an unversioned Java interface graph and does not define external
syntax, unknown-field handling, defaults, or evolution rules.

## Decision

Phase 0 introduces an exact external contract identified by:

```text
apiVersion: sync.astrasync.io/v1
kind: SyncJob
```

The parser:

1. Parses YAML and JSON through one structured tree model.
2. Rejects duplicate keys, unknown fields, missing required fields, wrong node types, and scalar
   coercion.
3. Reports failures with a stable document path.
4. Accepts only string values in connector/transform option maps.
5. Applies documented deterministic defaults only (`transforms: []` and
   `runtime.maxBatchRecords: 1024`).
6. Produces immutable value objects with defensively copied, deterministically ordered maps.
7. Rejects an unknown `apiVersion` or `kind`; it never guesses or upgrades a document.

Future incompatible syntax uses a new `apiVersion` and an explicit parser/compiler path. A v1
reader may add only fields whose absence has a documented deterministic meaning; it must continue
to reject fields it does not understand.

## Consequences

### Positive

- Misspellings and unsupported features fail before data access.
- The same document has stable meaning across YAML and JSON inputs.
- Compiler tests can assert exact error locations and normalized values.
- Version evolution becomes explicit and reviewable.

### Negative

- Users cannot add arbitrary annotations or connector-native typed values to v1.
- New fields require parser, model, documentation, and compatibility updates together.
- Strict duplicate detection can reject documents accepted by permissive YAML tooling.

## Alternatives Considered

### Lenient Jackson Bean Binding

Rejected because unknown fields and scalar coercion can hide operationally significant mistakes.

### Store Arbitrary Maps and Validate in Connectors

Rejected for the core JobSpec shape because connector code would receive ambiguous, partially
validated control-plane data. Only connector option values remain connector-owned, and v1 limits
them to strings.

### Infer the Version from Present Fields

Rejected because inference makes old documents change meaning as fields are added.

## Related Decisions

- ADR-009: Exactly-once via Capability Negotiation
- ADR-011: Bounded Pull-based Single-node Runtime
- ADR-013: Descriptor-first Connector Planning
