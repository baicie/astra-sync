# ADR-014: Local Runner and CLI Boundary

## Status

Accepted

## Date

2026-08-03

## Context

The bounded Engine and connector compiler are reusable library boundaries, but the repository's
Engine main class is only a banner and the Engine JAR is shaded as though it were an application.
Making the Engine instantiate the first concrete connector would invert the intended plugin
dependency direction. Putting parsing, planning, materialization, execution, terminal output, and
process exit behavior into one main method would also make later JDBC paths duplicate or bypass
the validated pipeline.

## Decision

Phase 0 separates three responsibilities:

1. The Engine remains a connector-agnostic library and owns `LocalJobRunner`, which accepts an
   explicit `ConnectorRegistry`, compiles the entire JobSpec, pins descriptor versions, creates
   resource-free connector instances, and invokes the bounded kernel.
2. Concrete connector modules depend on `connector-api` and connector-owned client or format
   libraries, never on the Engine.
3. A new CLI application module depends on the Engine and selected built-in connectors. It owns
   JobSpec file reading, explicit registry composition, command parsing, terminal rendering, exit
   codes, and runnable shaded-JAR packaging.

The Engine's placeholder main class and shade configuration are removed. The CLI uses Picocli and
exposes `run <job-spec-path>`, help, and version. Compilation and connector-option validation finish
before either connector `open`; external resource ownership remains inside the kernel lifecycle.

Phase 0 uses explicit built-in registration. ServiceLoader, isolated plugin classloaders, remote
registries, and dynamic installation remain later capabilities.

## Consequences

### Positive

- Core planning and runtime code cannot acquire a dependency on a concrete connector.
- CLI and future embedding APIs share the same compile/materialize/execute sequence.
- Process exit behavior is testable without terminating the test JVM.
- The runnable artifact has one clear owner and no nested shaded-Engine packaging.
- New built-in connectors can be composed without changing the execution loop.

### Negative

- Adding a built-in connector initially requires a CLI composition change.
- The local runner introduces a small materialization API before distributed workers exist.
- Plugin discovery and dependency isolation are deferred.

## Alternatives Considered

### Put the CSV Factory in the Engine

Rejected because the core runtime would depend on a leaf connector and every later connector would
expand that dependency.

### Keep All Logic in the CLI

Rejected because JDBC and embedded callers would duplicate materialization order and could bypass
compile-before-open guarantees.

### Introduce ServiceLoader Now

Deferred because the first vertical slice has one trusted built-in connector and no classloader,
version-conflict, or installation model. Explicit composition is deterministic and testable.

## Related Decisions

- ADR-002: Direct Pipeline Mode as Default
- ADR-009: Exactly-once via Capability Negotiation
- ADR-011: Bounded Pull-based Single-node Runtime
- ADR-012: Strict Versioned JobSpec Boundary
- ADR-013: Descriptor-first Connector Planning
