package io.astrasync.format.arrow;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import java.math.BigDecimal;
import java.math.RoundingMode;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.LocalTime;
import java.time.OffsetDateTime;
import java.time.ZoneId;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.concurrent.atomic.AtomicLong;
import org.apache.arrow.memory.BufferAllocator;
import org.apache.arrow.vector.BigIntVector;
import org.apache.arrow.vector.BitVector;
import org.apache.arrow.vector.DateDayVector;
import org.apache.arrow.vector.DecimalVector;
import org.apache.arrow.vector.FieldVector;
import org.apache.arrow.vector.Float4Vector;
import org.apache.arrow.vector.Float8Vector;
import org.apache.arrow.vector.IntVector;
import org.apache.arrow.vector.SmallIntVector;
import org.apache.arrow.vector.TimeNanoVector;
import org.apache.arrow.vector.TimeStampNanoTZVector;
import org.apache.arrow.vector.TimeStampNanoVector;
import org.apache.arrow.vector.TinyIntVector;
import org.apache.arrow.vector.VarBinaryVector;
import org.apache.arrow.vector.VarCharVector;
import org.apache.arrow.vector.VectorSchemaRoot;
import org.apache.arrow.vector.types.DateUnit;
import org.apache.arrow.vector.types.FloatingPointPrecision;
import org.apache.arrow.vector.types.TimeUnit;
import org.apache.arrow.vector.types.pojo.ArrowType;
import org.apache.arrow.vector.types.pojo.Field;
import org.apache.arrow.vector.types.pojo.FieldType;
import org.apache.arrow.vector.types.pojo.Schema;

/** Converts immutable AstraSync rows to and from bounded Apache Arrow vectors. */
public final class ArrowBatchCodec {
    private static final int MAX_DECIMAL_PRECISION = 38;
    private static final String UTC_TIMEZONE = "UTC";
    private static final AtomicLong ALLOCATOR_SEQUENCE = new AtomicLong();

    private ArrowBatchCodec() {}

    public static Schema inferSchema(RowBatch batch) {
        Objects.requireNonNull(batch, "batch must not be null");
        if (batch.rows().isEmpty()) {
            throw new IllegalArgumentException("cannot infer an Arrow schema from an empty batch");
        }

        List<String> names = List.copyOf(batch.rows().get(0).asMap().keySet());
        validateRowColumns(batch.rows(), names);
        List<Field> fields = new ArrayList<>(names.size());
        for (String name : names) {
            boolean nullable = false;
            Class<?> valueType = null;
            List<BigDecimal> decimals = new ArrayList<>();
            for (Row row : batch.rows()) {
                Object value = row.get(name);
                if (value == null) {
                    nullable = true;
                    continue;
                }
                if (valueType == null) {
                    valueType = value.getClass();
                } else if (valueType != value.getClass()) {
                    throw new IllegalArgumentException("column '" + name + "' mixes Java value types "
                            + valueType.getName() + " and " + value.getClass().getName());
                }
                if (value instanceof BigDecimal decimal) {
                    decimals.add(decimal);
                }
            }
            if (valueType == null) {
                throw new IllegalArgumentException(
                        "column '" + name + "' contains only nulls; provide an explicit Arrow schema");
            }
            ArrowType arrowType = inferType(name, valueType, decimals);
            FieldType fieldType = nullable ? FieldType.nullable(arrowType) : FieldType.notNullable(arrowType);
            fields.add(new Field(name, fieldType, List.of()));
        }
        return new Schema(fields);
    }

    public static ArrowBatch encode(BufferAllocator parent, long maxAllocationBytes, RowBatch batch) {
        return encode(parent, maxAllocationBytes, inferSchema(batch), batch);
    }

