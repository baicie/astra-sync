package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.fasterxml.jackson.databind.ObjectMapper;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.engine.checkpoint.FileSplitProgressStore;
import io.astrasync.engine.checkpoint.SplitPlanMismatchException;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskException;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.worker.JdbcWorkerTaskFactoryProvider;
import io.astrasync.engine.worker.WorkerConfiguration;
import io.astrasync.engine.worker.WorkerService;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.CopyOnWriteArrayList;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class CoordinatorApplicationTest {
    private static final String JOB_ID = "distributed-jdbc";
    private static final String FIRST_SPLIT = "jdbc:SOURCE_DATA-split-0";
    private static final String SECOND_SPLIT = "jdbc:SOURCE_DATA-split-1";

    @TempDir
    Path tempDirectory;

    @Test
    void runsJdbcFullLoadAcrossTwoTcpWorkers() throws Exception {
        String url = jdbcUrl();
        initializeDatabase(url);
        Path jobSpec = writeJob(url);
        List<String> assignments = new CopyOnWriteArrayList<>();
        BatchTaskFactory delegate = jdbcTaskFactory(jobSpec);

        try (WorkerService worker0 = worker("worker-0", recording(delegate, "worker-0", assignments));
                WorkerService worker1 = worker("worker-1", recording(delegate, "worker-1", assignments))) {
            worker0.start();
            worker1.start();

            ResumableRunResult result = CoordinatorApplication.run(
                    configuration(jobSpec, tempDirectory.resolve("full-load-progress"), endpoints(worker0, worker1)));

            assertThat(result.resumedSplitCount()).isZero();
            assertThat(result.executedSplitCount()).isEqualTo(2);
            assertThat(result.executionEpoch()).isZero();
            assertThat(result.recoveredSplitCount()).isZero();
            assertThat(result.metrics().readCount()).isEqualTo(4);
            assertThat(result.metrics().writtenCount()).isEqualTo(4);
            assertThat(assignments).containsExactlyInAnyOrder("worker-0:" + FIRST_SPLIT, "worker-1:" + SECOND_SPLIT);
            assertThat(readTarget(url)).containsExactly("1:Ada", "2:Lin", "3:Kai", "4:May");
        }
    }

    @Test
    void resumesDurableSplitAfterRemoteFailureAndRejectsPlanDriftBeforeMaterialization() throws Exception {
        String url = jdbcUrl();
        initializeDatabase(url);
        Path jobSpec = writeJob(url);
        Path progressDirectory = tempDirectory.resolve("restart-progress");
        List<String> firstAssignments = new CopyOnWriteArrayList<>();
        BatchTaskFactory delegate = jdbcTaskFactory(jobSpec);

        try (WorkerService worker0 = worker("worker-0", recording(delegate, "worker-0", firstAssignments));
                WorkerService worker1 =
                        worker("worker-1", failingAfterCompletion(progressDirectory, "worker-1", firstAssignments))) {
            worker0.start();
            worker1.start();
            CoordinatorConfiguration configuration =
                    configuration(jobSpec, progressDirectory, endpoints(worker0, worker1));

            assertThatThrownBy(() -> CoordinatorApplication.run(configuration))
                    .isInstanceOf(BatchTaskException.class)
                    .hasRootCauseMessage("remote task failed: failed to open source");
        }

        assertThat(firstAssignments).containsExactlyInAnyOrder("worker-0:" + FIRST_SPLIT, "worker-1:" + SECOND_SPLIT);
        assertThat(new FileSplitProgressStore(progressDirectory)
                        .load(JOB_ID)
                        .orElseThrow()
                        .completedSplits())
                .containsOnlyKeys(FIRST_SPLIT);
        assertThat(readTarget(url)).containsExactly("1:Ada", "2:Lin");

        List<String> resumedAssignments = new CopyOnWriteArrayList<>();
        BatchTaskFactory resumedDelegate = jdbcTaskFactory(jobSpec);
        try (WorkerService worker0 = worker("worker-0", recording(resumedDelegate, "worker-0", resumedAssignments));
                WorkerService worker1 =
                        worker("worker-1", recording(resumedDelegate, "worker-1", resumedAssignments))) {
            worker0.start();
            worker1.start();
            CoordinatorConfiguration configuration =
                    configuration(jobSpec, progressDirectory, endpoints(worker0, worker1));

            ResumableRunResult resumed = CoordinatorApplication.run(configuration);

            assertThat(resumed.resumedSplitCount()).isEqualTo(1);
            assertThat(resumed.executedSplitCount()).isEqualTo(1);
            assertThat(resumedAssignments).containsExactly("worker-0:" + SECOND_SPLIT);
            assertThat(readTarget(url)).containsExactly("1:Ada", "2:Lin", "3:Kai", "4:May");

            ResumableRunResult alreadyComplete = CoordinatorApplication.run(configuration);
            assertThat(alreadyComplete.resumedSplitCount()).isEqualTo(2);
            assertThat(alreadyComplete.executedSplitCount()).isZero();
            assertThat(resumedAssignments).containsExactly("worker-0:" + SECOND_SPLIT);

            execute(url, "INSERT INTO SOURCE_DATA VALUES (0, 'New')");
            assertThatThrownBy(() -> CoordinatorApplication.run(configuration))
                    .isInstanceOf(SplitPlanMismatchException.class)
                    .hasMessage("split plan changed for job " + JOB_ID);
            assertThat(resumedAssignments).containsExactly("worker-0:" + SECOND_SPLIT);
        }
    }

    private static BatchTaskFactory recording(BatchTaskFactory delegate, String workerId, List<String> assignments) {
        return split -> {
            assignments.add(workerId + ':' + split.splitId());
            return delegate.create(split);
        };
    }

    private static BatchTaskFactory failingAfterCompletion(
            Path progressDirectory, String workerId, List<String> assignments) {
        return split -> {
            assignments.add(workerId + ':' + split.splitId());
            return new BatchTask(split, new FailAfterDurableCompletionSource(progressDirectory), new EmptySink(), 2, 1);
        };
    }

    private static BatchTaskFactory jdbcTaskFactory(Path jobSpec) {
        return new JdbcWorkerTaskFactoryProvider()
                .create(Map.of(JdbcWorkerTaskFactoryProvider.JOB_SPEC_ENVIRONMENT, jobSpec.toString()));
    }

    private static WorkerService worker(String workerId, BatchTaskFactory taskFactory) {
        return new WorkerService(
                new WorkerConfiguration(workerId, 0, JdbcWorkerTaskFactoryProvider.class.getName(), 1, 0, 4),
                taskFactory);
    }

    private static List<WorkerEndpoint> endpoints(WorkerService worker0, WorkerService worker1) {
        return List.of(
                new WorkerEndpoint("worker-0", "127.0.0.1", worker0.port()),
                new WorkerEndpoint("worker-1", "127.0.0.1", worker1.port()));
    }

    private static CoordinatorConfiguration configuration(
            Path jobSpec, Path progressDirectory, List<WorkerEndpoint> endpoints) {
        return new CoordinatorConfiguration(jobSpec, progressDirectory, endpoints, Duration.ofSeconds(15), 1, 1);
    }

    private Path writeJob(String url) throws IOException {
        Path path = tempDirectory.resolve(JOB_ID + ".yaml");
        Files.writeString(
                path,
                """
                apiVersion: sync.astrasync.io/v1
                kind: SyncJob
                metadata:
                  name: %s
                spec:
                  source:
                    connector: jdbc
                    options:
                      url: '%s'
                      table: SOURCE_DATA
                      splitColumn: ID
                      splitCount: '2'
                  sink:
                    connector: jdbc
                    options:
                      url: '%s'
                      table: TARGET_DATA
                  delivery:
                    guarantee: at-most-once
                  runtime:
                    maxBatchRecords: 2
                """
                        .formatted(JOB_ID, yaml(url), yaml(url)),
                StandardCharsets.UTF_8);
        return path;
    }

    private static void initializeDatabase(String url) throws SQLException {
        execute(url, "CREATE TABLE SOURCE_DATA (ID INT NOT NULL, NAME VARCHAR(40))");
        execute(url, "CREATE TABLE TARGET_DATA (ID INT PRIMARY KEY, NAME VARCHAR(40))");
        execute(url, "INSERT INTO SOURCE_DATA VALUES (1, 'Ada'), (2, 'Lin'), (3, 'Kai'), (4, 'May')");
    }

    private static List<String> readTarget(String url) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                ResultSet result =
                        connection.createStatement().executeQuery("SELECT ID, NAME FROM TARGET_DATA ORDER BY ID")) {
            List<String> rows = new ArrayList<>();
            while (result.next()) {
                rows.add(result.getInt(1) + ":" + result.getString(2));
            }
            return rows;
        }
    }

    private static void execute(String url, String sql) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                Statement statement = connection.createStatement()) {
            statement.execute(sql);
        }
    }

    private static String jdbcUrl() {
        return "jdbc:h2:mem:coordinator_" + UUID.randomUUID().toString().replace('-', '_')
                + ";MODE=PostgreSQL;DB_CLOSE_DELAY=-1";
    }

    private static String yaml(String value) {
        return value.replace("'", "''");
    }

    private static final class FailAfterDurableCompletionSource implements BatchSource {
        private static final ObjectMapper MAPPER = new ObjectMapper();

        private final Path progressDirectory;

        private FailAfterDurableCompletionSource(Path progressDirectory) {
            this.progressDirectory = progressDirectory;
        }

        @Override
        public void open() {
            long deadline = System.nanoTime() + Duration.ofSeconds(10).toNanos();
            while (System.nanoTime() < deadline) {
                if (firstSplitIsDurable()) {
                    throw new IllegalStateException("planned restart failure after durable completion");
                }
                try {
                    Thread.sleep(10);
                } catch (InterruptedException exception) {
                    Thread.currentThread().interrupt();
                    throw new IllegalStateException("planned restart wait interrupted", exception);
                }
            }
            throw new IllegalStateException("timed out waiting for durable split completion");
        }

        private boolean firstSplitIsDurable() {
            Path manifest = progressDirectory.resolve(JOB_ID + ".json");
            if (!Files.exists(manifest)) {
                return false;
            }
            try {
                return MAPPER.readTree(manifest.toFile())
                        .path("completedSplits")
                        .has(FIRST_SPLIT);
            } catch (IOException exception) {
                return false;
            }
        }

        @Override
        public RowBatch readBatch(int maxRows) {
            throw new IllegalStateException("failing source must not be read");
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
