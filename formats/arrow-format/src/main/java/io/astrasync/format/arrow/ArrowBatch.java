package io.astrasync.format.arrow;

import io.astrasync.connector.api.data.RowBatch;
import java.util.Objects;
import org.apache.arrow.memory.BufferAllocator;
import org.apache.arrow.vector.VectorSchemaRoot;
import org.apache.arrow.vector.types.pojo.Schema;

/** A bounded Arrow root with exclusive ownership of its child allocator and backing resource. */
public final class ArrowBatch implements AutoCloseable {
    private final BufferAllocator allocator;
    private final AutoCloseable resource;
    private final VectorSchemaRoot root;
    private final boolean endOfInput;

    private boolean closed;

    ArrowBatch(BufferAllocator allocator, AutoCloseable resource, VectorSchemaRoot root, boolean endOfInput) {
        this.allocator = Objects.requireNonNull(allocator, "allocator must not be null");
        this.resource = Objects.requireNonNull(resource, "resource must not be null");
        this.root = Objects.requireNonNull(root, "root must not be null");
        this.endOfInput = endOfInput;
    }

    public synchronized int size() {
        requireOpen();
        return root.getRowCount();
    }

    public synchronized boolean endOfInput() {
        requireOpen();
        return endOfInput;
    }

    public synchronized Schema schema() {
        requireOpen();
        return root.getSchema();
    }

    /** Returns a borrowed root that is valid only until this batch is closed. */
    public synchronized VectorSchemaRoot root() {
        requireOpen();
        return root;
    }

    public synchronized long allocatedBytes() {
        requireOpen();
        return allocator.getAllocatedMemory();
    }

    public synchronized long allocationLimitBytes() {
        requireOpen();
        return allocator.getLimit();
    }

    public RowBatch toRowBatch() {
        return ArrowBatchCodec.decode(this);
    }

    public synchronized boolean isClosed() {
        return closed;
    }

    @Override
    public synchronized void close() {
        if (closed) {
            return;
        }
        closed = true;

        RuntimeException failure = null;
        try {
            resource.close();
        } catch (Exception exception) {
            failure = asCloseFailure("failed to close Arrow batch resource", exception);
        }
        try {
            allocator.close();
        } catch (RuntimeException exception) {
            if (failure == null) {
                failure = asCloseFailure("failed to close Arrow batch allocator", exception);
            } else {
                failure.addSuppressed(exception);
            }
        }
        if (failure != null) {
            throw failure;
        }
    }

    private void requireOpen() {
        if (closed) {
            throw new IllegalStateException("Arrow batch is closed");
        }
    }

    private static RuntimeException asCloseFailure(String message, Exception exception) {
        return exception instanceof RuntimeException runtimeException
                ? runtimeException
                : new IllegalStateException(message, exception);
    }
}
