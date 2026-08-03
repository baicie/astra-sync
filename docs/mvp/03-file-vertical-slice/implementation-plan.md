# Slice 03 Implementation Plan

## Preconditions

- [x] Start from verified slice 02 commit `0776547`.
- [x] Re-read the supplied architecture, Phase 0 plan, and ADR-002/009/011/012/013.
- [x] Inventory the empty file connector, placeholder Engine main, and current Maven packaging.
- [x] Fix local materialization, CLI, CSV, malformed-input, and output-publication semantics.
- [x] Add ADR-014 and ADR-015 before changing packaging or implementing file I/O.

## Steps

### 1. Establish Local Execution and Packaging Boundaries

- Add a generic Engine local runner and immutable run result.
- Compile the complete JobSpec before factory creation; validate factory output before kernel build.
- Convert the Engine to a library artifact and add the independent CLI module skeleton.
- Gate: probe tests prove compile/create/open order and no concrete Connector dependency in Engine.

### 2. Implement the Strict CSV Source

- Replace speculative file-module dependencies with Connector API and Apache Commons CSV only.
- Validate Source options without I/O and acquire reader/parser/header state only in `open`.
- Decode UTF-8 RFC 4180 rows into bounded insertion-ordered `Row` values.
- Fail on invalid headers, lexical errors, and inconsistent widths with location evidence.
- Gate: Source contract and malformed-fixture tests pass without loading the full file.

### 3. Implement the Create-new CSV Sink

- Validate Sink options without I/O and open only a previously absent target.
- Derive and write one deterministic header, enforce the column set, and flush each batch.
- Preserve empty strings and explicitly configured nulls; reject unsupported value types.
- Gate: existing-target, schema, partial-output, lifecycle, and round-trip tests pass.

### 4. Implement the Runnable CLI Vertical Path

- Add Picocli `run`, help, version, stable exit categories, and concise redacted output.
- Register the built-in CSV factory explicitly and delegate to the local runner.
- Add an example JobSpec and representative CSV fixture.
- Gate: the packaged shaded JAR performs a real file-to-file copy from a temporary directory.

### 5. Verify and Archive

- Run focused Connector/Engine/CLI verification with JaCoCo gates.
- Run full Reactor tests, Spotless, diff, public-dependency, and packaged-JAR checks.
- Record exact commands, counts, coverage, acceptance evidence, limitations, and commit IDs.
- Update Slice 03 and Phase 0 status before creating Slice 04.
- Gate: the worktree is clean and no implementation file or verification artifact is untracked.

## Change Control

Supporting silent row skips, headerless input, dialect customization, output replacement, temporary
publication, typed CSV coercion, ServiceLoader discovery, or stronger delivery semantics requires
a design update and a new or superseding ADR before implementation.
