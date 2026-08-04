package io.astrasync.engine.worker;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.time.Duration;
import java.util.Map;
import java.util.Objects;

/** Process-level TCP health probe for the Worker protocol listener. */
public final class WorkerHealthProbe {
    private static final Duration DEFAULT_TIMEOUT = Duration.ofSeconds(2);

    private WorkerHealthProbe() {}

    public static void main(String[] args) {
        Map<String, String> environment = System.getenv();
        String host = environment.getOrDefault("ASTRASYNC_WORKER_HEALTH_HOST", "127.0.0.1");
        int port = parsePort(
                environment.getOrDefault("ASTRASYNC_WORKER_PORT", Integer.toString(WorkerConfiguration.DEFAULT_PORT)));
        System.exit(isHealthy(host, port, DEFAULT_TIMEOUT) ? 0 : 1);
    }

    public static boolean isHealthy(String host, int port, Duration timeout) {
        Objects.requireNonNull(host, "host must not be null");
        Objects.requireNonNull(timeout, "timeout must not be null");
        long timeoutMillis = timeout.toMillis();
        if (host.isBlank() || port <= 0 || port > 65_535 || timeoutMillis <= 0 || timeoutMillis > Integer.MAX_VALUE) {
            throw new IllegalArgumentException("health probe host, port, and timeout must be valid");
        }
        try (Socket socket = new Socket()) {
            socket.connect(new InetSocketAddress(host, port), (int) timeoutMillis);
            return true;
        } catch (IOException exception) {
            return false;
        }
    }

    private static int parsePort(String value) {
        try {
            return Integer.parseInt(value);
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("ASTRASYNC_WORKER_PORT must be an integer", exception);
        }
    }
}
