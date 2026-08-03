package io.astrasync.engine.network;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskException;
import io.astrasync.engine.runtime.WorkerResult;
import io.astrasync.protocol.worker.ErrorCode;
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

    static SourceSplit toSplit(ExecuteTaskRequest request) {
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
}
