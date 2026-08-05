package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.IdempotentBatchSink;
import io.astrasync.connector.api.source.CheckpointableBatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.checkpoint.FileCheckpointStore;
import io.astrasync.engine.checkpoint.SplitPlanMismatchException;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskException;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
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

class CheckpointCoordinatorApplicationTest {
    private static final String JOB_ID = "checkpoint-jdbc";
    private static final String SPLIT_ID = "jdbc:SOURCE_DATA-split-0";

    @TempDir
    Path tempDirectory;

    @Test
    void resumesOnlyTheUnfinishedJdbcRangeAndSkipsItsDurableCompletion() throws Exception {
        String url = jdbcUrl();
        initializeDatabase(url);
        Path jobSpec = writeJob(url);
        Path progressDirectory = tempDirectory.resolve("checkpoint-progress");
        List<SplitPosition> materializedPositions = new CopyOnWriteArrayList<>();

        BatchTaskFactory firstFactory = checkpointFactory(jobSpec, materializedPositions, true);
        try (WorkerService worker = worker(firstFactory)) {
            worker.start();
            assertThatThrownBy(() -> CoordinatorApplication.run(configuration(jobSpec, progressDirectory, worker)))
                    .isInstanceOf(BatchTaskException.class)
                    .hasRootCauseMessage("remote task failed: planned failure after first checkpointed batch");
        }

        FileCheckpointStore store = new FileCheckpointStore(progressDirectory);
        assertThat(store.load(JOB_ID, SPLIT_ID)).hasValueSatisfying(checkpoint -> {
            assertThat(checkpoint.checkpointSequence()).isEqualTo(1);
            assertThat(checkpoint.sourcePosition().offsets()).containsEntry("ID", "2");
            assertThat(checkpoint.executionEpoch()).isEqualTo(1);
        });
        assertThat(store.loadCompletion(JOB_ID, SPLIT_ID)).isEmpty();
        assertThat(readTarget(url)).containsExactly("1:Ada", "2:Lin");

        BatchTaskFactory resumedFactory = checkpointFactory(jobSpec, materializedPositions, false);
        try (WorkerService worker = worker(resumedFactory)) {
            worker.start();
            CoordinatorConfiguration configuration = configuration(jobSpec, progressDirectory, worker);

            ResumableRunResult resumed = CoordinatorApplication.run(configuration);

            assertThat(resumed.executedSplitCount()).isEqualTo(1);
            assertThat(resumed.recoveredSplitCount()).isEqualTo(1);
            assertThat(resumed.executionEpoch()).isEqualTo(2);
            assertThat(readTarget(url)).containsExactly("1:Ada", "2:Lin", "3:Kai", "4:May");
            assertThat(materializedPositions)
                    .extracting(position -> position.offsets().get("ID"))
                    .containsExactly(null, "2");
            assertThat(store.loadCompletion(JOB_ID, SPLIT_ID)).isPresent();

            ResumableRunResult complete = CoordinatorApplication.run(configuration);
            assertThat(complete.resumedSplitCount()).isEqualTo(1);
            assertThat(complete.executedSplitCount()).isZero();
            assertThat(complete.recoveredSplitCount()).isZero();
            assertThat(complete.executionEpoch()).isEqualTo(3);
            assertThat(materializedPositions).hasSize(2);

            execute(url, "INSERT INTO SOURCE_DATA VALUES (0, 'New')");
            assertThatThrownBy(() -> CoordinatorApplication.run(configuration))
                    .isInstanceOf(SplitPlanMismatchException.class)
                    .hasMessage("split plan changed for job " + JOB_ID);
            assertThat(materializedPositions).hasSize(2);
        }
    }

    @Test
    void retriesACommittedExactlyOnceBatchWithoutDuplicatingTheTargetRows() throws Exception {
        String url = jdbcUrl();
        initializeDatabase(url);
        Path jobSpec = writeJob(url, "exactly-once");
        Path progressDirectory = tempDirectory.resolve("exactly-once-progress");

        BatchTaskFactory failingFactory = exactlyOnceFactory(jobSpec, true);
        try (WorkerService worker = worker(failingFactory)) {
            worker.start();
            assertThatThrownBy(() -> CoordinatorApplication.run(configuration(jobSpec, progressDirectory, worker)))
                    .isInstanceOf(BatchTaskException.class)
                    .hasRootCauseMessage("remote task failed: planned failure after exactly-once sink commit");
        }

        assertThat(readTarget(url)).containsExactly("1:Ada", "2:Lin");
        FileCheckpointStore store = new FileCheckpointStore(progressDirectory);
        assertThat(store.load(JOB_ID, SPLIT_ID)).isEmpty();

        try (WorkerService worker = worker(exactlyOnceFactory(jobSpec, false))) {
            worker.start();
            ResumableRunResult recovered =
                    CoordinatorApplication.run(configuration(jobSpec, progressDirectory, worker));

            assertThat(recovered.executionEpoch()).isEqualTo(2);
            assertThat(readTarget(url)).containsExactly("1:Ada", "2:Lin", "3:Kai", "4:May");
            assertThat(store.loadCompletion(JOB_ID, SPLIT_ID)).isPresent();
        }
    }

