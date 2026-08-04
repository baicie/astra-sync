package io.astrasync.engine.worker;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.network.WorkerClient;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.WorkerResult;
import java.time.Duration;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

class WorkerServiceTest {
    @Test
    void startsFromProviderExecutesTaskAndExposesTcpHealth() {
        Map<String, String> environment = Map.of(
                "ASTRASYNC_WORKER_ID", "worker-a",
                "ASTRASYNC_WORKER_PORT", "0",
                "ASTRASYNC_TASK_FACTORY_PROVIDER", TestProvider.class.getName(),
                "ASTRASYNC_MAX_CONCURRENT_TASKS", "1",
                "ASTRASYNC_TASK_QUEUE_CAPACITY", "1",
                "ASTRASYNC_MAX_CONNECTIONS", "4",
                "TEST_OPTION", "visible-to-provider");
        WorkerConfiguration configuration = WorkerConfiguration.fromEnvironment(environment);
        WorkerService service = WorkerApplication.createService(configuration, environment);

        service.start();
        int port = service.port();
        try {
            assertThat(WorkerHealthProbe.isHealthy("127.0.0.1", port, Duration.ofSeconds(2)))
                    .isTrue();
            WorkerResult result = new WorkerClient("127.0.0.1", port, Duration.ofSeconds(5))
                    .execute("worker-a", task(split("split-1")));
            assertThat(result.workerId()).isEqualTo("worker-a");
            assertThat(result.taskId()).isEqualTo("split-1");
            assertThat(TestProvider.environment.get()).containsEntry("TEST_OPTION", "visible-to-provider");
        } finally {
            service.close();
        }
        assertThat(WorkerHealthProbe.isHealthy("127.0.0.1", port, Duration.ofMillis(100)))
                .isFalse();
    }

    @Test
    void rejectsMissingInvalidAndUnusableConfiguration() {
        assertThatThrownBy(() -> WorkerConfiguration.fromEnvironment(Map.of()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("missing required environment variable ASTRASYNC_WORKER_ID");
        assertThatThrownBy(() -> WorkerConfiguration.fromEnvironment(Map.of(
                        "ASTRASYNC_WORKER_ID", "worker-a",
                        "ASTRASYNC_TASK_FACTORY_PROVIDER", TestProvider.class.getName(),
                        "ASTRASYNC_MAX_CONNECTIONS", "many")))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("environment variable ASTRASYNC_MAX_CONNECTIONS must be an integer");
        WorkerConfiguration unknownProvider = new WorkerConfiguration("worker-a", 0, "missing.Provider", 1, 0, 1);
        assertThatThrownBy(() -> WorkerApplication.createService(unknownProvider, Map.of()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("task factory provider class was not found: missing.Provider");
    }

    public static final class TestProvider implements WorkerTaskFactoryProvider {
        private static final AtomicReference<Map<String, String>> environment = new AtomicReference<>();

        public TestProvider() {}

        @Override
        public BatchTaskFactory create(Map<String, String> configuration) {
            environment.set(configuration);
            return split -> task(split);
        }
    }

    private static BatchTask task(SourceSplit split) {
        return new BatchTask(split, new EmptySource(), new EmptySink(), 2, 1);
    }

    private static SourceSplit split(String splitId) {
        return new SourceSplit(splitId, "test-source", new SplitPosition(Map.of("id", "1")), SplitPosition.unbounded());
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
