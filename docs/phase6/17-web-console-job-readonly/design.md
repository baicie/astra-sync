# Phase 6 Slice 17 Design: Web Console and Read-only Job Operations

## Goals

1. Provide a usable same-origin browser view for Jobs in one configured namespace.
2. Reuse the existing protobuf `JobService` contract instead of introducing a second Job model.
3. Show identity, lifecycle state, desired state, execution timestamps, restart count, checkpoint,
   and failure information in list and detail views.
4. Make the pre-RBAC security boundary explicit and testable.

## Non-goals

- User authentication, token validation, tenant discovery, or a general RBAC engine.
- Create, update, delete, start, or stop controls.
- Job logs, metrics, worker topology, live streaming, or audit history.
- A browser-selectable namespace or a Console-owned persistence layer.

## Topology

```text
browser -> Console HTTP server -> JobService gRPC client -> control-plane API server -> repository
              |                         |
              +-- embedded static UI   +-- fixed CONSOLE_NAMESPACE
```

The Console process owns the HTTP presentation boundary. Its server-side adapter supplies the
configured namespace on every Job request. Browser query parameters can select pagination only;
they cannot override the namespace. `X-Astra-Namespace` is returned on Console API responses to
make the active scope visible to operators and diagnostics.

## HTTP contract

| Method | Path | Behavior |
|---|---|---|
| GET | `/health` | Process health, without a control-plane dependency |
| GET | `/ready` | Bounded read against the configured namespace |
| GET | `/api/jobs?page=&page_size=` | Namespace-scoped paginated Job list |
| GET | `/api/jobs/{name}` | Namespace-scoped Job detail |
| GET | `/api/jobs/{name}/status` | Namespace-scoped current Job status |

The API emits protobuf JSON using the canonical field names and maps gRPC `NotFound`,
`InvalidArgument`, `Unavailable`, and other status codes to HTTP responses. No write route is
registered, so the browser cannot accidentally reach lifecycle mutations through the Console.

## UI workflow

The initial page loads the first Job page, shows status badges and source/sink connectors in a
dense table, and selects a Job to open a detail panel. Refresh reloads the list and detail data;
the selected Job status can also be refreshed independently. Empty, loading, unavailable, and
failed-request states are rendered explicitly.

## Scope and future authorization

`CONSOLE_NAMESPACE` defaults to `default` for local development but must be set deliberately in a
production deployment. The Console is not an authentication or authorization implementation and
must run behind an authenticated edge in this slice. Slice 18 will define identity propagation,
tenant authorization, RBAC permissions, and audit requirements before the namespace can become a
user-controlled selection.
