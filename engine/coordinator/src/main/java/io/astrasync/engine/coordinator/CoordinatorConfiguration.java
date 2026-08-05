package io.astrasync.engine.coordinator;

import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;

/** Validated process configuration for the operational Coordinator entry point. */
public record CoordinatorConfiguration(
        Path jobSpecPath,
        Path progressDirectory,
        List<WorkerEndpoint> workers,
        Duration workerTimeout,
        int maxInFlightTasks,
        int maxInFlightBatches,
        long executionEpoch) {
    public static final int DEFAULT_WORKER_TIMEOUT_MILLIS = 30_000;
    public static final int DEFAULT_MAX_IN_FLIGHT_TASKS = 1;
    public static final int DEFAULT_MAX_IN_FLIGHT_BATCHES = 1;

    public CoordinatorConfiguration(
            Path jobSpecPath,
            Path progressDirectory,
            List<WorkerEndpoint> workers,
            Duration workerTimeout,
            int maxInFlightTasks,
            int maxInFlightBatches) {
        this(jobSpecPath, progressDirectory, workers, workerTimeout, maxInFlightTasks, maxInFlightBatches, 0);
    }

    public CoordinatorConfiguration {
        jobSpecPath = normalizePath(jobSpecPath, "jobSpecPath");
        progressDirectory = normalizePath(progressDirectory, "progressDirectory");
        Objects.requireNonNull(workers, "workers must not be null");
        if (workers.isEmpty()) {
            throw new IllegalArgumentException("at least one Worker endpoint is required");
        }
        List<WorkerEndpoint> copy = new ArrayList<>(workers.size());
        Set<String> workerIds = new HashSet<>();
        for (WorkerEndpoint worker : workers) {
            WorkerEndpoint checked = Objects.requireNonNull(worker, "workers must not contain null");
            if (!workerIds.add(checked.workerId())) {
                throw new IllegalArgumentException("Worker endpoint ID is duplicated: " + checked.workerId());
            }
            copy.add(checked);
        }
        workers = List.copyOf(copy);
        workerTimeout = Objects.requireNonNull(workerTimeout, "workerTimeout must not be null");
        long timeoutMillis = workerTimeout.toMillis();
        if (timeoutMillis <= 0 || timeoutMillis > Integer.MAX_VALUE) {
            throw new IllegalArgumentException("workerTimeout must be positive and fit in milliseconds");
        }
        if (maxInFlightTasks <= 0) {
            throw new IllegalArgumentException("maxInFlightTasks must be positive");
        }
        if (maxInFlightBatches <= 0) {
            throw new IllegalArgumentException("maxInFlightBatches must be positive");
        }
        if (executionEpoch < 0) {
            throw new IllegalArgumentException("executionEpoch must not be negative");
        }
    }

    public static CoordinatorConfiguration fromEnvironment(Map<String, String> environment) {
        Objects.requireNonNull(environment, "environment must not be null");
        return new CoordinatorConfiguration(
                Path.of(required(environment, "ASTRASYNC_COORDINATOR_JOB_SPEC")),
                Path.of(required(environment, "ASTRASYNC_COORDINATOR_PROGRESS_DIR")),
                parseWorkers(required(environment, "ASTRASYNC_COORDINATOR_WORKERS")),
                Duration.ofMillis(
                        integer(environment, "ASTRASYNC_COORDINATOR_WORKER_TIMEOUT_MS", DEFAULT_WORKER_TIMEOUT_MILLIS)),
                integer(environment, "ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_TASKS", DEFAULT_MAX_IN_FLIGHT_TASKS),
                integer(environment, "ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_BATCHES", DEFAULT_MAX_IN_FLIGHT_BATCHES),
                optionalPositiveLong(environment, "ASTRASYNC_COORDINATOR_EXECUTION_EPOCH"));
    }

    private static List<WorkerEndpoint> parseWorkers(String value) {
        String[] values = value.split(",", -1);
        List<WorkerEndpoint> endpoints = new ArrayList<>(values.length);
        for (String item : values) {
            if (item.isBlank()) {
                throw new IllegalArgumentException("Worker endpoint list contains a blank entry");
            }
            endpoints.add(WorkerEndpoint.parse(item.trim()));
        }
        return endpoints;
    }

    private static String required(Map<String, String> environment, String name) {
        String value = environment.get(name);
        if (value == null || value.isBlank()) {
            throw new IllegalArgumentException("missing required environment variable " + name);
        }
        return value;
    }

    private static int integer(Map<String, String> environment, String name, int defaultValue) {
        String value = environment.get(name);
        if (value == null) {
            return defaultValue;
        }
        try {
            return Integer.parseInt(value);
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("environment variable " + name + " must be an integer", exception);
        }
    }

    private static long optionalPositiveLong(Map<String, String> environment, String name) {
        String value = environment.get(name);
        if (value == null) {
            return 0;
        }
        try {
            long parsed = Long.parseLong(value);
            if (parsed <= 0) {
                throw new IllegalArgumentException("environment variable " + name + " must be positive");
            }
            return parsed;
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("environment variable " + name + " must be an integer", exception);
        }
    }

    private static Path normalizePath(Path value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        return value.toAbsolutePath().normalize();
    }
}