    public static ArrowBatch encode(BufferAllocator parent, long maxAllocationBytes, Schema schema, RowBatch batch) {
        Objects.requireNonNull(parent, "parent allocator must not be null");
        Objects.requireNonNull(schema, "schema must not be null");
        Objects.requireNonNull(batch, "batch must not be null");
        requirePositive(maxAllocationBytes, "maxAllocationBytes");
        validateSchema(schema);
        validateRows(schema, batch.rows());

        BufferAllocator child = parent.newChildAllocator(
                "astrasync-arrow-batch-" + ALLOCATOR_SEQUENCE.incrementAndGet(), 0, maxAllocationBytes);
        VectorSchemaRoot root = null;
        try {
            root = VectorSchemaRoot.create(schema, child);
            populate(root, batch.rows());
            return new ArrowBatch(child, root, root, batch.endOfInput());
        } catch (RuntimeException | Error failure) {
            closeAfterFailure(root, child, failure);
            throw failure;
        }
    }

    public static RowBatch decode(ArrowBatch batch) {
        Objects.requireNonNull(batch, "batch must not be null");
        VectorSchemaRoot root = batch.root();
        validateSchema(root.getSchema());

        List<Row> rows = new ArrayList<>(root.getRowCount());
        for (int rowIndex = 0; rowIndex < root.getRowCount(); rowIndex++) {
            LinkedHashMap<String, Object> values = new LinkedHashMap<>();
            for (FieldVector vector : root.getFieldVectors()) {
                values.put(vector.getName(), readValue(vector, rowIndex));
            }
            rows.add(Row.of(values));
        }
        return batch.endOfInput() ? RowBatch.last(rows) : RowBatch.data(rows);
    }

    static void validateSchema(Schema schema) {
        Objects.requireNonNull(schema, "schema must not be null");
        Set<String> names = new HashSet<>();
        for (Field field : schema.getFields()) {
            String name = Objects.requireNonNull(field.getName(), "Arrow field name must not be null");
            if (name.isBlank()) {
                throw new IllegalArgumentException("Arrow field name must not be blank");
            }
            if (!names.add(name)) {
                throw new IllegalArgumentException("duplicate Arrow field name '" + name + "'");
            }
            if (!field.getChildren().isEmpty()) {
                throw new IllegalArgumentException("nested Arrow field is not supported: " + name);
            }
            if (field.getDictionary() != null) {
                throw new IllegalArgumentException("dictionary-encoded Arrow field is not supported: " + name);
            }
            validateArrowType(name, field.getType());
        }
    }

    static long requirePositive(long value, String name) {
        if (value <= 0) {
            throw new IllegalArgumentException(name + " must be positive");
        }
        return value;
    }

    private static void validateRows(Schema schema, List<Row> rows) {
        List<String> names = schema.getFields().stream().map(Field::getName).toList();
        validateRowColumns(rows, names);
        for (Row row : rows) {
            for (Field field : schema.getFields()) {
                Object value = row.get(field.getName());
                if (value == null) {
                    if (!field.isNullable()) {
                        throw new IllegalArgumentException(
                                "column '" + field.getName() + "' is null but the Arrow field is not nullable");
                    }
                    continue;
                }
                validateValue(field, value);
            }
        }
    }

    private static void validateRowColumns(List<Row> rows, List<String> names) {
        Set<String> expected = Set.copyOf(names);
        for (int index = 0; index < rows.size(); index++) {
            Set<String> actual = rows.get(index).asMap().keySet();
            if (!actual.equals(expected)) {
                throw new IllegalArgumentException(
                        "row " + index + " columns " + actual + " do not match Arrow schema columns " + names);
            }
        }
    }

