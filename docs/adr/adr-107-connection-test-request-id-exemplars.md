# ADR-107: Connection Test Request ID Exemplars

## Status

Accepted

## Context

The Connection Test Executor emits `connection_test_total` only after the
authoritative `CompleteTest` write succeeds. The metric records bounded
`tenant_id` and `outcome` labels, but it does not carry the durable operation
ID, and the production `/metrics` handler uses the legacy Prometheus text
format.

Every claimed `TestWork` already contains an `OperationID`. The connection
domain validates it as a UUID, and it is the primary key of
`astrasync_connection_tests`. The executor can therefore use the existing
operation identity as the request ID for the completed metric sample without
adding a protocol field or generating another identifier.

## Decision

1. Use `TestWork.Operation.OperationID` as the `request_id` for the
   `connection_test_total` sample produced after durable completion.
2. Attach `request_id` as an exemplar only when the operation ID is a
   canonical lowercase UUID. Missing or non-canonical values still produce
   the ordinary metric sample without an exemplar.
3. Keep `request_id` absent from the normal metric labels. The existing
   `tenant_id` and `outcome` labels do not change.
4. Enable OpenMetrics negotiation on the production Connection Test Executor
   `/metrics` handler because the legacy Prometheus text format cannot
   transmit exemplars.
5. Preserve the existing authoritative-completion rule: a lease-lost or
   failed completion does not create a metric sample or exemplar.

The change is transport- and deployment-neutral. It does not add a metric
family, normal label, protocol field, dependency, Helm value, or CRD change.

## Consequences

- Operators can join a Connection Test metric sample to the
  `astrasync_connection_tests` row with the same `operation_id`, including the
  recorded outcome, phase, result code, and completion timestamp.
- Prometheus text responses remain available when OpenMetrics is not
  requested.
- Malformed operation IDs remain bounded because they are never copied into
  normal labels and are not emitted as exemplars.
- Existing tenant and outcome normalization remains unchanged.

## Rollback

Remove the operation ID argument from the Connection Test Recorder, restore
the production `/metrics` handler's default content negotiation, and remove
`request_id` from the metric call site. Existing samples remain otherwise
unchanged.
