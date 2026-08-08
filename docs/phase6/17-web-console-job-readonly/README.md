# Phase 6 Slice 17: Web Console and Read-only Job Operations

Slice 17 adds the first browser workflow for operators: inspect a namespace-scoped list of Jobs,
open one Job, and review its execution status and failure/checkpoint information.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-035: Namespace-scoped Read-only Job Console](../../adr/adr-035-namespace-scoped-read-only-job-console.md)

## Run locally

Start the control-plane gRPC API, then run the Console with:

```bash
CONSOLE_NAMESPACE=default ASTRASYNC_API_GRPC_ENDPOINT=127.0.0.1:50051 go run ./cmd/console
```

Open `http://127.0.0.1:8090`. The Console serves the UI and proxies only read operations under
`/api/jobs`.
