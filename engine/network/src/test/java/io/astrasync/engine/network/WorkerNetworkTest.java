package io.astrasync.engine.network;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.LoggerContext;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.core.read.ListAppender;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.AdaptiveBatchPolicy;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.WorkerResult;
import io.astrasync.protocol.worker.ExecuteCheckpointTaskRequest;
import io.astrasync.protocol.worker.ExecuteTaskRequest;
import io.astrasync.protocol.worker.SplitDescriptor;
import io.astrasync.protocol.worker.WorkerRequest;
import io.astrasync.protocol.worker.WorkerResponse;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.net.Socket;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;
import org.slf4j.LoggerFactory;

class WorkerNetworkTest {
    private static final Duration TIMEOUT = Duration.ofSeconds(5);
    private static final String JOB_ID = "3f36d9c6-77a2-4e83-8c7d-dc59b9b94a54";
    private static final String TENANT_ID = "4f36d9c6-77a2-4e83-8c7d-dc59b9b94a55";
    private static final String REQUEST_ID = "5f36d9c6-77a2-4e83-8c7d-dc59b9b94a56";

    @Test
    void executesTaskThroughVersionedRemoteWorkerAndMaterializesOnServer() {
        RecordingWorker worker = new RecordingWorker("worker-a");
        try (WorkerServer server = server(worker, 1, 1, 4)) {
            server.start();
            RemoteBatchWorker remote = remote(server, 2);

            WorkerResult result = remote.execute(task("split-1").withIdentity(JOB_ID, TENANT_ID, REQUEST_ID));

            assertThat(result).isEqualTo(new WorkerResult("worker-a", "split-1", new SyncResult(3, 3, 2, 2, 7)));
            assertThat(worker.tasks).singleElement().satisfies(materialized -> {
                assertThat(materialized.split()).isEqualTo(split("split-1"));
                assertThat(materialized.maxBatchRecords()).isEqualTo(3);
                assertThat(materialized.maxInFlightBatches()).isEqualTo(2);
                assertThat(materialized.jobId()).isEqualTo(JOB_ID);
                assertThat(materialized.tenantId()).isEqualTo(TENANT_ID);
                assertThat(materialized.requestId()).isEqualTo(REQUEST_ID);
            });
        }
    }

    @Test
    void logsRemoteTaskWithTrustedIdentity() {
        RecordingWorker worker = new RecordingWorker("worker-a");
        Logger logger = (Logger) LoggerFactory.getLogger(RemoteBatchWorker.class);
        LoggerContext context = (LoggerContext) LoggerFactory.getILoggerFactory();
        ListAppender<ILoggingEvent> appender = new ListAppender<>();
        appender.setContext(context);
        appender.start();
        logger.addAppender(appender);

        try (WorkerServer server = server(worker, 1, 1, 4)) {
            server.start();
            remote(server, 2).execute(task("split-1").withIdentity(JOB_ID, TENANT_ID, REQUEST_ID));
        } finally {
            logger.detachAppender(appender);
        }

        assertThat(appender.list).isNotEmpty();
        assertThat(appender.list).allSatisfy(event -> {
            assertThat(event.getMDCPropertyMap()).containsEntry("tenant_id", TENANT_ID);
            assertThat(event.getMDCPropertyMap()).containsEntry("job_id", JOB_ID);
            assertThat(event.getMDCPropertyMap()).containsEntry("worker_id", "worker-a");
            assertThat(event.getMDCPropertyMap()).containsEntry("request_id", REQUEST_ID);
        });
    }

