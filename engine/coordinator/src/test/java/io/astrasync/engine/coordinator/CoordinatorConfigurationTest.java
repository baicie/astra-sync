package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class CoordinatorConfigurationTest {
    @TempDir
    Path tempDirectory;

    @Test
    void parsesWorkerEndpointsAndDefaultsFromEnvironment() {
        Path jobSpec = tempDirectory.resolve("job.yaml");
        Path progress = tempDirectory.resolve("progress");

        CoordinatorConfiguration configuration = CoordinatorConfiguration.fromEnvironment(Map.of(
                "ASTRASYNC_COORDINATOR_JOB_SPEC", jobSpec.toString(),
                "ASTRASYNC_COORDINATOR_PROGRESS_DIR", progress.toString(),
                "ASTRASYNC_COORDINATOR_WORKERS", "worker-0@127.0.0.1:50051, worker-1@worker-1.worker:50052"));

        assertThat(configuration.jobSpecPath())
                .isEqualTo(jobSpec.toAbsolutePath().normalize());
        assertThat(configuration.progressDirectory())
                .isEqualTo(progress.toAbsolutePath().normalize());
        assertThat(configuration.workers())
                .containsExactly(
                        new WorkerEndpoint("worker-0", "127.0.0.1", 50051),
                        new WorkerEndpoint("worker-1", "worker-1.worker", 50052));
        assertThat(configuration.workerTimeout()).isEqualTo(Duration.ofSeconds(30));
        assertThat(configuration.maxInFlightTasks()).isEqualTo(1);
        assertThat(configuration.maxInFlightBatches()).isEqualTo(1);
        assertThat(configuration.executionEpoch()).isZero();
    }

    @Test
    void parsesExplicitCapacityAndTimeout() {
        Map<String, String> environment = requiredEnvironment();
        environment.put("ASTRASYNC_COORDINATOR_WORKER_TIMEOUT_MS", "2500");
        environment.put("ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_TASKS", "2");
        environment.put("ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_BATCHES", "4");
        environment.put("ASTRASYNC_COORDINATOR_EXECUTION_EPOCH", "17");

        CoordinatorConfiguration configuration = CoordinatorConfiguration.fromEnvironment(environment);

        assertThat(configuration.workerTimeout()).isEqualTo(Duration.ofMillis(2500));
        assertThat(configuration.maxInFlightTasks()).isEqualTo(2);
        assertThat(configuration.maxInFlightBatches()).isEqualTo(4);
        assertThat(configuration.executionEpoch()).isEqualTo(17);
    }

    @Test
    void rejectsDuplicateWorkerIdsAndMalformedEndpoints() {
        assertThatThrownBy(() -> new CoordinatorConfiguration(
                        tempDirectory.resolve("job.yaml"),
                        tempDirectory.resolve("progress"),
                        List.of(
                                new WorkerEndpoint("worker-0", "host-a", 50051),
                                new WorkerEndpoint("worker-0", "host-b", 50052)),
                        Duration.ofSeconds(1),
                        1,
                        1))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("Worker endpoint ID is duplicated: worker-0");
        assertThatThrownBy(() -> WorkerEndpoint.parse("worker-0:50051"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("Worker endpoint must use worker-id@host:port");
        assertThatThrownBy(() -> WorkerEndpoint.parse("worker-0@host:many"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("Worker endpoint port must be an integer");
        assertThatThrownBy(() -> WorkerEndpoint.parse("worker-0@host:65536"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("port must be between 1 and 65535");
    }

    @Test
    void rejectsMissingBlankAndInvalidEnvironmentValues() {
        assertThatThrownBy(() -> CoordinatorConfiguration.fromEnvironment(Map.of()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("missing required environment variable ASTRASYNC_COORDINATOR_JOB_SPEC");

        Map<String, String> blankWorker = requiredEnvironment();
        blankWorker.put("ASTRASYNC_COORDINATOR_WORKERS", "worker-0@host:50051,");
        assertThatThrownBy(() -> CoordinatorConfiguration.fromEnvironment(blankWorker))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("Worker endpoint list contains a blank entry");

        Map<String, String> invalidInteger = requiredEnvironment();
        invalidInteger.put("ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_TASKS", "many");
        assertThatThrownBy(() -> CoordinatorConfiguration.fromEnvironment(invalidInteger))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("environment variable ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_TASKS must be an integer");

        Map<String, String> invalidTimeout = requiredEnvironment();
        invalidTimeout.put("ASTRASYNC_COORDINATOR_WORKER_TIMEOUT_MS", "0");
        assertThatThrownBy(() -> CoordinatorConfiguration.fromEnvironment(invalidTimeout))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("workerTimeout must be positive and fit in milliseconds");

        Map<String, String> invalidEpoch = requiredEnvironment();
        invalidEpoch.put("ASTRASYNC_COORDINATOR_EXECUTION_EPOCH", "0");
        assertThatThrownBy(() -> CoordinatorConfiguration.fromEnvironment(invalidEpoch))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("environment variable ASTRASYNC_COORDINATOR_EXECUTION_EPOCH must be positive");
    }

    private Map<String, String> requiredEnvironment() {
        return new java.util.HashMap<>(Map.of(
                "ASTRASYNC_COORDINATOR_JOB_SPEC",
                        tempDirectory.resolve("job.yaml").toString(),
                "ASTRASYNC_COORDINATOR_PROGRESS_DIR",
                        tempDirectory.resolve("progress").toString(),
                "ASTRASYNC_COORDINATOR_WORKERS", "worker-0@127.0.0.1:50051"));
    }
}
