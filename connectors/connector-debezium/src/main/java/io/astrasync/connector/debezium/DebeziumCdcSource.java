package io.astrasync.connector.debezium;

import io.astrasync.connector.api.data.CdcBatch;
import io.astrasync.connector.api.data.CdcPhase;
import io.astrasync.connector.api.data.DataEvent;
import io.astrasync.connector.api.source.CdcSource;
import io.astrasync.connector.api.source.SplitPosition;
import io.debezium.embedded.Connect;
import io.debezium.engine.DebeziumEngine;
import io.debezium.engine.RecordChangeEvent;
import io.debezium.engine.format.ChangeEventFormat;
import io.debezium.engine.spi.OffsetCommitPolicy;
import java.io.IOException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Objects;
import java.util.Optional;
import java.util.Properties;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import org.apache.kafka.connect.source.SourceRecord;

/** Embedded Debezium source whose offsets advance only after an AstraSync acknowledgement. */
public final class DebeziumCdcSource implements CdcSource {
    private final String connectorIdentity;
    private final Properties connectorProperties;
    private final Duration commitTimeout;
    private final DebeziumRecordConverter converter;
    private final ArrayBlockingQueue<PendingBatch> batches;
    private final AtomicLong sequence = new AtomicLong();
    private final AtomicReference<Throwable> terminalFailure = new AtomicReference<>();

    private State state = State.NEW;
    private OffsetStateRegistry.Handle offsetState;
    private DebeziumEngine<RecordChangeEvent<SourceRecord>> engine;
    private ExecutorService engineExecutor;
    private PendingBatch outstanding;

    public DebeziumCdcSource(
            String connectorIdentity, Properties connectorProperties, int queuedBatches, Duration commitTimeout) {
        this(connectorIdentity, connectorProperties, queuedBatches, commitTimeout, new DebeziumRecordConverter());
    }

    DebeziumCdcSource(
            String connectorIdentity,
            Properties connectorProperties,
            int queuedBatches,
            Duration commitTimeout,
            DebeziumRecordConverter converter) {
        this.connectorIdentity = requireText(connectorIdentity, "connectorIdentity");
        Objects.requireNonNull(connectorProperties, "connectorProperties must not be null");
        this.connectorProperties = new Properties();
        this.connectorProperties.putAll(connectorProperties);
        if (queuedBatches <= 0) {
            throw new IllegalArgumentException("queuedBatches must be positive");
        }
        this.batches = new ArrayBlockingQueue<>(queuedBatches);
        this.commitTimeout = requirePositive(commitTimeout, "commitTimeout");
        this.converter = Objects.requireNonNull(converter, "converter must not be null");
    }

    @Override
    public synchronized void openAt(SplitPosition resumePosition) {
        requireState(State.NEW, "open");
        offsetState = OffsetStateRegistry.register(
                connectorIdentity, Objects.requireNonNull(resumePosition, "resumePosition must not be null"));
        Properties properties = new Properties();
        properties.putAll(connectorProperties);
        properties.setProperty("offset.storage", AstraOffsetBackingStore.class.getName());
        properties.setProperty(AstraOffsetBackingStore.STATE_ID_PROPERTY, offsetState.id());
        properties.setProperty(DebeziumEngine.OFFSET_FLUSH_INTERVAL_MS_PROP, "0");
        try {
            engine = DebeziumEngine.create(ChangeEventFormat.of(Connect.class))
                    .using(properties)
                    .using(OffsetCommitPolicy.always())
                    .notifying(this::handleBatch)
                    .using(this::completed)
                    .build();
            engineExecutor = Executors.newSingleThreadExecutor(runnable -> {
                Thread thread = new Thread(runnable, "astrasync-debezium-" + connectorIdentity);
                thread.setDaemon(true);
                return thread;
            });
            state = State.OPEN;
            engineExecutor.submit(engine);
        } catch (RuntimeException exception) {
            state = State.CLOSED;
            offsetState.close();
            throw new IllegalStateException("failed to start Debezium source " + connectorIdentity, exception);
        }
    }

    @Override
    public Optional<CdcBatch> poll(Duration timeout) {
        requireOpen("poll");
        requirePositive(timeout, "timeout");
        synchronized (this) {
            if (outstanding != null) {
                throw new IllegalStateException("acknowledge the current CDC batch before polling again");
            }
        }
        try {
            PendingBatch pending = batches.poll(timeout.toNanos(), TimeUnit.NANOSECONDS);
            if (pending == null) {
                throwIfFailed();
                return Optional.empty();
            }
            synchronized (this) {
                requireState(State.OPEN, "poll");
                outstanding = pending;
            }
            return Optional.of(pending.batch());
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("interrupted while polling Debezium", exception);
        }
    }

    @Override
    public SplitPosition acknowledge(CdcBatch batch) {
        Objects.requireNonNull(batch, "batch must not be null");
        PendingBatch pending;
        synchronized (this) {
            requireState(State.OPEN, "acknowledge");
            pending = outstanding;
            if (pending == null || pending.batch() != batch) {
                throw new IllegalArgumentException("CDC batch is not the current delivered batch");
            }
        }
        pending.acknowledged().complete(true);
        try {
            SplitPosition position = pending.position().get(commitTimeout.toNanos(), TimeUnit.NANOSECONDS);
            synchronized (this) {
                outstanding = null;
            }
            return position;
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("interrupted while acknowledging Debezium batch", exception);
        } catch (ExecutionException exception) {
            throw new IllegalStateException("Debezium failed to persist the acknowledged batch", exception.getCause());
        } catch (TimeoutException exception) {
            throw new IllegalStateException("timed out acknowledging Debezium batch", exception);
        }
    }

