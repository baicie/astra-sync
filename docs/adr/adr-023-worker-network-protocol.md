# ADR-023: Versioned Worker Protocol and Bounded Remote Admission

## Status

Accepted

## Context

Phase 1 Slice 01 and Slice 02 execute tasks entirely inside one JVM. The Coordinator/Worker
boundary is already explicit, but there is no wire contract for assigning a split to a Worker or
returning task-local metrics. Adding an unbounded socket reader or serializing connector resources
would make network failures and resource ownership ambiguous.

The first network slice needs a small protocol that can be tested without service discovery or
external infrastructure. It also needs an explicit backpressure boundary so a slow or saturated
Worker cannot accumulate unlimited remote task state.

## Decision

1. Add a standalone protobuf Worker protocol at version `1`. A request contains one execute or
   cancel operation, the target Worker ID, split descriptor, and task limits. A response contains a
   task result, cancellation result, or typed protocol/error response.
2. Transport one length-prefixed protobuf frame per TCP connection. The frame has a fixed maximum
   size and the protocol version is validated on both sides before dispatch.
3. A `WorkerServer` owns a bounded task executor and a bounded connection executor. A full task
   executor returns `RESOURCE_EXHAUSTED`; it never expands its queue or silently drops a request.
4. A `RemoteBatchWorker` uses a semaphore to bound client-side in-flight tasks. It sends only the
   split descriptor and execution limits. The server uses a local `BatchTaskFactory` to materialize
   connector resources, preserving resource ownership on the Worker.
5. Cancellation is explicit. The server tracks active task futures and calls `Future.cancel(true)`
   for a matching cancel request. No retry or replay is performed after a transport or task failure.

## Consequences

The Coordinator can use the existing `BatchWorker` contract with a remote adapter, and network
admission has measurable bounded behavior. The protocol is transport-replaceable because its payload
does not contain Java connector objects. This slice does not provide network RowBatch streaming,
TLS/mTLS, authentication, service discovery, durable Worker registration, checkpointing, retry,
resume, or exactly-once delivery. RowBatch backpressure remains local to the Worker until a later
data-plane exchange slice.
