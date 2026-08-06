package io.astrasync.engine.network;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskException;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.CheckpointBatchWorker;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.EpochFence;
import io.astrasync.engine.runtime.EpochFencedException;
import io.astrasync.engine.runtime.WorkerResult;
import io.astrasync.protocol.worker.CheckpointAckRequest;
import io.astrasync.protocol.worker.ErrorCode;
import io.astrasync.protocol.worker.ExecuteCheckpointTaskRequest;
import io.astrasync.protocol.worker.ExecuteTaskRequest;
import io.astrasync.protocol.worker.WorkerRequest;
import io.astrasync.protocol.worker.WorkerResponse;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.SocketException;
import java.util.Objects;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CancellationException;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.FutureTask;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.SynchronousQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

/** A bounded, versioned Worker endpoint for remote task execution and cancellation. */
public final class WorkerServer implements AutoCloseable {
    private final String workerId;
    private final int requestedPort;
    private final BatchTaskFactory taskFactory;
    private final BatchWorker worker;
    private final ThreadPoolExecutor taskExecutor;
    private final ThreadPoolExecutor connectionExecutor;
    private final ConcurrentHashMap<String, FutureTask<WorkerResponse>> activeTasks = new ConcurrentHashMap<>();
    private final EpochFence checkpointEpochFence = new EpochFence();
    private volatile ServerSocket serverSocket;
    private volatile Thread acceptThread;

    public WorkerServer(
            String workerId,
            int port,
            BatchTaskFactory taskFactory,
            BatchWorker worker,
            int maxConcurrentTasks,
            int queueCapacity,
            int maxConnections) {
        this.workerId = requireText(workerId, "workerId");
        if (port < 0 || port > 65_535) {
            throw new IllegalArgumentException("port must be between 0 and 65535");
        }
        this.requestedPort = port;
        this.taskFactory = Objects.requireNonNull(taskFactory, "taskFactory must not be null");
        this.worker = Objects.requireNonNull(worker, "worker must not be null");
        if (maxConcurrentTasks <= 0) {
            throw new IllegalArgumentException("maxConcurrentTasks must be positive");
        }
        if (queueCapacity < 0) {
            throw new IllegalArgumentException("queueCapacity must not be negative");
        }
        if (maxConnections <= 0) {
            throw new IllegalArgumentException("maxConnections must be positive");
        }
        BlockingQueue<Runnable> taskQueue =
                queueCapacity == 0 ? new SynchronousQueue<>() : new ArrayBlockingQueue<>(queueCapacity);
        this.taskExecutor = new ThreadPoolExecutor(
                maxConcurrentTasks,
                maxConcurrentTasks,
                0,
                TimeUnit.MILLISECONDS,
                taskQueue,
                runnable -> daemonThread(runnable, "astrasync-worker-task-"));
        this.connectionExecutor = new ThreadPoolExecutor(
                maxConnections,
                maxConnections,
                0,
                TimeUnit.MILLISECONDS,
                new SynchronousQueue<>(),
                runnable -> daemonThread(runnable, "astrasync-worker-connection-"));
    }

    public synchronized void start() {
        if (serverSocket != null) {
            throw new IllegalStateException("Worker server is already started");
        }
        try {
            ServerSocket opened = new ServerSocket(requestedPort);
            serverSocket = opened;
            Thread thread = new Thread(this::acceptLoop, "astrasync-worker-accept-" + workerId);
            thread.setDaemon(true);
            acceptThread = thread;
            thread.start();
        } catch (IOException exception) {
            throw new NetworkWorkerException("failed to start Worker server", exception);
        }
    }

    public int port() {
        ServerSocket current = serverSocket;
        if (current == null) {
            throw new IllegalStateException("Worker server is not started");
        }
        return current.getLocalPort();
    }

    @Override
    public synchronized void close() {
        ServerSocket current = serverSocket;
        serverSocket = null;
        if (current != null) {
            try {
                current.close();
            } catch (IOException ignored) {
                // The endpoint is being closed; active clients receive their transport failure.
            }
        }
        Thread thread = acceptThread;
        acceptThread = null;
        if (thread != null) {
            thread.interrupt();
        }
        activeTasks.values().forEach(future -> future.cancel(true));
        activeTasks.clear();
        taskExecutor.shutdownNow();
        connectionExecutor.shutdownNow();
    }

    private void acceptLoop() {
        while (serverSocket != null) {
            try {
                Socket socket = serverSocket.accept();
                try {
                    connectionExecutor.execute(() -> handle(socket));
                } catch (RejectedExecutionException exception) {
                    closeQuietly(socket);
                }
            } catch (SocketException exception) {
                if (serverSocket != null) {
                    throw new NetworkWorkerException("Worker server accept loop failed", exception);
                }
            } catch (IOException exception) {
                if (serverSocket != null) {
                    throw new NetworkWorkerException("Worker server accept loop failed", exception);
                }
            }
        }
    }

