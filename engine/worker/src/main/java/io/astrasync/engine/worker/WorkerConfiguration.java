package io.astrasync.engine.worker;

import java.util.Map;
import java.util.Objects;

/** Validated process configuration for one Worker protocol endpoint. */
public record WorkerConfiguration(
        String workerId,
        int port,
        String taskFactoryProvider,
        int maxConcurrentTasks,
        int taskQueueCapacity,
        int maxConnections) {
    public static final int DEFAULT_PORT = 50_051;

    public WorkerConfiguration {
        workerId = requireText(workerId, "workerId");
        if (port < 0 || port > 65_535) {
            throw new IllegalArgumentException("port must be between 0 and 65535");
        }
        taskFactoryProvider = requireText(taskFactoryProvider, "taskFactoryProvider");
        if (maxConcurrentTasks <= 0) {
            throw new IllegalArgumentException("maxConcurrentTasks must be positive");
        }
        if (taskQueueCapacity < 0) {
            throw new IllegalArgumentException("taskQueueCapacity must not be negative");
        }
        if (maxConnections <= 0) {
            throw new IllegalArgumentException("maxConnections must be positive");
        }
    }

    public static WorkerConfiguration fromEnvironment(Map<String, String> environment) {
        Objects.requireNonNull(environment, "environment must not be null");
        return new WorkerConfiguration(
                required(environment, "ASTRASYNC_WORKER_ID"),
                integer(environment, "ASTRASYNC_WORKER_PORT", DEFAULT_PORT),
                required(environment, "ASTRASYNC_TASK_FACTORY_PROVIDER"),
                integer(environment, "ASTRASYNC_MAX_CONCURRENT_TASKS", 1),
                integer(environment, "ASTRASYNC_TASK_QUEUE_CAPACITY", 0),
                integer(environment, "ASTRASYNC_MAX_CONNECTIONS", 16));
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

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