    private static BatchTaskFactory checkpointFactory(
            Path jobSpec, List<SplitPosition> positions, boolean failAfterFirstBatch) {
        BatchTaskFactory delegate = new JdbcWorkerTaskFactoryProvider()
                .create(Map.of(JdbcWorkerTaskFactoryProvider.JOB_SPEC_ENVIRONMENT, jobSpec.toString()));
        return new BatchTaskFactory() {
            @Override
            public BatchTask create(SourceSplit split) {
                return delegate.create(split);
            }

            @Override
            public BatchTask create(SourceSplit split, CheckpointExecutionContext context) {
                positions.add(context.sourcePosition());
                BatchTask task = delegate.create(split, context);
                if (!failAfterFirstBatch) {
                    return task;
                }
                return new BatchTask(
                        task.split(),
                        new FailAfterFirstBatchSource((CheckpointableBatchSource) task.source()),
                        task.sink(),
                        task.maxBatchRecords(),
                        task.maxInFlightBatches());
            }
        };
    }

    private static BatchTaskFactory exactlyOnceFactory(Path jobSpec, boolean failAfterFirstCommit) {
        BatchTaskFactory delegate = new JdbcWorkerTaskFactoryProvider()
                .create(Map.of(JdbcWorkerTaskFactoryProvider.JOB_SPEC_ENVIRONMENT, jobSpec.toString()));
        return new BatchTaskFactory() {
            @Override
            public BatchTask create(SourceSplit split) {
                return delegate.create(split);
            }

            @Override
            public BatchTask create(SourceSplit split, CheckpointExecutionContext context) {
                BatchTask task = delegate.create(split, context);
                if (!failAfterFirstCommit) {
                    return task;
                }
                return new BatchTask(
                        task.split(),
                        task.source(),
                        new FailAfterCommitSink((IdempotentBatchSink) task.sink()),
                        task.maxBatchRecords(),
                        task.maxInFlightBatches(),
                        task.exactlyOnce());
            }
        };
    }

    private static WorkerService worker(BatchTaskFactory taskFactory) {
        return new WorkerService(
                new WorkerConfiguration("worker-0", 0, JdbcWorkerTaskFactoryProvider.class.getName(), 1, 0, 4),
                taskFactory);
    }

    private static CoordinatorConfiguration configuration(Path jobSpec, Path progressDirectory, WorkerService worker) {
        return new CoordinatorConfiguration(
                jobSpec,
                progressDirectory,
                List.of(new WorkerEndpoint("worker-0", "127.0.0.1", worker.port())),
                Duration.ofSeconds(15),
                1,
                1);
    }

    private Path writeJob(String url) throws IOException {
        return writeJob(url, "at-least-once");
    }

    private Path writeJob(String url, String guarantee) throws IOException {
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
                      resumeColumn: ID
                      splitCount: '1'
                  sink:
                    connector: jdbc
                    options:
                      url: '%s'
                      table: TARGET_DATA
                  delivery:
                    guarantee: %s
                  runtime:
                    maxBatchRecords: 2
                """
                        .formatted(JOB_ID, yaml(url), yaml(url), guarantee),
                StandardCharsets.UTF_8);
        return path;
    }

    private static void initializeDatabase(String url) throws SQLException {
        execute(url, "CREATE TABLE SOURCE_DATA (ID INT PRIMARY KEY, NAME VARCHAR(40))");
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
        return "jdbc:h2:mem:checkpoint_" + UUID.randomUUID().toString().replace('-', '_')
                + ";MODE=PostgreSQL;DB_CLOSE_DELAY=-1";
    }

    private static String yaml(String value) {
        return value.replace("'", "''");
    }

    private static final class FailAfterFirstBatchSource implements CheckpointableBatchSource {
        private final CheckpointableBatchSource delegate;
        private int reads;

        private FailAfterFirstBatchSource(CheckpointableBatchSource delegate) {
            this.delegate = delegate;
        }

        @Override
        public void openAt(SplitPosition resumePosition) {
            delegate.openAt(resumePosition);
        }

        @Override
        public RowBatch readBatch(int maxRows) {
            if (reads++ > 0) {
                throw new IllegalStateException("planned failure after first checkpointed batch");
            }
            return delegate.readBatch(maxRows);
        }

        @Override
        public SplitPosition positionAfter(RowBatch batch) {
            return delegate.positionAfter(batch);
        }

        @Override
        public void close() {
            delegate.close();
        }
    }

    private static final class FailAfterCommitSink implements IdempotentBatchSink {
        private final IdempotentBatchSink delegate;
        private boolean failed;

        private FailAfterCommitSink(IdempotentBatchSink delegate) {
            this.delegate = delegate;
        }

        @Override
        public void open(CheckpointContext context) {
            delegate.open(context);
        }

        @Override
        public void assertEpoch(long executionEpoch) {
            delegate.assertEpoch(executionEpoch);
        }

        @Override
        public void writeBatch(RowBatch batch, SinkCommitContext commitContext) {
            delegate.writeBatch(batch, commitContext);
            if (!failed) {
                failed = true;
                throw new IllegalStateException("planned failure after exactly-once sink commit");
            }
        }

        @Override
        public String lastCommitToken() {
            return delegate.lastCommitToken();
        }

        @Override
        public void close() {
            delegate.close();
        }
    }
}
