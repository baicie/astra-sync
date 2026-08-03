# Slice 03 Design

## Wire Contract

The `worker.proto` contract contains `WorkerRequest` and `WorkerResponse` envelopes. Requests carry
one `ExecuteTaskRequest` or `CancelTaskRequest`; execute requests include a `SplitDescriptor`,
`maxBatchRecords`, and `maxInFlightBatches`. Results carry `SyncMetrics`, while protocol capacity,
identity, and version failures use `ErrorCode`.

The socket framing is a four-byte big-endian payload length followed by serialized protobuf bytes.
Frames must be positive and no larger than 8 MiB. One request and one response use one connection,
which keeps the MVP lifecycle explicit and makes disconnects observable as transport failures.

## Admission and Cancellation

`WorkerServer` uses a fixed-size task executor with either a bounded queue or direct handoff. Its
connection executor is also bounded. `RemoteBatchWorker` uses a semaphore before opening a socket.
These limits create backpressure at task admission instead of allowing unbounded remote work.

Active server futures are keyed by task ID. A matching cancel request calls `cancel(true)`, which
lets existing cooperative Worker implementations release their Source/Sink resources through their
normal interruption and failure paths.

## Ownership Boundary

The client transmits only immutable split metadata and numeric limits. Connector instances remain
server-owned and are created by `BatchTaskFactory`, so JDBC connections, files, and sink resources
are never serialized or shared across the network.