    @Override
    public void close() {
        DebeziumEngine<RecordChangeEvent<SourceRecord>> openedEngine;
        ExecutorService openedExecutor;
        OffsetStateRegistry.Handle openedOffsetState;
        synchronized (this) {
            if (state == State.CLOSED) {
                return;
            }
            state = State.CLOSED;
            openedEngine = engine;
            openedExecutor = engineExecutor;
            openedOffsetState = offsetState;
            if (outstanding != null) {
                outstanding.acknowledged().complete(false);
                outstanding.position().completeExceptionally(new IllegalStateException("CDC source closed"));
                outstanding = null;
            }
            PendingBatch queued;
            while ((queued = batches.poll()) != null) {
                queued.acknowledged().complete(false);
                queued.position().completeExceptionally(new IllegalStateException("CDC source closed"));
            }
        }

        RuntimeException failure = null;
        if (openedEngine != null) {
            try {
                openedEngine.close();
            } catch (IOException exception) {
                failure = new IllegalStateException("failed to close Debezium engine", exception);
            }
        }
        if (openedExecutor != null) {
            openedExecutor.shutdownNow();
            try {
                if (!openedExecutor.awaitTermination(10, TimeUnit.SECONDS)) {
                    failure = addFailure(failure, new IllegalStateException("Debezium engine did not stop"));
                }
            } catch (InterruptedException exception) {
                Thread.currentThread().interrupt();
                failure = addFailure(
                        failure, new IllegalStateException("interrupted while stopping Debezium", exception));
            }
        }
        if (openedOffsetState != null) {
            openedOffsetState.close();
        }
        if (failure != null) {
            throw failure;
        }
    }

    private void handleBatch(
            List<RecordChangeEvent<SourceRecord>> records,
            DebeziumEngine.RecordCommitter<RecordChangeEvent<SourceRecord>> committer)
            throws InterruptedException {
        List<DataEvent> events = new ArrayList<>(records.size());
        for (RecordChangeEvent<SourceRecord> record : records) {
            converter.convert(record.record()).ifPresent(events::add);
        }
        if (events.isEmpty()) {
            markProcessed(records, committer);
            return;
        }

        long revision = offsetState.revision();
        CdcBatch batch = new CdcBatch(sequence.incrementAndGet(), events, phase(events), snapshotCompleted(events));
        PendingBatch pending = new PendingBatch(batch, revision, new CompletableFuture<>(), new CompletableFuture<>());
        batches.put(pending);
        try {
            if (!pending.acknowledged().get()) {
                return;
            }
            markProcessed(records, committer);
            pending.position().complete(offsetState.awaitPositionAfter(revision, commitTimeout));
        } catch (ExecutionException exception) {
            Throwable cause = exception.getCause();
            pending.position().completeExceptionally(cause);
            throw new IllegalStateException("CDC acknowledgement failed", cause);
        } catch (RuntimeException exception) {
            pending.position().completeExceptionally(exception);
            throw exception;
        }
    }

    private static void markProcessed(
            List<RecordChangeEvent<SourceRecord>> records,
            DebeziumEngine.RecordCommitter<RecordChangeEvent<SourceRecord>> committer)
            throws InterruptedException {
        for (RecordChangeEvent<SourceRecord> record : records) {
            committer.markProcessed(record);
        }
        committer.markBatchFinished();
    }

    private void completed(boolean success, String message, Throwable error) {
        if (!success && state == State.OPEN) {
            Throwable failure = error == null ? new IllegalStateException(message) : error;
            terminalFailure.compareAndSet(null, failure);
            PendingBatch pending = batches.peek();
            if (pending != null) {
                pending.position().completeExceptionally(failure);
            }
        }
    }

    private void throwIfFailed() {
        Throwable failure = terminalFailure.get();
        if (failure != null) {
            throw new IllegalStateException("Debezium source terminated", failure);
        }
    }

    private synchronized void requireOpen(String operation) {
        requireState(State.OPEN, operation);
        throwIfFailed();
    }

    private void requireState(State expected, String operation) {
        if (state != expected) {
            throw new IllegalStateException("cannot " + operation + " Debezium source while state is " + state);
        }
    }

    private static CdcPhase phase(List<DataEvent> events) {
        boolean snapshot = events.stream().anyMatch(event -> event.getOperation() == DataEvent.Operation.SNAPSHOT);
        boolean streaming = events.stream().anyMatch(event -> event.getOperation() != DataEvent.Operation.SNAPSHOT);
        if (snapshot && streaming) {
            return CdcPhase.HANDOFF;
        }
        return snapshot ? CdcPhase.SNAPSHOT : CdcPhase.STREAMING;
    }

    private static boolean snapshotCompleted(List<DataEvent> events) {
        return events.stream()
                .map(DataEvent::getHeaders)
                .map(headers -> headers.get("source.snapshot"))
                .anyMatch(value -> "last".equalsIgnoreCase(value));
    }

    private static Duration requirePositive(Duration duration, String name) {
        Objects.requireNonNull(duration, name + " must not be null");
        if (duration.isZero() || duration.isNegative()) {
            throw new IllegalArgumentException(name + " must be positive");
        }
        return duration;
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }

    private static RuntimeException addFailure(RuntimeException existing, RuntimeException next) {
        if (existing == null) {
            return next;
        }
        existing.addSuppressed(next);
        return existing;
    }

    private record PendingBatch(
            CdcBatch batch,
            long offsetRevision,
            CompletableFuture<Boolean> acknowledged,
            CompletableFuture<SplitPosition> position) {}

    private enum State {
        NEW,
        OPEN,
        CLOSED
    }
}
