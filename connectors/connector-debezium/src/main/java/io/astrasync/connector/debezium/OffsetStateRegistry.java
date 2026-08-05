package io.astrasync.connector.debezium;

import io.astrasync.connector.api.source.SplitPosition;
import java.nio.ByteBuffer;
import java.time.Duration;
import java.util.Map;
import java.util.Objects;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;

final class OffsetStateRegistry {
    private static final Map<String, State> STATES = new ConcurrentHashMap<>();

    private OffsetStateRegistry() {}

    static Handle register(String connectorIdentity, SplitPosition resumePosition) {
        State state = new State(connectorIdentity, OffsetStateCodec.decode(connectorIdentity, resumePosition));
        String id = UUID.randomUUID().toString();
        if (STATES.putIfAbsent(id, state) != null) {
            throw new IllegalStateException("duplicate Debezium offset state id");
        }
        return new Handle(id, state);
    }

    static State require(String id) {
        State state = STATES.get(id);
        if (state == null) {
            throw new IllegalStateException("Debezium offset state is not registered: " + id);
        }
        return state;
    }

    static final class Handle implements AutoCloseable {
        private final String id;
        private final State state;
        private boolean closed;

        private Handle(String id, State state) {
            this.id = id;
            this.state = state;
        }

        String id() {
            return id;
        }

        long revision() {
            return state.revision();
        }

        SplitPosition awaitPositionAfter(long revision, Duration timeout) {
            return state.awaitPositionAfter(revision, timeout);
        }

        @Override
        public synchronized void close() {
            if (!closed) {
                closed = true;
                STATES.remove(id, state);
                state.close();
            }
        }
    }

    static final class State {
        private final String connectorIdentity;
        private Map<ByteBuffer, ByteBuffer> offsets;
        private long revision;
        private boolean closed;

        private State(String connectorIdentity, Map<ByteBuffer, ByteBuffer> offsets) {
            this.connectorIdentity = connectorIdentity;
            this.offsets = OffsetStateCodec.copy(offsets);
        }

        synchronized Map<ByteBuffer, ByteBuffer> initialOffsets() {
            return OffsetStateCodec.copy(offsets);
        }

        synchronized void saved(Map<ByteBuffer, ByteBuffer> updatedOffsets) {
            if (!closed) {
                offsets = OffsetStateCodec.copy(updatedOffsets);
                revision++;
                notifyAll();
            }
        }

        synchronized long revision() {
            return revision;
        }

        synchronized SplitPosition awaitPositionAfter(long previousRevision, Duration timeout) {
            Objects.requireNonNull(timeout, "timeout must not be null");
            if (timeout.isNegative() || timeout.isZero()) {
                throw new IllegalArgumentException("timeout must be positive");
            }
            long timeoutNanos = timeout.toNanos();
            long deadline = System.nanoTime() + timeoutNanos;
            while (revision <= previousRevision && !closed) {
                long remaining = deadline - System.nanoTime();
                if (remaining <= 0) {
                    throw new IllegalStateException("timed out waiting for Debezium to persist an acknowledged offset");
                }
                try {
                    long millis =
                            Math.max(1, Math.min(Duration.ofNanos(remaining).toMillis(), 1_000));
                    wait(millis);
                } catch (InterruptedException exception) {
                    Thread.currentThread().interrupt();
                    throw new IllegalStateException(
                            "interrupted while waiting for Debezium offset persistence", exception);
                }
            }
            if (closed) {
                throw new IllegalStateException("Debezium offset state closed before acknowledgement completed");
            }
            return OffsetStateCodec.encode(connectorIdentity, offsets);
        }

        synchronized void close() {
            closed = true;
            notifyAll();
        }
    }
}
