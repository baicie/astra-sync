package io.astrasync.engine.network;

import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.CheckpointProgressListener;
import io.astrasync.engine.runtime.WorkerResult;
import io.astrasync.protocol.worker.ErrorCode;
import io.astrasync.protocol.worker.WorkerResponse;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.time.Duration;
import java.util.Objects;

/** Bidirectional framed client for checkpoint progress and Coordinator acknowledgements. */
public final class CheckpointWorkerClient {
    private final String host;
    private final int port;
    private final int timeoutMillis;

    public CheckpointWorkerClient(String host, int port, Duration timeout) {
        this.host = requireText(host, "host");
        if (port <= 0 || port > 65_535) {
            throw new IllegalArgumentException("port must be between 1 and 65535");
        }
        this.port = port;
        Objects.requireNonNull(timeout, "timeout must not be null");
        long millis = timeout.toMillis();
        if (millis <= 0 || millis > Integer.MAX_VALUE) {
            throw new IllegalArgumentException("timeout must be positive and fit in milliseconds");
        }
        timeoutMillis = (int) millis;
    }

    public WorkerResult execute(
            String workerId, CheckpointExecutionContext context, BatchTask task, CheckpointProgressListener listener) {
        Objects.requireNonNull(context, "context must not be null");
        Objects.requireNonNull(task, "task must not be null");
        CheckpointProgressListener checkedListener = CheckpointProgressListener.require(listener);
        try (Socket socket = new Socket()) {
            socket.connect(new InetSocketAddress(host, port), timeoutMillis);
            socket.setSoTimeout(timeoutMillis);
            WorkerProtocolCodec.writeRequest(
                    socket.getOutputStream(),
                    WorkerProtocolMapper.checkpointRequest(
                            requireText(workerId, "workerId"), context.splitFingerprint(), context, task));
            long expectedSequence = Math.addExact(context.checkpointSequence(), 1);
            while (true) {
                WorkerResponse response = WorkerProtocolCodec.readResponse(socket.getInputStream());
                if (response.getProtocolVersion() != WorkerProtocol.CHECKPOINT_VERSION) {
                    throw new NetworkWorkerException(
                            "unsupported checkpoint Worker protocol version: " + response.getProtocolVersion());
                }
                if (response.hasBatchCommitted()) {
                    CheckpointProgress progress =
                            WorkerProtocolMapper.toCheckpointProgress(response.getBatchCommitted());
                    if (!workerId.equals(response.getBatchCommitted().getWorkerId())
                            || !context.jobId().equals(progress.jobId())
                            || context.executionEpoch() != progress.executionEpoch()
                            || !task.taskId().equals(progress.taskId())
                            || progress.checkpointSequence() != expectedSequence) {
                        throw new NetworkWorkerException("remote Worker returned unexpected checkpoint identity");
                    }
                    checkedListener.onBatchCommitted(progress);
                    WorkerProtocolCodec.writeRequest(
                            socket.getOutputStream(),
                            WorkerProtocolMapper.checkpointAck(workerId, progress, progress.fingerprint()));
                    expectedSequence = Math.addExact(expectedSequence, 1);
                    continue;
                }
                if (response.hasError()) {
                    throw error(
                            response.getError().getCode(), response.getError().getMessage());
                }
                if (!response.hasTaskResult()) {
                    throw new NetworkWorkerException("remote Worker returned no checkpoint task result");
                }
                return WorkerProtocolMapper.toWorkerResult(response.getTaskResult(), workerId, task.taskId());
            }
        } catch (NetworkWorkerException exception) {
            throw exception;
        } catch (IOException exception) {
            throw new NetworkWorkerException("failed to communicate with Worker " + host + ':' + port, exception);
        }
    }

    private static NetworkWorkerException error(ErrorCode code, String message) {
        return new NetworkWorkerException("remote checkpoint Worker rejected task (" + code + "): " + message);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
