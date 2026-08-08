# Phase 6 Slice 17 Verification

## Required Local Checks

| Check | Result |
|---|---|
| Console focused Go tests | PASS: `go test ./...` |
| `go vet ./...` in `console` | PASS |
| Control-plane API Server tests | PASS: `go test ./...` |
| `mvn -B -ntp verify -DskipITs` | PASS: 31-module reactor |
| `mvn -B -ntp spotless:check` | PASS |
| `git diff --check` | PASS |

## Acceptance Evidence

- The Console serves a browser page with Job list and detail/status views.
- Every Job read is constrained to the configured namespace; browser input cannot change it.
- The Console exposes no Job mutation endpoint and delegates Job state to the control plane.
- Health remains process-local while readiness verifies the configured control-plane scope.
- The UI renders loading, empty, error, and normal data states.

## Pull-request Gate

PENDING: record the PR number, required CI checks, and squash-merge commit here after delivery.