    private static ArrowType inferType(String name, Class<?> type, List<BigDecimal> decimals) {
        if (type == Boolean.class) {
            return ArrowType.Bool.INSTANCE;
        }
        if (type == Byte.class) {
            return new ArrowType.Int(8, true);
        }
        if (type == Short.class) {
            return new ArrowType.Int(16, true);
        }
        if (type == Integer.class) {
            return new ArrowType.Int(32, true);
        }
        if (type == Long.class) {
            return new ArrowType.Int(64, true);
        }
        if (type == Float.class) {
            return new ArrowType.FloatingPoint(FloatingPointPrecision.SINGLE);
        }
        if (type == Double.class) {
            return new ArrowType.FloatingPoint(FloatingPointPrecision.DOUBLE);
        }
        if (type == BigDecimal.class) {
            return inferDecimal(name, decimals);
        }
        if (type == String.class) {
            return ArrowType.Utf8.INSTANCE;
        }
        if (type == byte[].class) {
            return ArrowType.Binary.INSTANCE;
        }
        if (type == LocalDate.class) {
            return new ArrowType.Date(DateUnit.DAY);
        }
        if (type == LocalTime.class) {
            return new ArrowType.Time(TimeUnit.NANOSECOND, 64);
        }
        if (type == LocalDateTime.class) {
            return new ArrowType.Timestamp(TimeUnit.NANOSECOND, null);
        }
        if (type == OffsetDateTime.class) {
            return new ArrowType.Timestamp(TimeUnit.NANOSECOND, UTC_TIMEZONE);
        }
        throw new IllegalArgumentException("column '" + name + "' has unsupported Java value type " + type.getName());
    }

    private static ArrowType.Decimal inferDecimal(String name, List<BigDecimal> decimals) {
        int scale = decimals.stream().mapToInt(BigDecimal::scale).max().orElse(0);
        if (scale < 0) {
            scale = 0;
        }
        int integerDigits = 0;
        for (BigDecimal decimal : decimals) {
            integerDigits = Math.max(integerDigits, Math.max(0, decimal.precision() - decimal.scale()));
        }
        int precision = Math.max(1, Math.addExact(integerDigits, scale));
        if (precision > MAX_DECIMAL_PRECISION) {
            throw new IllegalArgumentException("column '" + name + "' requires Decimal128 precision " + precision
                    + ", maximum is " + MAX_DECIMAL_PRECISION);
        }
        return new ArrowType.Decimal(precision, scale, 128);
    }

    private static void validateArrowType(String name, ArrowType type) {
        if (type instanceof ArrowType.Bool || type instanceof ArrowType.Utf8 || type instanceof ArrowType.Binary) {
            return;
        }
        if (type instanceof ArrowType.Int integer) {
            if (!integer.getIsSigned()
                    || (integer.getBitWidth() != 8
                            && integer.getBitWidth() != 16
                            && integer.getBitWidth() != 32
                            && integer.getBitWidth() != 64)) {
                throw unsupportedArrowType(name, type);
            }
            return;
        }
        if (type instanceof ArrowType.FloatingPoint floatingPoint) {
            if (floatingPoint.getPrecision() != FloatingPointPrecision.SINGLE
                    && floatingPoint.getPrecision() != FloatingPointPrecision.DOUBLE) {
                throw unsupportedArrowType(name, type);
            }
            return;
        }
        if (type instanceof ArrowType.Decimal decimal) {
            if (decimal.getBitWidth() != 128
                    || decimal.getPrecision() <= 0
                    || decimal.getPrecision() > MAX_DECIMAL_PRECISION
                    || decimal.getScale() < 0
                    || decimal.getScale() > decimal.getPrecision()) {
                throw unsupportedArrowType(name, type);
            }
            return;
        }
        if (type instanceof ArrowType.Date date && date.getUnit() == DateUnit.DAY) {
            return;
        }
        if (type instanceof ArrowType.Time time && time.getUnit() == TimeUnit.NANOSECOND && time.getBitWidth() == 64) {
            return;
        }
        if (type instanceof ArrowType.Timestamp timestamp && timestamp.getUnit() == TimeUnit.NANOSECOND) {
            if (timestamp.getTimezone() != null) {
                try {
                    ZoneId.of(timestamp.getTimezone());
                } catch (RuntimeException exception) {
                    throw new IllegalArgumentException(
                            "Arrow timestamp field '" + name + "' has invalid timezone " + timestamp.getTimezone(),
                            exception);
                }
            }
            return;
        }
        throw unsupportedArrowType(name, type);
    }

    private static IllegalArgumentException unsupportedArrowType(String name, ArrowType type) {
        return new IllegalArgumentException("unsupported Arrow type for column '" + name + "': " + type);
    }