    private void handle(Socket socket) {
        try (socket) {
            try {
                WorkerRequest request = WorkerProtocolCodec.readRequest(socket.getInputStream());
                if (request.getOperationCase() == WorkerRequest.OperationCase.EXECUTE_CHECKPOINT_TASK) {
                    executeCheckpoint(
                            request.getProtocolVersion(),
                            request.getExecuteCheckpointTask(),
                            socket.getInputStream(),
                            socket.getOutputStream());
                    return;
                }
                WorkerResponse response = dispatch(request);
                WorkerProtocolCodec.writeResponse(socket.getOutputStream(), response);
            } catch (IOException | RuntimeException exception) {
                try {
                    WorkerProtocolCodec.writeResponse(
                            socket.getOutputStream(),
                            WorkerProtocolMapper.error(ErrorCode.INVALID_REQUEST, null, exception.getMessage()));
                } catch (IOException ignored) {
                    // The client disconnected after submitting the request.
                }
            }
        } catch (IOException ignored) {
            // Closing a client socket is part of normal transport failure handling.
        }
    }

    private WorkerResponse dispatch(WorkerRequest request) {
        if (request.getProtocolVersion() != WorkerProtocol.CURRENT_VERSION) {
            return WorkerProtocolMapper.error(
                    ErrorCode.PROTOCOL_VERSION_MISMATCH,
                    null,
                    "unsupported Worker protocol version: " + request.getProtocolVersion());
        }
        return switch (request.getOperationCase()) {
            case EXECUTE_TASK -> execute(request.getExecuteTask());
            case CANCEL_TASK -> cancel(
                    request.getCancelTask().getWorkerId(),
                    request.getCancelTask().getTaskId());
            case EXECUTE_CHECKPOINT_TASK, CHECKPOINT_ACK -> WorkerProtocolMapper.error(
                    ErrorCode.INVALID_REQUEST, null, "checkpoint messages require a checkpoint task stream");
            case OPERATION_NOT_SET -> WorkerProtocolMapper.error(
                    ErrorCode.INVALID_REQUEST, null, "Worker request has no operation");
        };
    }

    private void executeCheckpoint(
            int protocolVersion, ExecuteCheckpointTaskRequest request, InputStream input, OutputStream output)
            throws IOException {
        if (protocolVersion != WorkerProtocol.CHECKPOINT_VERSION
                || !workerId.equals(request.getWorkerId())
                || request.getJobId().isBlank()
                || request.getExecutionEpoch() <= 0
                || request.getTaskId().isBlank()
                || !request.hasSplit()
                || request.getMaxBatchRecords() <= 0
                || request.getMaxInFlightBatches() <= 0
                || request.getSplitFingerprint().isBlank()
                || !request.getTaskId().equals(request.getSplit().getSplitId())) {
            WorkerProtocolCodec.writeResponse(
                    output,
                    WorkerProtocolMapper.checkpointError(
                            ErrorCode.INVALID_REQUEST, request.getTaskId(), "invalid checkpoint task request"));
            return;
        }
        try {
            checkpointEpochFence.activate(request.getJobId(), request.getExecutionEpoch());
        } catch (EpochFencedException exception) {
            WorkerProtocolCodec.writeResponse(
                    output,
                    WorkerProtocolMapper.checkpointError(
                            ErrorCode.EPOCH_FENCED, request.getTaskId(), exception.getMessage()));
            return;
        }

        FutureTask<WorkerResponse> task = new FutureTask<>(() -> executeCheckpointTask(request, input, output));
        if (activeTasks.putIfAbsent(request.getTaskId(), task) != null) {
            WorkerProtocolCodec.writeResponse(
                    output,
                    WorkerProtocolMapper.checkpointError(
                            ErrorCode.TASK_REJECTED, request.getTaskId(), "task is already active"));
            return;
        }
        try {
            taskExecutor.execute(task);
        } catch (RejectedExecutionException exception) {
            activeTasks.remove(request.getTaskId(), task);
            WorkerProtocolCodec.writeResponse(
                    output,
                    WorkerProtocolMapper.checkpointError(
                            ErrorCode.RESOURCE_EXHAUSTED, request.getTaskId(), "Worker task capacity is full"));
            return;
        }
        try {
            WorkerProtocolCodec.writeResponse(output, task.get());
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            WorkerProtocolCodec.writeResponse(
                    output,
                    WorkerProtocolMapper.checkpointError(
                            ErrorCode.TASK_CANCELLED, request.getTaskId(), "task wait interrupted"));
        } catch (CancellationException exception) {
            WorkerProtocolCodec.writeResponse(
                    output,
                    WorkerProtocolMapper.checkpointError(
                            ErrorCode.TASK_CANCELLED, request.getTaskId(), "task was cancelled"));
        } catch (ExecutionException exception) {
            WorkerProtocolCodec.writeResponse(
                    output,
                    WorkerProtocolMapper.checkpointTaskFailure(
                            workerId,
                            request.getTaskId(),
                            exception.getCause(),
                            io.astrasync.engine.kernel.SyncResult.empty()));
        } finally {
            activeTasks.remove(request.getTaskId(), task);
        }
    }

