package io.astrasync.engine.coordinator;

import java.util.Objects;

/** Stable identity and network location of one remote Worker. */
public record WorkerEndpoint(String workerId, String host, int port) {
    public WorkerEndpoint {
        workerId = requireText(workerId, "workerId");
        host = requireText(host, "host");
        if (port <= 0 || port > 65_535) {
            throw new IllegalArgumentException("port must be between 1 and 65535");
        }
    }

    /** Parses the deployment form {@code worker-id@host:port}. */
    public static WorkerEndpoint parse(String value) {
        Objects.requireNonNull(value, "endpoint must not be null");
        int separator = value.indexOf('@');
        if (separator <= 0 || separator != value.lastIndexOf('@')) {
            throw new IllegalArgumentException("Worker endpoint must use worker-id@host:port");
        }
        String workerId = value.substring(0, separator);
        String address = value.substring(separator + 1);
        int portSeparator = address.lastIndexOf(':');
        if (portSeparator <= 0 || portSeparator == address.length() - 1 || address.indexOf(':') != portSeparator) {
            throw new IllegalArgumentException("Worker endpoint must use worker-id@host:port");
        }
        int port;
        try {
            port = Integer.parseInt(address.substring(portSeparator + 1));
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("Worker endpoint port must be an integer", exception);
        }
        return new WorkerEndpoint(workerId, address.substring(0, portSeparator), port);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
