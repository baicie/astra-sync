package io.astrasync.engine.network;

import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.WorkerResult;
import io.astrasync.protocol.worker.ErrorCode;
import io.astrasync.protocol.worker.WorkerResponse;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.time.Duration;
import java.util.Objects;

/** Synchronous client for one framed Worker endpoint. */
public final class WorkerClient {
    private final String host;
    private final int port;
    private final int timeoutMillis;

    public WorkerClient(String host, int port, Duration timeout) {
        this.host = requireText(host, "host");
        if (port <= 0 || port > 65_535) {
            throw new IllegalArgumentException("port must be between 1 and 65535");
        }
        Objects.requireNonNull(timeout, "timeout must not be null");
        long timeoutValue = timeout.toMillis();
        if (timeoutValue <= 0 || timeoutValue > Integer.MAX_VALUE) {
            throw new IllegalArgumentException("timeout must be positive and fit in an integer number of milliseconds");
        }
        this.port = port;
        this.timeoutMillis = (int) timeoutValue;
    }

    public WorkerResult execute(String workerId, BatchTask task) {
        Objects.requireNonNull(task, "task must not be null");
        WorkerResponse response =
                exchange(WorkerProtocolMapper.executeRequest(requireText(workerId, "workerId"), task));
        if (response.hasError()) {
            throw error(response);
        }
        if (!response.hasTaskResult()) {
            throw new NetworkWorkerException("remote Worker returned no task result");
        }
        return WorkerProtocolMapper.toWorkerResult(response.getTaskResult(), workerId, task.taskId());
    }

    String host() {
        return host;
    }

    int port() {
        return port;
    }

    Duration timeout() {
        return Duration.ofMillis(timeoutMillis);
    }

    public boolean cancel(String workerId, String taskId, String reason) {
        String expectedWorkerId = requireText(workerId, "workerId");
        String expectedTaskId = requireText(taskId, "taskId");
        WorkerResponse response = exchange(WorkerProtocolMapper.cancelRequest(
                expectedWorkerId, expectedTaskId, reason == null ? "cancelled by client" : reason));
        if (response.hasError()) {
            throw error(response);
        }
        if (!response.hasCancelResponse()
                || !expectedTaskId.equals(response.getCancelResponse().getTaskId())) {
            throw new NetworkWorkerException("remote Worker returned an invalid cancel response");
        }
        return response.getCancelResponse().getCancelled();
    }

    private WorkerResponse exchange(io.astrasync.protocol.worker.WorkerRequest request) {
        try (Socket socket = new Socket()) {
            socket.connect(new InetSocketAddress(host, port), timeoutMillis);
            socket.setSoTimeout(timeoutMillis);
            WorkerProtocolCodec.writeRequest(socket.getOutputStream(), request);
            WorkerResponse response = WorkerProtocolCodec.readResponse(socket.getInputStream());
            if (response.getProtocolVersion() != WorkerProtocol.CURRENT_VERSION) {
                throw new NetworkWorkerException(
                        "unsupported Worker protocol version: " + response.getProtocolVersion());
            }
            return response;
        } catch (NetworkWorkerException exception) {
            throw exception;
        } catch (IOException exception) {
            throw new NetworkWorkerException("failed to communicate with Worker " + host + ':' + port, exception);
        }
    }

    private static NetworkWorkerException error(WorkerResponse response) {
        ErrorCode code = response.getError().getCode();
        return new NetworkWorkerException("remote Worker rejected task"
                + (code == ErrorCode.RESOURCE_EXHAUSTED ? " due to backpressure" : "")
                + ": "
                + response.getError().getMessage());
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
