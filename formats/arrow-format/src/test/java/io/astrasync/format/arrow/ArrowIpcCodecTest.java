package io.astrasync.format.arrow;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.Arrays;
import java.util.List;
import org.apache.arrow.memory.BufferAllocator;
import org.apache.arrow.memory.RootAllocator;
import org.apache.arrow.vector.VectorSchemaRoot;
import org.apache.arrow.vector.ipc.ArrowStreamWriter;
import org.apache.arrow.vector.types.pojo.ArrowType;
import org.apache.arrow.vector.types.pojo.Field;
import org.apache.arrow.vector.types.pojo.FieldType;
import org.apache.arrow.vector.types.pojo.Schema;
import org.junit.jupiter.api.Test;

class ArrowIpcCodecTest {
    private static final long MEMORY_LIMIT = 16L * 1024 * 1024;
    private static final long PAYLOAD_LIMIT = 16L * 1024 * 1024;

    @Test
    void framedIpcRoundTripPreservesSchemaRowsAndTerminalState() {
        RowBatch input = RowBatch.last(List.of(Row.of("id", 1L), Row.of("id", 2L)));
        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT);
                ArrowBatch encoded = ArrowBatchCodec.encode(parent, MEMORY_LIMIT, input)) {
            byte[] payload = ArrowIpcCodec.encode(encoded, PAYLOAD_LIMIT);
            assertThat(payload).startsWith((byte) 'A', (byte) 'S', (byte) 'T', (byte) 'R', (byte) 1, (byte) 1);

            long encodedMemory = parent.getAllocatedMemory();
            try (ArrowBatch decoded = ArrowIpcCodec.decode(parent, MEMORY_LIMIT, PAYLOAD_LIMIT, payload)) {
                assertThat(decoded.schema()).isEqualTo(encoded.schema());
                assertThat(decoded.endOfInput()).isTrue();
                assertThat(decoded.toRowBatch()).isEqualTo(input);
            }
            assertThat(parent.getAllocatedMemory()).isEqualTo(encodedMemory);
        }
    }

    @Test
    void framedIpcPreservesNonTerminalState() {
        RowBatch input = RowBatch.data(List.of(Row.of("value", "next")));
        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT);
                ArrowBatch encoded = ArrowBatchCodec.encode(parent, MEMORY_LIMIT, input)) {
            byte[] payload = ArrowIpcCodec.encode(encoded, PAYLOAD_LIMIT);
            assertThat(payload[5]).isZero();
            try (ArrowBatch decoded = ArrowIpcCodec.decode(parent, MEMORY_LIMIT, PAYLOAD_LIMIT, payload)) {
                assertThat(decoded.endOfInput()).isFalse();
                assertThat(decoded.toRowBatch()).isEqualTo(input);
            }
        }
    }

    @Test
    void rejectsInvalidHeaderVersionFlagsReservedBytesAndLimits() {
        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT);
                ArrowBatch encoded =
                        ArrowBatchCodec.encode(parent, MEMORY_LIMIT, RowBatch.last(List.of(Row.of("id", 1L))))) {
            byte[] payload = ArrowIpcCodec.encode(encoded, PAYLOAD_LIMIT);

            assertInvalid(parent, changed(payload, 0, (byte) 'X'), "invalid magic");
            assertInvalid(parent, changed(payload, 4, (byte) 2), "unsupported Arrow IPC frame version 2");
            assertInvalid(parent, changed(payload, 5, (byte) 2), "unknown flags 2");
            assertInvalid(parent, changed(payload, 6, (byte) 1), "reserved bytes must be zero");
            assertThatThrownBy(() -> ArrowIpcCodec.decode(parent, MEMORY_LIMIT, PAYLOAD_LIMIT, new byte[8]))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessage("Arrow IPC payload is too short");
            assertThatThrownBy(() -> ArrowIpcCodec.decode(parent, MEMORY_LIMIT, payload.length - 1L, payload))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("limit is");
            assertThatThrownBy(() -> ArrowIpcCodec.encode(encoded, 8))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("exceeds limit");
        }
    }

    @Test
    void rejectsTruncatedMissingBatchAndUnsupportedSchemaWithoutLeaks() throws IOException {
        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT);
                ArrowBatch encoded =
                        ArrowBatchCodec.encode(parent, MEMORY_LIMIT, RowBatch.last(List.of(Row.of("id", 1L))))) {
            long baseline = parent.getAllocatedMemory();
            byte[] payload = ArrowIpcCodec.encode(encoded, PAYLOAD_LIMIT);
            byte[] truncated = Arrays.copyOf(payload, Math.max(9, payload.length / 2));
            assertThatThrownBy(() -> ArrowIpcCodec.decode(parent, MEMORY_LIMIT, PAYLOAD_LIMIT, truncated))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("invalid Arrow IPC stream");
            assertThat(parent.getAllocatedMemory()).isEqualTo(baseline);

            Schema supported =
                    new Schema(List.of(new Field("id", FieldType.nullable(new ArrowType.Int(64, true)), List.of())));
            byte[] noBatch = frame(parent, supported, 0);
            assertThatThrownBy(() -> ArrowIpcCodec.decode(parent, MEMORY_LIMIT, PAYLOAD_LIMIT, noBatch))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("no record batch");
            assertThat(parent.getAllocatedMemory()).isEqualTo(baseline);

            Schema unsupported =
                    new Schema(List.of(new Field("id", FieldType.nullable(new ArrowType.Int(32, false)), List.of())));
            byte[] unsupportedPayload = frame(parent, unsupported, 1);
            assertThatThrownBy(() -> ArrowIpcCodec.decode(parent, MEMORY_LIMIT, PAYLOAD_LIMIT, unsupportedPayload))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("unsupported Arrow type");
            assertThat(parent.getAllocatedMemory()).isEqualTo(baseline);

            byte[] multipleBatches = frame(parent, supported, 2);
            assertThatThrownBy(() -> ArrowIpcCodec.decode(parent, MEMORY_LIMIT, PAYLOAD_LIMIT, multipleBatches))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("exactly one record batch");
            assertThat(parent.getAllocatedMemory()).isEqualTo(baseline);
        }
    }

    @Test
    void decodeAllocationLimitFailsWithoutLeakingChildAllocator() {
        String large = "payload".repeat(16 * 1024);
        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT);
                ArrowBatch encoded =
                        ArrowBatchCodec.encode(parent, MEMORY_LIMIT, RowBatch.last(List.of(Row.of("value", large))))) {
            byte[] payload = ArrowIpcCodec.encode(encoded, PAYLOAD_LIMIT);
            long baseline = parent.getAllocatedMemory();
            assertThatThrownBy(() -> ArrowIpcCodec.decode(parent, 1024, PAYLOAD_LIMIT, payload))
                    .isInstanceOf(RuntimeException.class);
            assertThat(parent.getAllocatedMemory()).isEqualTo(baseline);
            assertThat(parent.getChildAllocators()).hasSize(1);
        }
    }

    private static void assertInvalid(RootAllocator parent, byte[] payload, String message) {
        long baseline = parent.getAllocatedMemory();
        assertThatThrownBy(() -> ArrowIpcCodec.decode(parent, MEMORY_LIMIT, PAYLOAD_LIMIT, payload))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining(message);
        assertThat(parent.getAllocatedMemory()).isEqualTo(baseline);
    }

    private static byte[] changed(byte[] payload, int index, byte value) {
        byte[] changed = Arrays.copyOf(payload, payload.length);
        changed[index] = value;
        return changed;
    }

    private static byte[] frame(BufferAllocator allocator, Schema schema, int batchCount) throws IOException {
        try (BufferAllocator child = allocator.newChildAllocator("test-frame", 0, MEMORY_LIMIT);
                VectorSchemaRoot root = VectorSchemaRoot.create(schema, child);
                ByteArrayOutputStream stream = new ByteArrayOutputStream()) {
            root.setRowCount(0);
            try (ArrowStreamWriter writer = new ArrowStreamWriter(root, null, stream)) {
                writer.start();
                for (int index = 0; index < batchCount; index++) {
                    writer.writeBatch();
                }
                writer.end();
            }
            byte[] arrow = stream.toByteArray();
            byte[] payload = new byte[8 + arrow.length];
            payload[0] = 'A';
            payload[1] = 'S';
            payload[2] = 'T';
            payload[3] = 'R';
            payload[4] = 1;
            System.arraycopy(arrow, 0, payload, 8, arrow.length);
            return payload;
        }
    }
}