    @Test
    void rejectsNewTasksWhenTheRemoteWorkerQueueIsFull() throws Exception {
        BlockingWorker worker = new BlockingWorker("worker-a");
        try (WorkerServer server = server(worker, 1, 0, 4)) {
            server.start();
            RemoteBatchWorker remote = remote(server, 2);
            ExecutorService calls = Executors.newFixedThreadPool(2);
            try {
                Future<WorkerResult> first = calls.submit(() -> remote.execute(task("split-1")));
                assertThat(worker.started.await(5, TimeUnit.SECONDS)).isTrue();
                Future<WorkerResult> second = calls.submit(() -> remote.execute(task("split-2")));

                assertThatThrownBy(() -> second.get(5, TimeUnit.SECONDS))
                        .isInstanceOf(ExecutionException.class)
                        .hasCauseInstanceOf(NetworkWorkerException.class)
                        .hasRootCauseMessage(
                                "remote Worker rejected task due to backpressure: Worker task capacity is full");
                worker.release.countDown();
                assertThat(first.get(5, TimeUnit.SECONDS).taskId()).isEqualTo("split-1");
            } finally {
                calls.shutdownNow();
            }
        }
    }

    @Test
    void cancellationInterruptsTheRemoteTask() throws Exception {
        BlockingWorker worker = new BlockingWorker("worker-a");
        try (WorkerServer server = server(worker, 1, 0, 4)) {
            server.start();
            WorkerClient client = new WorkerClient("127.0.0.1", server.port(), TIMEOUT);
            ExecutorService calls = Executors.newSingleThreadExecutor();
            try {
                Future<WorkerResult> execution = calls.submit(() -> client.execute("worker-a", task("split-1")));
                assertThat(worker.started.await(5, TimeUnit.SECONDS)).isTrue();

                assertThat(client.cancel("worker-a", "split-1", "test cancellation"))
                        .isTrue();
                assertThatThrownBy(() -> execution.get(5, TimeUnit.SECONDS))
                        .isInstanceOf(ExecutionException.class)
                        .hasCauseInstanceOf(NetworkWorkerException.class);
                assertThat(worker.interrupted.await(5, TimeUnit.SECONDS)).isTrue();
            } finally {
                calls.shutdownNow();
            }
        }
    }

    @Test
    void rejectsAWorkerPolicyMismatchBeforeExecution() {
        RecordingWorker worker = new RecordingWorker("worker-a");
        try (WorkerServer server = server(worker, 1, 1, 4)) {
            server.start();
            BatchTask adaptiveTask = new RemoteTaskFactory(
                            3, 2, false, AdaptiveBatchPolicy.adaptive(1, 2, 1_000_000, 0))
                    .create(split("split-1"));

            assertThatThrownBy(() -> remote(server, 2).execute(adaptiveTask))
                    .isInstanceOf(NetworkWorkerException.class)
                    .hasMessageContaining("task factory changed the requested split");
            assertThat(worker.tasks).isEmpty();
        }
    }

    @Test
    void acceptsOlderRequestsWithoutIdentityFields() throws Exception {
        RecordingWorker worker = new RecordingWorker("worker-a");
        try (WorkerServer server = server(worker, 1, 1, 4)) {
            server.start();
            SourceSplit split = split("split-1");
            WorkerRequest request = WorkerRequest.newBuilder()
                    .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                    .setExecuteTask(ExecuteTaskRequest.newBuilder()
                            .setWorkerId("worker-a")
                            .setTaskId("split-1")
                            .setSplit(SplitDescriptor.newBuilder()
                                    .setSplitId("split-1")
                                    .setSourceId("test-source")
                                    .putStartOffsets("id", "1")
                                    .build())
                            .setMaxBatchRecords(3)
                            .setMaxInFlightBatches(2)
                            .build())
                    .build();

            try (Socket socket = new Socket("127.0.0.1", server.port())) {
                WorkerProtocolCodec.writeRequest(socket.getOutputStream(), request);
                WorkerResponse response = WorkerProtocolCodec.readResponse(socket.getInputStream());

                assertThat(response.hasTaskResult()).isTrue();
                assertThat(response.getTaskResult().getSuccess()).isTrue();
                assertThat(worker.tasks).singleElement().satisfies(materialized -> {
                    assertThat(materialized.split()).isEqualTo(split);
                    assertThat(materialized.jobId()).isEqualTo(BatchTask.UNKNOWN_JOB_ID);
                    assertThat(materialized.tenantId()).isEqualTo(BatchTask.UNKNOWN_TENANT_ID);
                    assertThat(materialized.requestId()).isEqualTo(BatchTask.UNKNOWN_REQUEST_ID);
                });
            }
        }
    }

