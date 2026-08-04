package io.astrasync.engine.network;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskException;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.EpochFence;
import io.astrasync.engine.runtime.WorkerResult;
import io.astrasync.protocol.worker.ErrorCode;
import io.astrasync.protocol.worker.ExecuteCheckpointTaskRequest;
import io.astrasync.protocol.worker.ExecuteTaskRequest;
import io.astrasync.protocol.worker.SplitDescriptor;
import io.astrasync.protocol.worker.SyncMetrics;
import io.astrasync.protocol.worker.TaskResult;
import io.astrasync.protocol.worker.WorkerRequest;
import io.astrasync.protocol.worker.WorkerResponse;
import java.util.Objects;

final class WorkerProtocolMapper {
    private WorkerProtocolMapper() {}

    static WorkerRequest executeRequest(String workerId, BatchTask task) {
        Objects.requireNonNull(task, "task must not be null");
        SourceSplit split = task.split();
        ExecuteTaskRequest execute = ExecuteTaskRequest.newBuilder()
                .setWorkerId(workerId)
                .setTaskId(task.taskId())
                .setSplit(toDescriptor(split))
                .setMaxBatchRecords(task.maxBatchRecords())
                .setMaxInFlightBatches(task.maxInFlightBatches())
                .build();
        return WorkerRequest.newBuilder()
                .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                .setExecuteTask(execute)
                .build();
    }

    static WorkerRequest cancelRequest(String workerId, String taskId, String reason) {
        return WorkerRequest.newBuilder()
                .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                .setCancelTask(io.astrasync.protocol.worker.CancelTaskRequest.newBuilder()
                        .setWorkerId(workerId)
                        .setTaskId(taskId)
                        .setReason(reason)
                        .build())
                .build();
    }

    static WorkerRequest checkpointRequest(
            String workerId, String splitFingerprint, CheckpointExecutionContext context, BatchTask task) {
        Objects.requireNonNull(context, "context must not be null");
        Objects.requireNonNull(task, "task must not be null");
        ExecuteCheckpointTaskRequest execute = ExecuteCheckpointTaskRequest.newBuilder()
                .setWorkerId(workerId)
                .setJobId(context.jobId())
                .setExecutionEpoch(context.executionEpoch())
                .setTaskId(task.taskId())
                .setSplit(toDescriptor(task.split()))
                .setMaxBatchRecords(task.maxBatchRecords())
                .setMaxInFlightBatches(task.maxInFlightBatches())
                .setCheckpointSequence(context.checkpointSequence())
                .putAllSourcePosition(context.sourcePosition().offsets())
                .setSplitFingerprint(requireText(splitFingerprint, "splitFingerprint"))
                .build();
        return WorkerRequest.newBuilder()
                .setProtocolVersion(WorkerProtocol.CHECKPOINT_VERSION)
                .setExecuteCheckpointTask(execute)
                .build();
    }

    static WorkerRequest checkpointAck(String workerId, CheckpointProgress progress, String checkpointFingerprint) {
        return WorkerRequest.newBuilder()
                .setProtocolVersion(WorkerProtocol.CHECKPOINT_VERSION)
                .setCheckpointAck(io.astrasync.protocol.worker.CheckpointAckRequest.newBuilder()
                        .setWorkerId(workerId)
                        .setJobId(progress.jobId())
                        .setExecutionEpoch(progress.executionEpoch())
                        .setTaskId(progress.taskId())
                        .setCheckpointSequence(progress.checkpointSequence())
                        .setCheckpointFingerprint(requireText(checkpointFingerprint, "checkpointFingerprint"))
                        .build())
                .build();
    }

    static CheckpointProgress toCheckpointProgress(io.astrasync.protocol.worker.BatchCommittedResponse response) {
        return new CheckpointProgress(
                response.getJobId(),
                response.getExecutionEpoch(),
                response.getTaskId(),
                response.getCheckpointSequence(),
                new SplitPosition(response.getSourcePositionMap()),
                response.getSinkCommitToken(),
                response.getBatchDigest());
    }

    static CheckpointExecutionContext checkpointContext(ExecuteCheckpointTaskRequest request, EpochFence epochFence) {
        return new CheckpointExecutionContext(
                request.getJobId(),
                request.getExecutionEpoch(),
                request.getTaskId(),
                request.getSplitFingerprint(),
                request.getCheckpointSequence(),
                new SplitPosition(request.getSourcePositionMap()),
                epochFence);
    }

    static WorkerResponse batchCommitted(CheckpointProgress progress, String workerId) {
        return WorkerResponse.newBuilder()
                .setProtocolVersion(WorkerProtocol.CHECKPOINT_VERSION)
                .setBatchCommitted(io.astrasync.protocol.worker.BatchCommittedResponse.newBuilder()
                        .setWorkerId(workerId)
                        .setJobId(progress.jobId())
                        .setExecutionEpoch(progress.executionEpoch())
                        .setTaskId(progress.taskId())
                        .setCheckpointSequence(progress.checkpointSequence())
                        .putAllSourcePosition(progress.sourcePosition().offsets())
                        .setSinkCommitToken(progress.sinkCommitToken())
                        .setBatchDigest(progress.batchDigest())
                        .build())
                .build();
    }

    static SourceSplit toSplit(ExecuteTaskRequest request) {
        SplitDescriptor descriptor = request.getSplit();
        return new SourceSplit(
                descriptor.getSplitId(),
                descriptor.getSourceId(),
                new SplitPosition(descriptor.getStartOffsetsMap()),
                new SplitPosition(descriptor.getEndOffsetsMap()));
    }