    private static void validateValue(Field field, Object value) {
        String name = field.getName();
        ArrowType type = field.getType();
        if (type instanceof ArrowType.Bool) {
            requireType(name, value, Boolean.class);
        } else if (type instanceof ArrowType.Int integer) {
            Class<?> expected =
                    switch (integer.getBitWidth()) {
                        case 8 -> Byte.class;
                        case 16 -> Short.class;
                        case 32 -> Integer.class;
                        case 64 -> Long.class;
                        default -> throw unsupportedArrowType(name, type);
                    };
            requireType(name, value, expected);
        } else if (type instanceof ArrowType.FloatingPoint floatingPoint) {
            if (floatingPoint.getPrecision() == FloatingPointPrecision.SINGLE) {
                requireType(name, value, Float.class);
            } else {
                requireType(name, value, Double.class);
            }
        } else if (type instanceof ArrowType.Decimal decimal) {
            BigDecimal checked = requireType(name, value, BigDecimal.class);
            BigDecimal scaled;
            try {
                scaled = checked.setScale(decimal.getScale(), RoundingMode.UNNECESSARY);
            } catch (ArithmeticException exception) {
                throw new IllegalArgumentException(
                        "column '" + name + "' value " + checked + " cannot use Arrow decimal scale "
                                + decimal.getScale(),
                        exception);
            }
            if (scaled.precision() > decimal.getPrecision()) {
                throw new IllegalArgumentException("column '" + name + "' value " + checked
                        + " exceeds Arrow decimal precision " + decimal.getPrecision());
            }
        } else if (type instanceof ArrowType.Utf8) {
            requireType(name, value, String.class);
        } else if (type instanceof ArrowType.Binary) {
            requireType(name, value, byte[].class);
        } else if (type instanceof ArrowType.Date) {
            requireType(name, value, LocalDate.class);
        } else if (type instanceof ArrowType.Time) {
            requireType(name, value, LocalTime.class);
        } else if (type instanceof ArrowType.Timestamp timestamp) {
            if (timestamp.getTimezone() == null) {
                requireType(name, value, LocalDateTime.class);
            } else {
                requireType(name, value, OffsetDateTime.class);
            }
        } else {
            throw unsupportedArrowType(name, type);
        }
    }

    private static <T> T requireType(String name, Object value, Class<T> expected) {
        if (!expected.isInstance(value)) {
            throw new IllegalArgumentException("column '" + name + "' requires " + expected.getName() + " but got "
                    + value.getClass().getName());
        }
        return expected.cast(value);
    }

    private static void populate(VectorSchemaRoot root, List<Row> rows) {
        int rowCount = rows.size();
        for (FieldVector vector : root.getFieldVectors()) {
            if (rowCount > 0) {
                vector.setInitialCapacity(rowCount);
                vector.allocateNew();
            }
            for (int rowIndex = 0; rowIndex < rowCount; rowIndex++) {
                Object value = rows.get(rowIndex).get(vector.getName());
                if (value == null) {
                    vector.setNull(rowIndex);
                } else {
                    writeValue(vector, rowIndex, value);
                }
            }
            vector.setValueCount(rowCount);
        }
        root.setRowCount(rowCount);
    }

