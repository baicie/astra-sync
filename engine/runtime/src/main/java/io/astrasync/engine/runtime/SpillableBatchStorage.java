package io.astrasync.engine.runtime;

import io.astrasync.connector.api.data.RowBatch;
import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.channels.FileChannel;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;
import java.util.ArrayList;
import java.util.Objects;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.Semaphore;
import java.util.concurrent.TimeUnit;
import java.util.function.Supplier;

/** Bounded file-backed storage for batches that are waiting in an exchange. */
final class SpillableBatchStorage implements AutoCloseable {
    private static final long WAIT_MILLIS = 25;

    private final ArrayBlockingQueue<Path> files;
    private final Semaphore slots;
    private final Path directory;
    private final long maxBytes;
    private final Object bytesMonitor = new Object();
    private long reservedBytes;
    private boolean closed;

    SpillableBatchStorage(int capacity, SpillPolicy policy) {
        if (capacity <= 0) {
            throw new IllegalArgumentException("capacity must be positive");
        }
        SpillPolicy checked = Objects.requireNonNull(policy, "spill policy must not be null");
        if (!checked.enabled()) {
            throw new IllegalArgumentException("spill policy must be enabled");
        }
        int queueCapacity = Math.min(capacity, checked.maxFiles());
        this.files = new ArrayBlockingQueue<>(queueCapacity);
        this.slots = new Semaphore(queueCapacity);
        this.maxBytes = checked.maxBytes();
        try {
            Files.createDirectories(checked.root());
            if (!Files.isDirectory(checked.root())) {
                throw new IOException("spill root is not a directory");
            }
            if (!Files.isWritable(checked.root())) {
                throw new IOException("spill root is not writable");
            }
            this.directory = Files.createTempDirectory(checked.root(), "astrasync-exchange-");
        } catch (IOException exception) {
            throw new ExchangeFailureException("failed to create spill directory", exception);
        }
    }

    int size() {
        return files.size();
    }

    int capacity() {
        return files.remainingCapacity() + files.size();
    }

    void publish(RowBatch batch, Supplier<Throwable> failure) {
        Objects.requireNonNull(batch, "batch must not be null");
        acquireSlot(failure);
        Path file = null;
        byte[] payload = null;
        boolean enqueued = false;
        try {
            payload = SpillFrameCodec.encode(batch, maxBytes);
            reserveBytes(payload.length, failure);
            file = Files.createTempFile(directory, "batch-", ".spill");
            writeDurably(file, payload);
            synchronized (bytesMonitor) {
                throwIfFailed(failure);
                if (!files.offer(file)) {
                    throw new IOException("spill queue capacity changed unexpectedly");
                }
                enqueued = true;
            }
        } catch (ExchangeFailureException exception) {
            throw exception;
        } catch (IOException | RuntimeException exception) {
            throw new ExchangeFailureException("failed to spill batch", exception);
        } finally {
            if (!enqueued) {
                if (file != null) {
                    deleteQuietly(file);
                }
                if (payload != null) {
                    releaseBytes(payload.length);
                }
                slots.release();
            }
        }
    }

    RowBatch receive(Supplier<Throwable> failure) {
        Path file = null;
        try {
            while (file == null) {
                throwIfFailed(failure);
                file = files.poll(WAIT_MILLIS, TimeUnit.MILLISECONDS);
            }
            byte[] payload = Files.readAllBytes(file);
            return SpillFrameCodec.decode(payload, maxBytes);
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            throw new ExchangeFailureException("receiver interrupted", exception);
        } catch (ExchangeFailureException exception) {
            throw exception;
        } catch (IOException | RuntimeException exception) {
            throw new ExchangeFailureException("failed to read spill batch", exception);
        } finally {
            if (file != null) {
                long size = fileSize(file);
                deleteQuietly(file);
                releaseBytes(size);
                slots.release();
            }
        }
    }

    void fail() {
        synchronized (bytesMonitor) {
            closed = true;
            bytesMonitor.notifyAll();
        }
        cleanupQueuedFiles();
    }

    @Override
    public void close() {
        synchronized (bytesMonitor) {
            closed = true;
            bytesMonitor.notifyAll();
        }
        cleanupQueuedFiles();
        try {
            Files.deleteIfExists(directory);
        } catch (IOException ignored) {
            // The exchange's primary failure is reported by its producer or consumer.
        }
    }

    private void acquireSlot(Supplier<Throwable> failure) {
        try {
            while (true) {
                throwIfFailed(failure);
                if (slots.tryAcquire(WAIT_MILLIS, TimeUnit.MILLISECONDS)) {
                    return;
                }
            }
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            throw new ExchangeFailureException("publisher interrupted", exception);
        }
    }

    private void reserveBytes(long bytes, Supplier<Throwable> failure) {
        if (bytes > maxBytes) {
            throw new IllegalArgumentException("spill frame exceeds maxBytes " + maxBytes);
        }
        synchronized (bytesMonitor) {
            try {
                while (reservedBytes > maxBytes - bytes) {
                    throwIfFailed(failure);
                    bytesMonitor.wait(WAIT_MILLIS);
                }
                throwIfFailed(failure);
                reservedBytes += bytes;
            } catch (InterruptedException exception) {
                Thread.currentThread().interrupt();
                throw new ExchangeFailureException("publisher interrupted", exception);
            }
        }
    }

    private void releaseBytes(long bytes) {
        synchronized (bytesMonitor) {
            reservedBytes = Math.max(0, reservedBytes - Math.max(0, bytes));
            bytesMonitor.notifyAll();
        }
    }

    private void cleanupQueuedFiles() {
        ArrayList<Path> queued = new ArrayList<>();
        files.drainTo(queued);
        for (Path file : queued) {
            releaseBytes(fileSize(file));
            deleteQuietly(file);
            slots.release();
        }
    }

    private void throwIfFailed(Supplier<Throwable> failure) {
        Throwable cause = failure.get();
        if (cause != null) {
            throw new ExchangeFailureException("batch exchange failed", cause);
        }
        synchronized (bytesMonitor) {
            if (closed) {
                throw new ExchangeFailureException("spill storage is closed", null);
            }
        }
    }

    private static void writeDurably(Path file, byte[] payload) throws IOException {
        try (FileChannel channel =
                FileChannel.open(file, StandardOpenOption.WRITE, StandardOpenOption.TRUNCATE_EXISTING)) {
            ByteBuffer buffer = ByteBuffer.wrap(payload);
            while (buffer.hasRemaining()) {
                channel.write(buffer);
            }
            channel.force(true);
        }
    }

    private static long fileSize(Path file) {
        try {
            return Files.exists(file) ? Files.size(file) : 0;
        } catch (IOException ignored) {
            return 0;
        }
    }

    private static void deleteQuietly(Path file) {
        try {
            Files.deleteIfExists(file);
        } catch (IOException ignored) {
            // Cleanup is best effort after the primary exchange failure.
        }
    }
}