    static SourceSplit toSplit(ExecuteCheckpointTaskRequest request) {
        SplitDescriptor descriptor = request.getSplit();
        return new SourceSplit(
                descriptor.getSplitId(),
                descriptor.getSourceId(),
                new SplitPosition(descriptor.getStartOffsetsMap()),
                new SplitPosition(descriptor.getEndOffsetsMap()));
    }

    static WorkerResponse success(WorkerResult result) {
        return WorkerResponse.newBuilder()
                .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                .setTaskResult(TaskResult.newBuilder()
                        .setWorkerId(result.workerId())
                        .setTaskId(result.taskId())
                        .setSuccess(true)
                        .setMetrics(toMetrics(result.metrics()))
                        .build())
                .build();
    }

    static WorkerResponse taskFailure(String workerId, String taskId, Throwable failure, SyncResult metrics) {
        return WorkerResponse.newBuilder()
                .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                .setTaskResult(TaskResult.newBuilder()
                        .setWorkerId(workerId)
                        .setTaskId(taskId)
                        .setSuccess(false)
                        .setMetrics(toMetrics(metrics))
                        .setErrorType(failure.getClass().getName())
                        .setErrorMessage(message(failure))
                        .build())
                .build();
    }

    static WorkerResponse checkpointSuccess(WorkerResult result) {
        return WorkerResponse.newBuilder()
                .setProtocolVersion(WorkerProtocol.CHECKPOINT_VERSION)
                .setTaskResult(TaskResult.newBuilder()
                        .setWorkerId(result.workerId())
                        .setTaskId(result.taskId())
                        .setSuccess(true)
                        .setMetrics(toMetrics(result.metrics()))
                        .build())
                .build();
    }

    static WorkerResponse checkpointTaskFailure(String workerId, String taskId, Throwable failure, SyncResult metrics) {
        return WorkerResponse.newBuilder()
                .setProtocolVersion(WorkerProtocol.CHECKPOINT_VERSION)
                .setTaskResult(TaskResult.newBuilder()
                        .setWorkerId(workerId)
                        .setTaskId(taskId)
                        .setSuccess(false)
                        .setMetrics(toMetrics(metrics))
                        .setErrorType(failure.getClass().getName())
                        .setErrorMessage(message(failure))
                        .build())
                .build();
    }

    static WorkerResponse error(ErrorCode code, String taskId, String message) {
        return WorkerResponse.newBuilder()
                .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                .setError(io.astrasync.protocol.worker.ErrorResponse.newBuilder()
                        .setCode(code)
                        .setTaskId(taskId == null ? "" : taskId)
                        .setMessage(message)
                        .build())
                .build();
    }

    static WorkerResponse checkpointError(ErrorCode code, String taskId, String message) {
        return error(code, taskId, message).toBuilder()
                .setProtocolVersion(WorkerProtocol.CHECKPOINT_VERSION)
                .build();
    }

    static WorkerResponse cancelled(String taskId, boolean cancelled, String message) {
        return WorkerResponse.newBuilder()
                .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                .setCancelResponse(io.astrasync.protocol.worker.CancelResponse.newBuilder()
                        .setTaskId(taskId)
                        .setCancelled(cancelled)
                        .setMessage(message)
                        .build())
                .build();
    }

    static WorkerResult toWorkerResult(TaskResult result, String expectedWorkerId, String expectedTaskId) {
        if (!expectedWorkerId.equals(result.getWorkerId()) || !expectedTaskId.equals(result.getTaskId())) {
            throw new NetworkWorkerException("remote Worker returned an unexpected task identity");
        }
        SyncResult metrics = toMetrics(result.getMetrics());
        if (result.getSuccess()) {
            return new WorkerResult(result.getWorkerId(), result.getTaskId(), metrics);
        }
        NetworkWorkerException cause = new NetworkWorkerException("remote task failed: "
                + (result.getErrorMessage().isBlank() ? result.getErrorType() : result.getErrorMessage()));
        throw new BatchTaskException(expectedWorkerId, expectedTaskId, cause, metrics);
    }

    static SyncResult partialResult(Throwable failure) {
        return failure instanceof BatchTaskException taskException ? taskException.partialResult() : SyncResult.empty();
    }

    private static SplitDescriptor toDescriptor(SourceSplit split) {
        return SplitDescriptor.newBuilder()
                .setSplitId(split.splitId())
                .setSourceId(split.sourceId())
                .putAllStartOffsets(split.start().offsets())
                .putAllEndOffsets(split.end().offsets())
                .build();
    }

    private static SyncMetrics toMetrics(SyncResult metrics) {
        return SyncMetrics.newBuilder()
                .setReadCount(metrics.readCount())
                .setWrittenCount(metrics.writtenCount())
                .setBatchCount(metrics.batchCount())
                .setMaxObservedBatchSize(metrics.maxObservedBatchSize())
                .setElapsedNanos(metrics.elapsedNanos())
                .build();
    }

    private static SyncResult toMetrics(SyncMetrics metrics) {
        return new SyncResult(
                metrics.getReadCount(),
                metrics.getWrittenCount(),
                metrics.getBatchCount(),
                metrics.getMaxObservedBatchSize(),
                metrics.getElapsedNanos());
    }

    private static String message(Throwable failure) {
        return failure.getMessage() == null ? failure.getClass().getName() : failure.getMessage();
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
