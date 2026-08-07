package io.astrasync.engine.runtime;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.EOFException;
import java.io.IOException;
import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.LocalTime;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/** Versioned, bounded frame codec for spill files. */
final class SpillFrameCodec {
    private static final byte[] MAGIC = {'A', 'S', 'P', 'L'};
    private static final int VERSION = 1;
    private static final int END_OF_INPUT_FLAG = 1;
    private static final int KNOWN_FLAGS = END_OF_INPUT_FLAG;
    private static final int HEADER_BYTES = 12;
    private static final int MAX_ELEMENTS = 1_000_000;

    private static final byte NULL = 0;
    private static final byte STRING = 1;
    private static final byte BOOLEAN = 2;
    private static final byte BYTE = 3;
    private static final byte SHORT = 4;
    private static final byte INTEGER = 5;
    private static final byte LONG = 6;
    private static final byte FLOAT = 7;
    private static final byte DOUBLE = 8;
    private static final byte DECIMAL = 9;
    private static final byte BINARY = 10;
    private static final byte LOCAL_DATE = 11;
    private static final byte LOCAL_TIME = 12;
    private static final byte LOCAL_DATE_TIME = 13;
    private static final byte OFFSET_DATE_TIME = 14;

    private SpillFrameCodec() {}

    static byte[] encode(RowBatch batch, long maxBytes) {
        Objects.requireNonNull(batch, "batch must not be null");
        requirePositive(maxBytes, "maxBytes");
        BoundedOutputStream bounded = new BoundedOutputStream(maxBytes);
        try (DataOutputStream output = new DataOutputStream(bounded)) {
            output.write(MAGIC);
            output.writeByte(VERSION);
            output.writeByte(batch.endOfInput() ? END_OF_INPUT_FLAG : 0);
            output.writeShort(0);
            output.writeInt(batch.size());
            for (Row row : batch.rows()) {
                writeRow(output, row, maxBytes);
            }
            output.flush();
            return bounded.toByteArray();
        } catch (SpillFrameLimitException exception) {
            throw new IllegalArgumentException("spill frame exceeds maxBytes " + maxBytes, exception);
        } catch (IOException exception) {
            throw new IllegalStateException("failed to encode spill frame", exception);
        }
    }

    static RowBatch decode(byte[] payload, long maxBytes) {
        Objects.requireNonNull(payload, "payload must not be null");
        requirePositive(maxBytes, "maxBytes");
        if (payload.length > maxBytes) {
            throw new IllegalArgumentException("spill frame has " + payload.length + " bytes, limit is " + maxBytes);
        }
        try (DataInputStream input = new DataInputStream(new ByteArrayInputStream(payload))) {
            for (byte expected : MAGIC) {
                if (input.readByte() != expected) {
                    throw invalid("invalid spill frame magic");
                }
            }
            int version = input.readUnsignedByte();
            if (version != VERSION) {
                throw invalid("unsupported spill frame version " + version);
            }
            int flags = input.readUnsignedByte();
            if ((flags & ~KNOWN_FLAGS) != 0) {
                throw invalid("unknown spill frame flags " + flags);
            }
            if (input.readUnsignedShort() != 0) {
                throw invalid("spill frame reserved bytes must be zero");
            }
            int rowCount = readCount(input, "row count");
            List<Row> rows = new java.util.ArrayList<>(rowCount);
            for (int index = 0; index < rowCount; index++) {
                rows.add(readRow(input, maxBytes));
            }
            if (input.available() != 0) {
                throw invalid("spill frame has trailing bytes");
            }
            return (flags & END_OF_INPUT_FLAG) != 0 ? RowBatch.last(rows) : RowBatch.data(rows);
        } catch (IllegalArgumentException exception) {
            throw exception;
        } catch (EOFException exception) {
            throw invalid("spill frame is truncated", exception);
        } catch (IOException | RuntimeException exception) {
            throw invalid("failed to decode spill frame", exception);
        }
    }

    private static void writeRow(DataOutputStream output, Row row, long maxBytes) throws IOException {
        Map<String, Object> values = row.asMap();
        writeCount(output, values.size(), "column count");
        for (Map.Entry<String, Object> entry : values.entrySet()) {
            writeString(output, entry.getKey(), maxBytes);
            writeValue(output, entry.getValue(), maxBytes);
        }
    }

    private static Row readRow(DataInputStream input, long maxBytes) throws IOException {
        int columnCount = readCount(input, "column count");
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        for (int index = 0; index < columnCount; index++) {
            String name = readString(input, maxBytes);
            if (values.containsKey(name)) {
                throw invalid("duplicate spill frame column " + name);
            }
            values.put(name, readValue(input, maxBytes));
        }
        return Row.of(values);
    }

