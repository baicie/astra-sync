package io.astrasync.engine.coordinator;

import java.net.URI;
import java.time.Duration;
import java.util.Objects;
import java.util.UUID;

/** Authenticated control-plane heartbeat configuration for one execution epoch. */
public record ExecutionHeartbeatConfiguration(URI endpoint, String token, Duration interval) {
    public ExecutionHeartbeatConfiguration {
        endpoint = Objects.requireNonNull(endpoint, "endpoint must not be null");
        String scheme = endpoint.getScheme();
        if (!("http".equalsIgnoreCase(scheme) || "https".equalsIgnoreCase(scheme))) {
            throw new IllegalArgumentException("heartbeat endpoint must use HTTP or HTTPS");
        }
        if (endpoint.getHost() == null) {
            throw new IllegalArgumentException("heartbeat endpoint must include a host");
        }
        if (token == null || token.isBlank()) {
            throw new IllegalArgumentException("heartbeat token must not be blank");
        }
        try {
            UUID.fromString(token);
        } catch (IllegalArgumentException exception) {
            throw new IllegalArgumentException("heartbeat token must be a UUID", exception);
        }
        interval = Objects.requireNonNull(interval, "interval must not be null");
        if (interval.isZero() || interval.isNegative() || interval.toMillis() > Integer.MAX_VALUE) {
            throw new IllegalArgumentException("heartbeat interval must be positive and fit in milliseconds");
        }
    }
}
