# Phase 1 Slice 03: Network Worker Protocol

Slice 03 connects the existing Coordinator/Worker contract across a real TCP boundary with a small
versioned protobuf protocol.

## Delivered

- Standalone `astrasync-protocol-worker` protobuf module.
- Length-prefixed, maximum-sized Worker frames with protocol version validation.
- `WorkerServer` with server-side task materialization, bounded task/connection executors, metrics,
  structured errors, and cancellation.
- `WorkerClient` and `RemoteBatchWorker` with bounded client-side in-flight tasks.
- Loopback tests for remote execution, queue rejection, cancellation interruption, and malformed frames.

## Flow

1. The Coordinator assigns a `BatchTask` to a `RemoteBatchWorker`.
2. The client sends the split descriptor and limits, never Java connector resources.
3. The server validates the version, Worker ID, split identity, and limits.
4. The server-side `BatchTaskFactory` creates fresh source/sink resources and the local `BatchWorker`
   executes the task.
5. The server returns task metrics or a typed failure. A cancel request interrupts the active future.

The backpressure guarantee is task admission: both sides have bounded in-flight capacity. This slice
does not move `RowBatch` values between processes; the existing bounded Source/Sink exchange remains
inside the Worker.
