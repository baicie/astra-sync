package io.astrasync.engine.coordinator;

import java.io.IOException;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.Optional;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/** Sends authenticated liveness while the Coordinator owns an execution. */
final class ExecutionHeartbeat implements AutoCloseable {
    private static final Logger LOG = LoggerFactory.getLogger(ExecutionHeartbeat.class);

    private final ExecutionHeartbeatConfiguration configuration;
    private final HttpClient client;
    private final ScheduledExecutorService executor;

    private ExecutionHeartbeat(ExecutionHeartbeatConfiguration configuration) {
        this.configuration = configuration;
        this.client = HttpClient.newBuilder()
                .connectTimeout(requestTimeout(configuration.interval()))
                .build();
        this.executor = Executors.newSingleThreadScheduledExecutor(runnable -> {
            Thread thread = new Thread(runnable, "astrasync-execution-heartbeat");
            thread.setDaemon(true);
            return thread;
        });
    }

    static ExecutionHeartbeat start(Optional<ExecutionHeartbeatConfiguration> configuration) {
        if (configuration.isEmpty()) {
            return new ExecutionHeartbeat();
        }
        ExecutionHeartbeat heartbeat = new ExecutionHeartbeat(configuration.orElseThrow());
        try {
            heartbeat.send();
        } catch (RuntimeException exception) {
            heartbeat.close();
            throw exception;
        }
        heartbeat.executor.scheduleWithFixedDelay(
                heartbeat::sendSafely,
                heartbeat.configuration.interval().toMillis(),
                heartbeat.configuration.interval().toMillis(),
                TimeUnit.MILLISECONDS);
        return heartbeat;
    }

    private ExecutionHeartbeat() {
        this.configuration = null;
        this.client = null;
        this.executor = null;
    }

    private void sendSafely() {
        try {
            send();
        } catch (RuntimeException exception) {
            LOG.error("execution heartbeat failed", exception);
        }
    }

    private void send() {
        HttpRequest request = HttpRequest.newBuilder(configuration.endpoint())
                .timeout(requestTimeout(configuration.interval()))
                .header("Authorization", "Bearer " + configuration.token())
                .POST(HttpRequest.BodyPublishers.noBody())
                .build();
        try {
            HttpResponse<Void> response = client.send(request, HttpResponse.BodyHandlers.discarding());
            if (response.statusCode() < 200 || response.statusCode() >= 300) {
                throw new IllegalStateException("control plane returned HTTP " + response.statusCode());
            }
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("heartbeat was interrupted", exception);
        } catch (IOException exception) {
            throw new IllegalStateException("control plane is unavailable", exception);
        }
    }

    private static Duration requestTimeout(Duration interval) {
        if (interval.compareTo(Duration.ofSeconds(1)) < 0) {
            return Duration.ofSeconds(1);
        }
        return interval.compareTo(Duration.ofSeconds(5)) < 0 ? interval : Duration.ofSeconds(5);
    }

    @Override
    public void close() {
        if (executor != null) {
            executor.shutdownNow();
        }
    }
}