    private static void writeValue(DataOutputStream output, Object value, long maxBytes) throws IOException {
        if (value == null) {
            output.writeByte(NULL);
        } else if (value instanceof String item) {
            output.writeByte(STRING);
            writeString(output, item, maxBytes);
        } else if (value instanceof Boolean item) {
            output.writeByte(BOOLEAN);
            output.writeBoolean(item);
        } else if (value instanceof Byte item) {
            output.writeByte(BYTE);
            output.writeByte(item);
        } else if (value instanceof Short item) {
            output.writeByte(SHORT);
            output.writeShort(item);
        } else if (value instanceof Integer item) {
            output.writeByte(INTEGER);
            output.writeInt(item);
        } else if (value instanceof Long item) {
            output.writeByte(LONG);
            output.writeLong(item);
        } else if (value instanceof Float item) {
            output.writeByte(FLOAT);
            output.writeFloat(item);
        } else if (value instanceof Double item) {
            output.writeByte(DOUBLE);
            output.writeDouble(item);
        } else if (value instanceof BigDecimal item) {
            output.writeByte(DECIMAL);
            writeString(output, item.toPlainString(), maxBytes);
        } else if (value instanceof byte[] item) {
            output.writeByte(BINARY);
            writeBytes(output, item, maxBytes);
        } else if (value instanceof LocalDate item) {
            output.writeByte(LOCAL_DATE);
            output.writeLong(item.toEpochDay());
        } else if (value instanceof LocalTime item) {
            output.writeByte(LOCAL_TIME);
            output.writeLong(item.toNanoOfDay());
        } else if (value instanceof LocalDateTime item) {
            output.writeByte(LOCAL_DATE_TIME);
            output.writeLong(item.toEpochSecond(ZoneOffset.UTC));
            output.writeInt(item.getNano());
        } else if (value instanceof OffsetDateTime item) {
            output.writeByte(OFFSET_DATE_TIME);
            output.writeLong(item.toEpochSecond());
            output.writeInt(item.getNano());
            output.writeInt(item.getOffset().getTotalSeconds());
        } else {
            throw new IllegalArgumentException(
                    "unsupported spill value type " + value.getClass().getName());
        }
    }

    private static Object readValue(DataInputStream input, long maxBytes) throws IOException {
        return switch (input.readUnsignedByte()) {
            case NULL -> null;
            case STRING -> readString(input, maxBytes);
            case BOOLEAN -> input.readBoolean();
            case BYTE -> input.readByte();
            case SHORT -> input.readShort();
            case INTEGER -> input.readInt();
            case LONG -> input.readLong();
            case FLOAT -> input.readFloat();
            case DOUBLE -> input.readDouble();
            case DECIMAL -> new BigDecimal(readString(input, maxBytes));
            case BINARY -> readBytes(input, maxBytes);
            case LOCAL_DATE -> LocalDate.ofEpochDay(input.readLong());
            case LOCAL_TIME -> LocalTime.ofNanoOfDay(input.readLong());
            case LOCAL_DATE_TIME -> LocalDateTime.ofEpochSecond(input.readLong(), input.readInt(), ZoneOffset.UTC);
            case OFFSET_DATE_TIME -> OffsetDateTime.ofInstant(
                    java.time.Instant.ofEpochSecond(input.readLong(), input.readInt()),
                    ZoneOffset.ofTotalSeconds(input.readInt()));
            default -> throw invalid("unsupported spill value tag");
        };
    }

    private static void writeString(DataOutputStream output, String value, long maxBytes) throws IOException {
        byte[] bytes = value.getBytes(StandardCharsets.UTF_8);
        writeBytes(output, bytes, maxBytes);
    }

    private static String readString(DataInputStream input, long maxBytes) throws IOException {
        return new String(readBytes(input, maxBytes), StandardCharsets.UTF_8);
    }

    private static void writeBytes(DataOutputStream output, byte[] value, long maxBytes) throws IOException {
        if (value.length > maxBytes || value.length > Integer.MAX_VALUE) {
            throw new IllegalArgumentException("spill frame field is too large");
        }
        output.writeInt(value.length);
        output.write(value);
    }

    private static byte[] readBytes(DataInputStream input, long maxBytes) throws IOException {
        int length = input.readInt();
        if (length < 0 || length > maxBytes || length > input.available()) {
            throw invalid("spill frame field length is invalid");
        }
        return input.readNBytes(length);
    }

    private static void writeCount(DataOutputStream output, int count, String name) throws IOException {
        if (count < 0 || count > MAX_ELEMENTS) {
            throw new IllegalArgumentException(name + " is out of bounds");
        }
        output.writeInt(count);
    }

    private static int readCount(DataInputStream input, String name) throws IOException {
        int count = input.readInt();
        if (count < 0 || count > MAX_ELEMENTS) {
            throw invalid(name + " is out of bounds");
        }
        return count;
    }

    private static long requirePositive(long value, String name) {
        if (value <= 0) {
            throw new IllegalArgumentException(name + " must be positive");
        }
        return value;
    }

    private static IllegalArgumentException invalid(String message) {
        return new IllegalArgumentException(message);
    }

    private static IllegalArgumentException invalid(String message, Throwable cause) {
        return new IllegalArgumentException(message, cause);
    }

    private static final class BoundedOutputStream extends java.io.OutputStream {
        private final long limit;
        private final ByteArrayOutputStream delegate = new ByteArrayOutputStream();
        private long written;

        private BoundedOutputStream(long limit) {
            this.limit = limit;
        }

        @Override
        public void write(int value) {
            reserve(1);
            delegate.write(value);
        }

        @Override
        public void write(byte[] bytes, int offset, int length) {
            Objects.checkFromIndexSize(offset, length, bytes.length);
            reserve(length);
            delegate.write(bytes, offset, length);
        }

        private void reserve(int length) {
            if (length > limit - written) {
                throw new SpillFrameLimitException();
            }
            written += length;
        }

        private byte[] toByteArray() {
            return delegate.toByteArray();
        }
    }

    private static final class SpillFrameLimitException extends RuntimeException {
        private static final long serialVersionUID = 1L;
    }
}