    @Test
    void logsAdmissionRejectionsWithWorkerIdentity() throws Exception {
        RecordingWorker worker = new RecordingWorker("worker-a");
        Logger logger = (Logger) LoggerFactory.getLogger(WorkerServer.class);
        LoggerContext context = (LoggerContext) LoggerFactory.getILoggerFactory();
        ListAppender<ILoggingEvent> appender = new ListAppender<>();
        appender.setContext(context);
        appender.start();
        logger.addAppender(appender);

        try (WorkerServer server = server(worker, 1, 1, 4)) {
            server.start();
            WorkerRequest request = WorkerRequest.newBuilder()
                    .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                    .setExecuteTask(ExecuteTaskRequest.newBuilder()
                            .setWorkerId("worker-a")
                            .build())
                    .build();

            try (Socket socket = new Socket("127.0.0.1", server.port())) {
                WorkerProtocolCodec.writeRequest(socket.getOutputStream(), request);
                WorkerResponse response = WorkerProtocolCodec.readResponse(socket.getInputStream());
                assertThat(response.hasError()).isTrue();
            }
        } finally {
            logger.detachAppender(appender);
        }

        assertThat(appender.list).anySatisfy(event -> {
            assertThat(event.getLevel()).isEqualTo(ch.qos.logback.classic.Level.WARN);
            assertThat(event.getMDCPropertyMap()).containsEntry("worker_id", "worker-a");
            assertThat(event.getMDCPropertyMap()).doesNotContainKey("request_id");
        });
    }

    @Test
    void logsCheckpointAdmissionRejectionsWithTaskIdentity() throws Exception {
        RecordingWorker worker = new RecordingWorker("worker-a");
        Logger logger = (Logger) LoggerFactory.getLogger(WorkerServer.class);
        LoggerContext context = (LoggerContext) LoggerFactory.getILoggerFactory();
        ListAppender<ILoggingEvent> appender = new ListAppender<>();
        appender.setContext(context);
        appender.start();
        logger.addAppender(appender);

        try (WorkerServer server = server(worker, 1, 1, 4)) {
            server.start();
            WorkerRequest request = WorkerRequest.newBuilder()
                    .setProtocolVersion(WorkerProtocol.CHECKPOINT_VERSION)
                    .setExecuteCheckpointTask(ExecuteCheckpointTaskRequest.newBuilder()
                            .setWorkerId("worker-b")
                            .setJobId("orders")
                            .setExecutionEpoch(1)
                            .setTaskId("split-1")
                            .setSplit(SplitDescriptor.newBuilder()
                                    .setSplitId("split-1")
                                    .setSourceId("test-source")
                                    .putStartOffsets("id", "1")
                                    .build())
                            .setMaxBatchRecords(3)
                            .setMaxInFlightBatches(2)
                            .setSplitFingerprint("fingerprint")
                            .setTenantId(TENANT_ID)
                            .setRequestId(REQUEST_ID)
                            .build())
                    .build();

            try (Socket socket = new Socket("127.0.0.1", server.port())) {
                WorkerProtocolCodec.writeRequest(socket.getOutputStream(), request);
                WorkerResponse response = WorkerProtocolCodec.readResponse(socket.getInputStream());
                assertThat(response.hasError()).isTrue();
            }
        } finally {
            logger.detachAppender(appender);
        }

        assertThat(appender.list).anySatisfy(event -> {
            assertThat(event.getLevel()).isEqualTo(ch.qos.logback.classic.Level.WARN);
            assertThat(event.getMDCPropertyMap()).containsEntry("worker_id", "worker-a");
            assertThat(event.getMDCPropertyMap()).containsEntry("job_id", "orders");
            assertThat(event.getMDCPropertyMap()).containsEntry("tenant_id", TENANT_ID);
            assertThat(event.getMDCPropertyMap()).containsEntry("request_id", REQUEST_ID);
        });
    }