    private WorkerResponse executeCheckpointTask(
            ExecuteCheckpointTaskRequest request, InputStream input, OutputStream output) {
        CheckpointExecutionContext context = WorkerProtocolMapper.checkpointContext(request, checkpointEpochFence);
        try {
            context.assertCurrent();
            SourceSplit split = WorkerProtocolMapper.toSplit(request);
            BatchTask task = Objects.requireNonNull(taskFactory.create(split, context), "task factory returned null");
            if (!split.equals(task.split())
                    || !request.getTaskId().equals(task.taskId())
                    || request.getMaxBatchRecords() != task.maxBatchRecords()
                    || request.getMaxInFlightBatches() != task.maxInFlightBatches()
                    || request.getExactlyOnce() != task.exactlyOnce()
                    || !WorkerProtocolMapper.matchesAdaptiveBatch(
                            request.hasAdaptiveBatch() ? request.getAdaptiveBatch() : null, task.batchPolicy())) {
                return WorkerProtocolMapper.checkpointError(
                        ErrorCode.TASK_REJECTED, request.getTaskId(), "task factory changed the requested split");
            }
            if (!(worker instanceof CheckpointBatchWorker checkpointWorker)) {
                return WorkerProtocolMapper.checkpointError(
                        ErrorCode.TASK_REJECTED, request.getTaskId(), "Worker does not support checkpoint tasks");
            }
            AtomicLong expectedSequence = new AtomicLong(Math.addExact(context.checkpointSequence(), 1));
            WorkerResult result = checkpointWorker.executeCheckpoint(context, task, progress -> {
                sendProgressAndAwaitAck(request, input, output, progress, expectedSequence.get());
                expectedSequence.incrementAndGet();
            });
            return WorkerProtocolMapper.checkpointSuccess(result);
        } catch (EpochFencedException exception) {
            return WorkerProtocolMapper.checkpointError(
                    ErrorCode.EPOCH_FENCED, request.getTaskId(), exception.getMessage());
        } catch (BatchTaskException exception) {
            return WorkerProtocolMapper.checkpointTaskFailure(
                    workerId,
                    request.getTaskId(),
                    exception.getCause() == null ? exception : exception.getCause(),
                    exception.partialResult());
        } catch (RuntimeException exception) {
            return WorkerProtocolMapper.checkpointTaskFailure(
                    workerId, request.getTaskId(), exception, io.astrasync.engine.kernel.SyncResult.empty());
        }
    }

    private void sendProgressAndAwaitAck(
            ExecuteCheckpointTaskRequest request,
            InputStream input,
            OutputStream output,
            CheckpointProgress progress,
            long expectedSequence) {
        try {
            if (!request.getJobId().equals(progress.jobId())
                    || request.getExecutionEpoch() != progress.executionEpoch()
                    || !request.getTaskId().equals(progress.taskId())
                    || progress.checkpointSequence() != expectedSequence) {
                throw new CheckpointProtocolException("checkpoint progress identity or sequence is invalid");
            }
            WorkerProtocolCodec.writeResponse(output, WorkerProtocolMapper.batchCommitted(progress, workerId));
            WorkerRequest acknowledgement = WorkerProtocolCodec.readRequest(input);
            if (acknowledgement.getProtocolVersion() != WorkerProtocol.CHECKPOINT_VERSION
                    || !acknowledgement.hasCheckpointAck()) {
                throw new CheckpointProtocolException("expected checkpoint acknowledgement");
            }
            CheckpointAckRequest ack = acknowledgement.getCheckpointAck();
            if (!workerId.equals(ack.getWorkerId())
                    || !request.getJobId().equals(ack.getJobId())
                    || request.getExecutionEpoch() != ack.getExecutionEpoch()
                    || !request.getTaskId().equals(ack.getTaskId())
                    || progress.checkpointSequence() != ack.getCheckpointSequence()
                    || !progress.fingerprint().equals(ack.getCheckpointFingerprint())) {
                throw new CheckpointProtocolException("checkpoint acknowledgement identity does not match progress");
            }
            checkpointEpochFence.assertCurrent(request.getJobId(), request.getExecutionEpoch());
        } catch (IOException exception) {
            throw new CheckpointProtocolException("failed to receive checkpoint acknowledgement", exception);
        }
    }

