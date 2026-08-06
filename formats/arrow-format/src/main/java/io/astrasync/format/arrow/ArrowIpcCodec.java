package io.astrasync.format.arrow;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.OutputStream;
import java.util.Arrays;
import java.util.Objects;
import java.util.concurrent.atomic.AtomicLong;
import org.apache.arrow.memory.BufferAllocator;
import org.apache.arrow.vector.VectorSchemaRoot;
import org.apache.arrow.vector.ipc.ArrowStreamReader;
import org.apache.arrow.vector.ipc.ArrowStreamWriter;

/** Encodes one Arrow batch in a bounded, versioned AstraSync IPC frame. */
public final class ArrowIpcCodec {
    private static final byte[] MAGIC = {'A', 'S', 'T', 'R'};
    private static final int HEADER_SIZE = 8;
    private static final int VERSION = 1;
    private static final int END_OF_INPUT_FLAG = 1;
    private static final int KNOWN_FLAGS = END_OF_INPUT_FLAG;
    private static final byte[] STREAM_END = {(byte) 0xff, (byte) 0xff, (byte) 0xff, (byte) 0xff, 0, 0, 0, 0};
    private static final AtomicLong ALLOCATOR_SEQUENCE = new AtomicLong();

    private ArrowIpcCodec() {}

    public static byte[] encode(ArrowBatch batch, long maxPayloadBytes) {
        Objects.requireNonNull(batch, "batch must not be null");
        ArrowBatchCodec.requirePositive(maxPayloadBytes, "maxPayloadBytes");
        LimitedOutputStream output = new LimitedOutputStream(maxPayloadBytes);
        try {
            writeHeader(output, batch.endOfInput());
            try (ArrowStreamWriter writer = new ArrowStreamWriter(batch.root(), null, output)) {
                writer.start();
                writer.writeBatch();
                writer.end();
            }
            return output.toByteArray();
        } catch (PayloadLimitException exception) {
            throw new IllegalArgumentException(
                    "Arrow IPC payload exceeds limit of " + maxPayloadBytes + " bytes", exception);
        } catch (IOException exception) {
            throw new IllegalStateException("failed to encode Arrow IPC payload", exception);
        }
    }

    public static ArrowBatch decode(
            BufferAllocator parent, long maxAllocationBytes, long maxPayloadBytes, byte[] payload) {
        Objects.requireNonNull(parent, "parent allocator must not be null");
        Objects.requireNonNull(payload, "payload must not be null");
        ArrowBatchCodec.requirePositive(maxAllocationBytes, "maxAllocationBytes");
        ArrowBatchCodec.requirePositive(maxPayloadBytes, "maxPayloadBytes");
        if (payload.length > maxPayloadBytes) {
            throw new IllegalArgumentException(
                    "Arrow IPC payload has " + payload.length + " bytes, limit is " + maxPayloadBytes);
        }
        boolean endOfInput = readHeader(payload);

        BufferAllocator child = parent.newChildAllocator(
                "astrasync-arrow-ipc-" + ALLOCATOR_SEQUENCE.incrementAndGet(), 0, maxAllocationBytes);
        ArrowStreamReader reader = null;
        try {
            ByteArrayInputStream input = new ByteArrayInputStream(payload, HEADER_SIZE, payload.length - HEADER_SIZE);
            reader = new ArrowStreamReader(input, child);
            boolean loaded;
            VectorSchemaRoot root;
            try {
                loaded = reader.loadNextBatch();
                root = loaded ? reader.getVectorSchemaRoot() : null;
            } catch (IOException | IllegalArgumentException exception) {
                throw new InvalidStreamException(exception);
            }
            if (!loaded) {
                throw new IllegalArgumentException("Arrow IPC stream contains no record batch");
            }
            if (!Arrays.equals(input.readAllBytes(), STREAM_END)) {
                throw new IllegalArgumentException("Arrow IPC stream must contain exactly one record batch");
            }
            ArrowBatchCodec.validateSchema(root.getSchema());
            return new ArrowBatch(child, reader, root, endOfInput);
        } catch (InvalidStreamException exception) {
            IllegalArgumentException failure =
                    new IllegalArgumentException("invalid Arrow IPC stream", exception.getCause());
            closeAfterFailure(reader, child, failure);
            throw failure;
        } catch (RuntimeException | Error failure) {
            closeAfterFailure(reader, child, failure);
            throw failure;
        }
    }

    private static void writeHeader(OutputStream output, boolean endOfInput) throws IOException {
        output.write(MAGIC);
        output.write(VERSION);
        output.write(endOfInput ? END_OF_INPUT_FLAG : 0);
        output.write(0);
        output.write(0);
    }

    private static boolean readHeader(byte[] payload) {
        if (payload.length <= HEADER_SIZE) {
            throw new IllegalArgumentException("Arrow IPC payload is too short");
        }
        for (int index = 0; index < MAGIC.length; index++) {
            if (payload[index] != MAGIC[index]) {
                throw new IllegalArgumentException("Arrow IPC payload has invalid magic");
            }
        }
        int version = Byte.toUnsignedInt(payload[4]);
        if (version != VERSION) {
            throw new IllegalArgumentException("unsupported Arrow IPC frame version " + version);
        }
        int flags = Byte.toUnsignedInt(payload[5]);
        if ((flags & ~KNOWN_FLAGS) != 0) {
            throw new IllegalArgumentException("Arrow IPC frame has unknown flags " + flags);
        }
        if (payload[6] != 0 || payload[7] != 0) {
            throw new IllegalArgumentException("Arrow IPC frame reserved bytes must be zero");
        }
        return (flags & END_OF_INPUT_FLAG) != 0;
    }

    private static void closeAfterFailure(ArrowStreamReader reader, BufferAllocator allocator, Throwable failure) {
        if (reader != null) {
            try {
                reader.close();
            } catch (IOException | RuntimeException closeFailure) {
                failure.addSuppressed(closeFailure);
            }
        }
        try {
            allocator.close();
        } catch (RuntimeException closeFailure) {
            failure.addSuppressed(closeFailure);
        }
    }

    private static final class LimitedOutputStream extends OutputStream {
        private final long limit;
        private final ByteArrayOutputStream delegate = new ByteArrayOutputStream();
        private long written;

        private LimitedOutputStream(long limit) {
            this.limit = limit;
        }

        @Override
        public void write(int value) throws IOException {
            reserve(1);
            delegate.write(value);
        }

        @Override
        public void write(byte[] values, int offset, int length) throws IOException {
            Objects.checkFromIndexSize(offset, length, values.length);
            reserve(length);
            delegate.write(values, offset, length);
        }

        private byte[] toByteArray() {
            return delegate.toByteArray();
        }

        private void reserve(int length) throws PayloadLimitException {
            if (length > limit - written) {
                throw new PayloadLimitException();
            }
            written += length;
        }
    }

    private static final class PayloadLimitException extends IOException {
        private static final long serialVersionUID = 1L;
    }

    private static final class InvalidStreamException extends RuntimeException {
        private static final long serialVersionUID = 1L;

        private InvalidStreamException(Exception cause) {
            super(cause);
        }
    }
}