    private static void writeValue(FieldVector vector, int index, Object value) {
        if (vector instanceof BitVector bits) {
            bits.setSafe(index, (Boolean) value ? 1 : 0);
        } else if (vector instanceof TinyIntVector integers) {
            integers.setSafe(index, (Byte) value);
        } else if (vector instanceof SmallIntVector integers) {
            integers.setSafe(index, (Short) value);
        } else if (vector instanceof IntVector integers) {
            integers.setSafe(index, (Integer) value);
        } else if (vector instanceof BigIntVector integers) {
            integers.setSafe(index, (Long) value);
        } else if (vector instanceof Float4Vector floats) {
            floats.setSafe(index, (Float) value);
        } else if (vector instanceof Float8Vector floats) {
            floats.setSafe(index, (Double) value);
        } else if (vector instanceof DecimalVector decimals) {
            BigDecimal decimal = ((BigDecimal) value).setScale(decimals.getScale(), RoundingMode.UNNECESSARY);
            decimals.setSafe(index, decimal);
        } else if (vector instanceof VarCharVector strings) {
            strings.setSafe(index, ((String) value).getBytes(StandardCharsets.UTF_8));
        } else if (vector instanceof VarBinaryVector binaries) {
            binaries.setSafe(index, (byte[]) value);
        } else if (vector instanceof DateDayVector dates) {
            dates.setSafe(index, Math.toIntExact(((LocalDate) value).toEpochDay()));
        } else if (vector instanceof TimeNanoVector times) {
            times.setSafe(index, ((LocalTime) value).toNanoOfDay());
        } else if (vector instanceof TimeStampNanoVector timestamps) {
            timestamps.setSafe(index, toEpochNanos(((LocalDateTime) value).toInstant(ZoneOffset.UTC)));
        } else if (vector instanceof TimeStampNanoTZVector timestamps) {
            timestamps.setSafe(index, toEpochNanos(((OffsetDateTime) value).toInstant()));
        } else {
            throw new IllegalArgumentException(
                    "unsupported Arrow vector " + vector.getClass().getName());
        }
    }

    private static Object readValue(FieldVector vector, int index) {
        if (vector.isNull(index)) {
            return null;
        }
        if (vector instanceof BitVector bits) {
            return bits.getObject(index);
        }
        if (vector instanceof TinyIntVector integers) {
            return integers.getObject(index);
        }
        if (vector instanceof SmallIntVector integers) {
            return integers.getObject(index);
        }
        if (vector instanceof IntVector integers) {
            return integers.getObject(index);
        }
        if (vector instanceof BigIntVector integers) {
            return integers.getObject(index);
        }
        if (vector instanceof Float4Vector floats) {
            return floats.getObject(index);
        }
        if (vector instanceof Float8Vector floats) {
            return floats.getObject(index);
        }
        if (vector instanceof DecimalVector decimals) {
            return decimals.getObject(index);
        }
        if (vector instanceof VarCharVector strings) {
            return strings.getObject(index).toString();
        }
        if (vector instanceof VarBinaryVector binaries) {
            return binaries.get(index);
        }
        if (vector instanceof DateDayVector dates) {
            return LocalDate.ofEpochDay(dates.get(index));
        }
        if (vector instanceof TimeNanoVector times) {
            return LocalTime.ofNanoOfDay(times.get(index));
        }
        if (vector instanceof TimeStampNanoVector timestamps) {
            return localDateTime(timestamps.get(index));
        }
        if (vector instanceof TimeStampNanoTZVector timestamps) {
            ArrowType.Timestamp type = (ArrowType.Timestamp) vector.getField().getType();
            return instant(timestamps.get(index))
                    .atZone(ZoneId.of(type.getTimezone()))
                    .toOffsetDateTime();
        }
        throw new IllegalArgumentException(
                "unsupported Arrow vector " + vector.getClass().getName());
    }

    private static long toEpochNanos(Instant instant) {
        return Math.addExact(Math.multiplyExact(instant.getEpochSecond(), 1_000_000_000L), instant.getNano());
    }

    private static Instant instant(long epochNanos) {
        long seconds = Math.floorDiv(epochNanos, 1_000_000_000L);
        int nanos = (int) Math.floorMod(epochNanos, 1_000_000_000L);
        return Instant.ofEpochSecond(seconds, nanos);
    }

    private static LocalDateTime localDateTime(long epochNanos) {
        return LocalDateTime.ofInstant(instant(epochNanos), ZoneOffset.UTC);
    }

    private static void closeAfterFailure(VectorSchemaRoot root, BufferAllocator allocator, Throwable failure) {
        if (root != null) {
            try {
                root.close();
            } catch (RuntimeException closeFailure) {
                failure.addSuppressed(closeFailure);
            }
        }
        try {
            allocator.close();
        } catch (RuntimeException closeFailure) {
            failure.addSuppressed(closeFailure);
        }
    }
}
