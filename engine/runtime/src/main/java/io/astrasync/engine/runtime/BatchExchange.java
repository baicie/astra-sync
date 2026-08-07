package io.astrasync.engine.runtime;

import io.astrasync.connector.api.data.RowBatch;
import java.time.Duration;
import java.util.Objects;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/** A bounded, failure-aware exchange for one Source/Sink pair. */
public final class BatchExchange implements AutoCloseable {
    private static final Duration WAIT_INTERVAL = Duration.ofMillis(25);

    private final BlockingQueue<RowBatch> batches;
    private final SpillableBatchStorage spillStorage;
    private final AtomicReference<Throwable> failure = new AtomicReference<>();

    public BatchExchange(int capacity) {
        this(capacity, SpillPolicy.disabled());
    }

    public BatchExchange(int capacity, SpillPolicy spillPolicy) {
        if (capacity <= 0) {
            throw new IllegalArgumentException("capacity must be positive");
        }
        SpillPolicy checked = Objects.requireNonNull(spillPolicy, "spillPolicy must not be null");
        if (checked.enabled()) {
            this.batches = null;
            this.spillStorage = new SpillableBatchStorage(capacity, checked);
        } else {
            this.batches = new ArrayBlockingQueue<>(capacity);
            this.spillStorage = null;
        }
    }

    public int capacity() {
        return spillStorage == null ? batches.remainingCapacity() + batches.size() : spillStorage.capacity();
    }

    public int size() {
        return spillStorage == null ? batches.size() : spillStorage.size();
    }

    public void publish(RowBatch batch) {
        Objects.requireNonNull(batch, "batch must not be null");
        if (spillStorage != null) {
            spillStorage.publish(batch, failure::get);
            return;
        }
        while (true) {
            throwIfFailed();
            try {
                if (batches.offer(batch, WAIT_INTERVAL.toMillis(), TimeUnit.MILLISECONDS)) {
                    return;
                }
            } catch (InterruptedException exception) {
                Thread.currentThread().interrupt();
                throw new ExchangeFailureException("publisher interrupted", exception);
            }
        }
    }

    /** Publishes a batch and returns the time spent waiting for exchange capacity. */
    public long publishMeasured(RowBatch batch) {
        long startedNanos = System.nanoTime();
        publish(batch);
        return Math.max(0, System.nanoTime() - startedNanos);
    }

    public RowBatch receive() {
        if (spillStorage != null) {
            return spillStorage.receive(failure::get);
        }
        while (true) {
            try {
                RowBatch batch = batches.poll(WAIT_INTERVAL.toMillis(), TimeUnit.MILLISECONDS);
                if (batch != null) {
                    return batch;
                }
            } catch (InterruptedException exception) {
                Thread.currentThread().interrupt();
                throw new ExchangeFailureException("receiver interrupted", exception);
            }
            throwIfFailed();
        }
    }

    public void fail(Throwable cause) {
        failure.compareAndSet(null, Objects.requireNonNull(cause, "cause must not be null"));
        if (spillStorage != null) {
            spillStorage.fail();
        }
    }

    @Override
    public void close() {
        if (spillStorage != null) {
            spillStorage.close();
        }
    }

    private void throwIfFailed() {
        Throwable cause = failure.get();
        if (cause != null) {
            throw new ExchangeFailureException("batch exchange failed", cause);
        }
    }
}
