package io.astrasync.engine.runtime;

import io.astrasync.connector.api.data.RowBatch;
import java.time.Duration;
import java.util.Objects;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/** A bounded, failure-aware exchange for one Source/Sink pair. */
public final class BatchExchange {
    private static final Duration WAIT_INTERVAL = Duration.ofMillis(25);

    private final BlockingQueue<RowBatch> batches;
    private final AtomicReference<Throwable> failure = new AtomicReference<>();

    public BatchExchange(int capacity) {
        if (capacity <= 0) {
            throw new IllegalArgumentException("capacity must be positive");
        }
        this.batches = new ArrayBlockingQueue<>(capacity);
    }

    public int capacity() {
        return batches.remainingCapacity() + batches.size();
    }

    public int size() {
        return batches.size();
    }

    public void publish(RowBatch batch) {
        Objects.requireNonNull(batch, "batch must not be null");
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

    public RowBatch receive() {
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
    }

    private void throwIfFailed() {
        Throwable cause = failure.get();
        if (cause != null) {
            throw new ExchangeFailureException("batch exchange failed", cause);
        }
    }
}