    @Test
    void framedCodecRejectsOversizedAndTruncatedFrames() throws Exception {
        WorkerRequest request = WorkerRequest.newBuilder()
                .setProtocolVersion(WorkerProtocol.CURRENT_VERSION)
                .build();
        ByteArrayOutputStream encoded = new ByteArrayOutputStream();

        assertThatThrownBy(() -> WorkerProtocolCodec.readRequest(new ByteArrayInputStream(new byte[] {0, 0, 0, 0})))
                .isInstanceOf(IOException.class)
                .hasMessage("invalid worker protocol frame length: 0");
        assertThatThrownBy(() -> WorkerProtocolCodec.readRequest(new ByteArrayInputStream(new byte[] {0, 0, 0, 4, 1})))
                .isInstanceOf(IOException.class);
        WorkerProtocolCodec.writeRequest(encoded, request);
        assertThat(WorkerProtocolCodec.readRequest(new ByteArrayInputStream(encoded.toByteArray())))
                .isEqualTo(request);

        ByteArrayOutputStream oversized = new ByteArrayOutputStream();
        DataOutputStream data = new DataOutputStream(oversized);
        data.writeInt(WorkerProtocol.MAX_FRAME_BYTES + 1);
        data.flush();
        assertThatThrownBy(() -> WorkerProtocolCodec.readRequest(new ByteArrayInputStream(oversized.toByteArray())))
                .isInstanceOf(IOException.class)
                .hasMessageContaining("maximum size");
    }

    private static WorkerServer server(RecordingWorker worker, int concurrency, int queue, int connections) {
        return new WorkerServer("worker-a", 0, factory(), worker, concurrency, queue, connections);
    }

    private static WorkerServer server(BlockingWorker worker, int concurrency, int queue, int connections) {
        return new WorkerServer("worker-a", 0, factory(), worker, concurrency, queue, connections);
    }

    private static RemoteBatchWorker remote(WorkerServer server, int maxInFlight) {
        return new RemoteBatchWorker("worker-a", new WorkerClient("127.0.0.1", server.port(), TIMEOUT), maxInFlight);
    }

    private static BatchTaskFactory factory() {
        return split -> new BatchTask(split, new EmptySource(), new EmptySink(), 3, 2);
    }

    private static BatchTask task(String taskId) {
        return new BatchTask(split(taskId), new EmptySource(), new EmptySink(), 3, 2);
    }

    private static SourceSplit split(String splitId) {
        return new SourceSplit(splitId, "test-source", new SplitPosition(Map.of("id", "1")), SplitPosition.unbounded());
    }

    private static final class RecordingWorker implements BatchWorker {
        private final String workerId;
        private final List<BatchTask> tasks = new ArrayList<>();

        private RecordingWorker(String workerId) {
            this.workerId = workerId;
        }

        @Override
        public String workerId() {
            return workerId;
        }

        @Override
        public WorkerResult execute(BatchTask task) {
            tasks.add(task);
            return new WorkerResult(workerId, task.taskId(), new SyncResult(3, 3, 2, 2, 7));
        }
    }

    private static final class BlockingWorker implements BatchWorker {
        private final String workerId;
        private final CountDownLatch started = new CountDownLatch(1);
        private final CountDownLatch release = new CountDownLatch(1);
        private final CountDownLatch interrupted = new CountDownLatch(1);

        private BlockingWorker(String workerId) {
            this.workerId = workerId;
        }

        @Override
        public String workerId() {
            return workerId;
        }

        @Override
        public WorkerResult execute(BatchTask task) {
            started.countDown();
            try {
                release.await();
            } catch (InterruptedException exception) {
                interrupted.countDown();
                Thread.currentThread().interrupt();
                throw new NetworkWorkerException("remote task interrupted", exception);
            }
            return new WorkerResult(workerId, task.taskId(), SyncResult.empty());
        }
    }

    private static final class EmptySource implements BatchSource {
        @Override
        public void open() {}

        @Override
        public RowBatch readBatch(int maxRows) {
            return RowBatch.end();
        }

        @Override
        public void close() {}
    }

    private static final class EmptySink implements BatchSink {
        @Override
        public void open() {}

        @Override
        public void writeBatch(RowBatch batch) {}

        @Override
        public void close() {}
    }
}
