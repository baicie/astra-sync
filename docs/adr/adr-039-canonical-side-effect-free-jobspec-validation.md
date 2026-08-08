# ADR-039: Canonical Side-effect-free JobSpec Validation

## Status

Accepted

## Context

The Go control plane validates protobuf shape and basic bounds, while the Java `JobCompiler` owns
connector resolution, role and capability negotiation, transform support, delivery feasibility,
and execution-plan construction. A browser form cannot infer those rules safely, and copying them
into Go would create multiple authorities that drift as connectors and execution profiles evolve.

Waiting until Coordinator startup to discover an invalid connector or unsupported delivery
guarantee gives operators poor feedback and can leave a Job durably accepted but not executable.
Conversely, validation that opens connectors or external systems can leak credentials, create side
effects, and make a control-plane database transaction depend on an unbounded network probe.

## Decision

Add a protobuf-first Job validation boundary whose semantic authority is a Java internal service
backed by the same strict JobSpec conversion, `ConnectorRegistry`, connector artifacts, and
`JobCompiler` used for execution. The Go API owns tenant authorization, structural validation,
request bounds, lifecycle admission, and public response sanitization. Browser checks and
descriptor-generated forms are advisory only.

Expose explicit Create, Update, and Start validation purposes. Create and Update compile the exact
submitted replacement spec. Start compiles the durable stored spec under the server-selected
deployment execution profile. Create, Update, and Start cannot commit when canonical validation is
invalid or unavailable. A prior preflight can be cached only when its tenant, purpose, canonical
spec digest, compiler/connector revision, authorization revision, and bounded lifetime match; it is
never client-authoritative.

Make validation side-effect free. It may read connector descriptors and option schemas but cannot
open sources or sinks, resolve credentials, enumerate data, discover schemas, connect to external
systems, or perform writes. Connectivity tests require a future separately permissioned and audited
operation. Bound request size, option count, issue count, execution time, memory, and concurrency.

Return stable issue codes, severity, field paths, validation ID, spec digest, and compiler revision.
Do not return rejected values, stack traces, raw JobSpecs, or credentials. Extend connector
descriptors with the option metadata needed for deterministic validation and descriptor-driven
forms. Sensitive options require pre-provisioned tenant-scoped `connection_ref` values and are
excluded from public Job projections, logs, audit, and idempotency records.

Keep Coordinator compile and connector-version fencing even after admission validation. Preflight
improves admission and feedback; it does not replace execution-time compatibility checks.

## Consequences

- Console feedback and mutation admission use the runtime's actual connector and capability rules.
- Go and browser code avoid duplicating semantic compiler logic.
- Create, Update, and Start availability now depends on a bounded internal validation service.
- Java/Go JobSpec conversion, error-code parity, connector option descriptors, and release revision
  compatibility become tested cross-language contracts.
- Connector authors must provide deterministic, side-effect-free descriptor validation metadata.
- Connectivity, schema discovery, and credential management remain deliberately separate surfaces.
- Existing Jobs containing raw sensitive options must be migrated before authenticated read/edit
  responses and Console mutation routes can be enabled safely.
