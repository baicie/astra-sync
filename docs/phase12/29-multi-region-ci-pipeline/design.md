# Phase 12 Slice 29: Multi-Region CI Pipeline

## Context

The repository already had a local `make test-integration-multi-region`
target and a Docker Compose acceptance suite. The CI job did not select that
suite when only `tests/integration` or its runner changed, and the workflow
did not provide a separate final cleanup step if the runner failed before the
Python wrapper could execute. Action references also used mutable release
tags.

## Decision

Keep the existing Makefile target as the single acceptance entry point and
extend the `changes` job with a `multi_region` output. The output is selected
by changes under the integration suite, its runner, the Makefile, protocol
inputs, or Docker build inputs. The acceptance job also has unconditional
steps to collect the wrapper log, upload the log directory, and remove
Compose resources.

Pin every third-party action in the workflow to the commit resolved for its
current release. Retain a release comment beside each SHA so dependency
updates remain reviewable and compatible with Dependabot's GitHub Actions
configuration.

## Alternatives Considered

- Running the integration suite on every pull request would increase CI cost
  for unrelated documentation changes without improving coverage.
- Calling `go test` directly in the workflow would duplicate the Makefile
  contract and could drift from local verification.
- A cloud-managed test environment would exceed this phase and change the
  deployment boundary.

## Consequences

The acceptance job now runs for relevant integration and runtime changes and
leaves diagnostics available on failure. The job still depends on a Linux
runner with Docker, and the test remains bounded by the workflow timeout.
