# Phase 15 Design: Connector Catalog Lifecycle Automation

## Context

The deployment connector inventory (`deployment/catalog/connector-inventory.pb`)
is committed to the repository and must match the connector descriptors
discovered by `ConnectorRegistry.discover()` at the current commit. The CI
gate (`Check deployment connector inventory` in `.github/workflows/ci.yml`)
and the local `Makefile catalog-check` target re-export the catalog and assert
byte-for-byte identity.

Three problems limited the lifecycle:

1. **Hardcoded `--compiler-build 0.1.0-SNAPSHOT`** in both the Makefile and
   the CI step. The CLI default is `AstraSyncCli.VERSION = "AstraSync 0.1.0-SNAPSHOT"`,
   so the override is redundant and breaks the moment the project version
   bumps. The catalog embeds `compiler_build` in a protobuf field; any
   version bump causes a spurious CI failure even when no descriptor changed.

2. **No diagnostics on failure.** `scripts/check-files-identical.py` only
   reports "files differ". The developer must manually decode the binary or
   run a debug build to learn whether the failure is a genuine descriptor
   change or build-version drift.

3. **No standalone inspection.** Inspecting the catalog requires building
   the CLI jar and parsing the binary manually.

## Decisions

### 1. Dynamic compiler-build from git SHA

`Makefile` exposes a `CATALOG_BUILD_VERSION` variable defaulting to
`$(shell git rev-parse --short HEAD)` and uses it in both `catalog-check` and
the new `catalog-export` target. The CI step reads the same value from
`${{ github.sha }}` and passes it to the CLI.

```makefile
CATALOG_BUILD_VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
CATALOG_EXECUTION_PROFILE ?= standard

catalog-export:
	java -jar cli/target/astrasync-cli-*-all.jar \
		catalog-export target/connector-inventory.pb \
		--compiler-build $(CATALOG_BUILD_VERSION) \
		--execution-profile $(CATALOG_EXECUTION_PROFILE)
```

When `git rev-parse` is unavailable (e.g. a container build that does not
preserve `.git/`), the value falls back to `unknown`. The committed catalog
will then also have `compiler_build = unknown`, so the byte-for-byte
identity assertion still holds for container-only release flows.

### 2. Readable catalog-print

A new `cli catalog-print` subcommand decodes a `.pb` file and prints
line-oriented `key=value` lines:

```
header.compiler_build=a1b2c3d
header.compiler_revision=sha256:def456…
header.execution_profile=standard
header.inventory_revision=sha256:abc123…
header.inventory_schema_version=1
header.job_spec_schema_revision=sync.astrasync.io/v1
header.descriptor_count=4
descriptor.csv.artifact_version=…
descriptor.csv.descriptor_revision=…
descriptor.csv.capabilities=SNAPSHOT
…
```

The format is deterministic (descriptors sorted by name; fields sorted
alphabetically within a descriptor). Two identical descriptor sets produce
identical line output regardless of the binary protobuf field order.

### 3. diff-catalog categorisation

`scripts/diff-catalog.py` runs `catalog-print` twice and categorises the
diff into:

- **Build-version drift** — `header.compiler_build` or `header.compiler_revision`
  changed. Expected on every commit. The script exits 0 in this case and prints
  "Verdict: build-version-only drift; descriptor set is identical."

- **Semantic drift** — descriptor added/removed, descriptor field changed, or
  any other `header.*` field changed. The script exits 1 and prints:
  - A bullet list of the categorised drift (one line per change).
  - A unified diff of the line-oriented output.

- **Tool failure** — CLI invocation failed, file missing, etc. The script
  exits 2.

This split mirrors the failure modes an operator experiences in practice:
"expected build version flip" is not a real failure; "descriptor set changed
unexpectedly" is.

### 4. CI integration

The CI step wraps `check-files-identical.py` with an `if` that, on failure,
invokes `diff-catalog.py` to print diagnostics inline. The diagnostics
include the bullet list and unified diff, so the developer sees the
exact fields that changed in the CI log without re-running locally.

### 5. Authoring documentation

`docs/catalog-authoring.md` documents:
- When regeneration is required (descriptor changes, new connector).
- The `make catalog-export && git add` procedure.
- How to read `catalog-check` failure diagnostics.
- Environment variable overrides (`CATALOG_BUILD_VERSION`, etc.).
- Cross-references to ADR-040, ADR-055, and `connector-dev.md`.

## Consequences

### Positive

- `catalog-check` no longer breaks on version bumps.
- CI failures carry actionable diagnostics.
- A standalone inspection tool exists for the catalog.
- The author flow is documented and discoverable.
- Existing deterministic-bytes contract is preserved.

### Negative

- `git rev-parse` requires a git checkout. Container builds without `.git/`
  fall back to `unknown` as the build version, which is acceptable for the
  committed catalog (it will also have `unknown`) but loses provenance
  information in the binary.

## Alternatives Considered

### Remove the `--compiler-build` option entirely

Removing the option would force the CLI to embed its own compile-time
`VERSION` constant, decoupling the catalog from the project's git state.
This is a regression because the catalog is supposed to be regenerated
against each commit, not each CLI jar build.

### Use the Maven project version instead of git SHA

`mvn help:evaluate -Dexpression=project.version` returns the version from
the root `pom.xml`. This couples the catalog to the Maven coordinate,
not the source state. Two commits on the same version would produce
indistinguishable catalogs even though their descriptor sets differ.

### Read the protobuf directly via the Python `google.protobuf` runtime

The Python binding would avoid the JVM dependency but requires the
generated Python bindings to be available. Project CI does not currently
provision them, and the CLI `catalog-print` path is more robust because
it reuses the project's already-generated Java bindings and proto schema
without introducing a new dependency.

### Encode the catalog as JSON instead of protobuf

JSON would be diff-friendly by default but breaks ADR-040's
deployment-authoritative binary catalog contract. The catalog must remain
binary to match the wire format the API server hands to the compiler.