    private WorkerResponse execute(ExecuteTaskRequest request) {
        if (!workerId.equals(request.getWorkerId())
                || request.getTaskId().isBlank()
                || !request.hasSplit()
                || request.getMaxBatchRecords() <= 0
                || request.getMaxInFlightBatches() <= 0
                || !request.getTaskId().equals(request.getSplit().getSplitId())) {
            return WorkerProtocolMapper.error(ErrorCode.INVALID_REQUEST, request.getTaskId(), "invalid task request");
        }
        FutureTask<WorkerResponse> task = new FutureTask<>(() -> executeTask(request));
        if (activeTasks.putIfAbsent(request.getTaskId(), task) != null) {
            return WorkerProtocolMapper.error(ErrorCode.TASK_REJECTED, request.getTaskId(), "task is already active");
        }
        try {
            taskExecutor.execute(task);
        } catch (RejectedExecutionException exception) {
            activeTasks.remove(request.getTaskId(), task);
            return WorkerProtocolMapper.error(
                    ErrorCode.RESOURCE_EXHAUSTED, request.getTaskId(), "Worker task capacity is full");
        }
        try {
            return task.get();
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            return WorkerProtocolMapper.error(ErrorCode.TASK_CANCELLED, request.getTaskId(), "task wait interrupted");
        } catch (CancellationException exception) {
            return WorkerProtocolMapper.error(ErrorCode.TASK_CANCELLED, request.getTaskId(), "task was cancelled");
        } catch (ExecutionException exception) {
            return WorkerProtocolMapper.error(
                    ErrorCode.INTERNAL,
                    request.getTaskId(),
                    exception.getCause().toString());
        } finally {
            activeTasks.remove(request.getTaskId(), task);
        }
    }

    private WorkerResponse executeTask(ExecuteTaskRequest request) {
        SourceSplit split = WorkerProtocolMapper.toSplit(request);
        try {
            BatchTask task = Objects.requireNonNull(taskFactory.create(split), "task factory returned null");
            if (!split.equals(task.split())
                    || !request.getTaskId().equals(task.taskId())
                    || request.getMaxBatchRecords() != task.maxBatchRecords()
                    || request.getMaxInFlightBatches() != task.maxInFlightBatches()
                    || !WorkerProtocolMapper.matchesAdaptiveBatch(
                            request.hasAdaptiveBatch() ? request.getAdaptiveBatch() : null, task.batchPolicy())) {
                return WorkerProtocolMapper.error(
                        ErrorCode.TASK_REJECTED, request.getTaskId(), "task factory changed the requested split");
            }
            WorkerResult result = worker.execute(task);
            return WorkerProtocolMapper.success(result);
        } catch (BatchTaskException exception) {
            return WorkerProtocolMapper.taskFailure(
                    workerId,
                    request.getTaskId(),
                    exception.getCause() == null ? exception : exception.getCause(),
                    exception.partialResult());
        } catch (RuntimeException exception) {
            return WorkerProtocolMapper.taskFailure(
                    workerId, request.getTaskId(), exception, WorkerProtocolMapper.partialResult(exception));
        }
    }

    private WorkerResponse cancel(String requestedWorkerId, String taskId) {
        if (!workerId.equals(requestedWorkerId) || taskId.isBlank()) {
            return WorkerProtocolMapper.error(ErrorCode.INVALID_REQUEST, taskId, "invalid cancel request");
        }
        FutureTask<WorkerResponse> task = activeTasks.get(taskId);
        if (task == null) {
            return WorkerProtocolMapper.error(ErrorCode.TASK_NOT_FOUND, taskId, "task is not active");
        }
        boolean cancelled = task.cancel(true);
        return WorkerProtocolMapper.cancelled(
                taskId, cancelled, cancelled ? "task cancellation requested" : "task already completed");
    }

    private static Thread daemonThread(Runnable runnable, String prefix) {
        Thread thread = new Thread(runnable, prefix + System.nanoTime());
        thread.setDaemon(true);
        return thread;
    }

    private static final class CheckpointProtocolException extends RuntimeException {
        private static final long serialVersionUID = 1L;

        private CheckpointProtocolException(String message) {
            super(message);
        }

        private CheckpointProtocolException(String message, Throwable cause) {
            super(message, cause);
        }
    }

    private static void closeQuietly(Socket socket) {
        try {
            socket.close();
        } catch (IOException ignored) {
            // The connection was rejected before a protocol response could be written.
        }
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
