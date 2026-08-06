package io.astrasync.format.arrow;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import java.math.BigDecimal;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.LocalTime;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.LinkedHashMap;
import java.util.List;
import org.apache.arrow.memory.ArrowBuf;
import org.apache.arrow.memory.RootAllocator;
import org.apache.arrow.vector.types.DateUnit;
import org.apache.arrow.vector.types.TimeUnit;
import org.apache.arrow.vector.types.pojo.ArrowType;
import org.apache.arrow.vector.types.pojo.DictionaryEncoding;
import org.apache.arrow.vector.types.pojo.Field;
import org.apache.arrow.vector.types.pojo.FieldType;
import org.apache.arrow.vector.types.pojo.Schema;
import org.junit.jupiter.api.Test;

class ArrowBatchCodecTest {
    private static final long MEMORY_LIMIT = 16L * 1024 * 1024;

    @Test
    void roundTripsEverySupportedScalarWithStableTypesAndOrder() {
        OffsetDateTime zoned = OffsetDateTime.parse("2026-08-06T10:11:12.123456789+08:00");
        Row first = row(
                "boolean_value",
                true,
                "tiny_value",
                (byte) 7,
                "small_value",
                (short) 32000,
                "integer_value",
                123456,
                "big_value",
                9_000_000_000L,
                "float_value",
                1.25F,
                "double_value",
                9.5D,
                "decimal_value",
                new BigDecimal("1234.50"),
                "string_value",
                "AstraSync",
                "binary_value",
                new byte[] {0, 1, -1},
                "date_value",
                LocalDate.of(2026, 8, 6),
                "time_value",
                LocalTime.of(10, 11, 12, 123456789),
                "timestamp_value",
                LocalDateTime.of(2026, 8, 6, 10, 11, 12, 123456789),
                "zoned_value",
                zoned);
        Row second = rowWithNullValues(first);
        RowBatch input = RowBatch.last(List.of(first, second));

        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT);
                ArrowBatch arrow = ArrowBatchCodec.encode(parent, MEMORY_LIMIT, input)) {
            assertThat(arrow.size()).isEqualTo(2);
            assertThat(arrow.endOfInput()).isTrue();
            assertThat(arrow.allocatedBytes()).isPositive().isLessThanOrEqualTo(MEMORY_LIMIT);
            assertThat(arrow.schema().getFields())
                    .extracting(Field::getName)
                    .containsExactlyElementsOf(first.asMap().keySet());
            assertThat(arrow.schema().getFields()).allMatch(Field::isNullable);

            RowBatch output = arrow.toRowBatch();
            assertThat(output.endOfInput()).isTrue();
            assertThat(output.size()).isEqualTo(2);
            Row decoded = output.rows().get(0);
            assertThat(decoded.get("boolean_value")).isEqualTo(true);
            assertThat(decoded.get("tiny_value")).isEqualTo((byte) 7);
            assertThat(decoded.get("small_value")).isEqualTo((short) 32000);
            assertThat(decoded.get("integer_value")).isEqualTo(123456);
            assertThat(decoded.get("big_value")).isEqualTo(9_000_000_000L);
            assertThat(decoded.get("float_value")).isEqualTo(1.25F);
            assertThat(decoded.get("double_value")).isEqualTo(9.5D);
            assertThat((BigDecimal) decoded.get("decimal_value")).isEqualByComparingTo("1234.50");
            assertThat(decoded.get("string_value")).isEqualTo("AstraSync");
            assertThat((byte[]) decoded.get("binary_value")).containsExactly(0, 1, -1);
            assertThat(decoded.get("date_value")).isEqualTo(LocalDate.of(2026, 8, 6));
            assertThat(decoded.get("time_value")).isEqualTo(LocalTime.of(10, 11, 12, 123456789));
            assertThat(decoded.get("timestamp_value")).isEqualTo(LocalDateTime.of(2026, 8, 6, 10, 11, 12, 123456789));
            assertThat(decoded.get("zoned_value")).isEqualTo(zoned.withOffsetSameInstant(ZoneOffset.UTC));
            assertThat(output.rows().get(1).asMap().values()).containsOnlyNulls();
        }
    }

    @Test
    void explicitSchemaSupportsAllNullAndTerminalEmptyBatches() {
        Schema schema = new Schema(List.of(
                new Field("note", FieldType.nullable(ArrowType.Utf8.INSTANCE), List.of()),
                new Field("count", FieldType.nullable(new ArrowType.Int(64, true)), List.of())));

        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT)) {
            RowBatch allNull = RowBatch.last(List.of(row("note", null, "count", null)));
            try (ArrowBatch arrow = ArrowBatchCodec.encode(parent, MEMORY_LIMIT, schema, allNull)) {
                assertThat(arrow.toRowBatch().rows().get(0).asMap().values()).containsOnlyNulls();
            }
            try (ArrowBatch empty = ArrowBatchCodec.encode(parent, MEMORY_LIMIT, schema, RowBatch.end())) {
                assertThat(empty.size()).isZero();
                assertThat(empty.toRowBatch()).isEqualTo(RowBatch.end());
            }
            assertThat(parent.getAllocatedMemory()).isZero();
        }
    }

    @Test
    void decimalInferenceUsesOneExactCommonScale() {
        RowBatch input = RowBatch.data(List.of(
                Row.of("amount", new BigDecimal("123.4")),
                Row.of("amount", new BigDecimal("0.005")),
                Row.of("amount", new BigDecimal("-99999"))));

        Schema schema = ArrowBatchCodec.inferSchema(input);
        ArrowType.Decimal decimal =
                (ArrowType.Decimal) schema.findField("amount").getType();
        assertThat(decimal.getScale()).isEqualTo(3);
        assertThat(decimal.getPrecision()).isEqualTo(8);

        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT);
                ArrowBatch arrow = ArrowBatchCodec.encode(parent, MEMORY_LIMIT, schema, input)) {
            assertThat(arrow.toRowBatch().rows())
                    .extracting(row -> (BigDecimal) row.get("amount"))
                    .containsExactly(new BigDecimal("123.400"), new BigDecimal("0.005"), new BigDecimal("-99999.000"));
        }
    }

    @Test
    void inferenceRejectsEmptyAllNullMixedUnsupportedAndMismatchedRows() {
        assertThatThrownBy(() -> ArrowBatchCodec.inferSchema(RowBatch.end()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("empty batch");
        assertThatThrownBy(() -> ArrowBatchCodec.inferSchema(RowBatch.data(List.of(Row.of("value", null)))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("only nulls");
        assertThatThrownBy(() ->
                        ArrowBatchCodec.inferSchema(RowBatch.data(List.of(Row.of("value", 1), Row.of("value", 2L)))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("mixes Java value types");
        assertThatThrownBy(() -> ArrowBatchCodec.inferSchema(RowBatch.data(List.of(Row.of("value", new Object())))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("unsupported Java value type");
        assertThatThrownBy(() ->
                        ArrowBatchCodec.inferSchema(RowBatch.data(List.of(Row.of("left", 1), Row.of("right", 2)))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("do not match Arrow schema columns");
        assertThatThrownBy(() -> ArrowBatchCodec.inferSchema(RowBatch.data(
                        List.of(Row.of("amount", new BigDecimal("123456789012345678901234567890123456789"))))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("maximum is 38");
    }

    @Test
    void explicitSchemaRejectsWrongValuesAndUnsupportedArrowTypes() {
        Schema nonNullable = schema("value", FieldType.notNullable(new ArrowType.Int(32, true)));
        Schema unsigned = schema("value", FieldType.nullable(new ArrowType.Int(32, false)));
        Schema milliTime = schema("value", FieldType.nullable(new ArrowType.Time(TimeUnit.MILLISECOND, 32)));
        Schema milliDate = schema("value", FieldType.nullable(new ArrowType.Date(DateUnit.MILLISECOND)));
        Schema decimal = schema("value", FieldType.notNullable(new ArrowType.Decimal(4, 2, 128)));

        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT)) {
            assertThatThrownBy(() -> ArrowBatchCodec.encode(
                            parent, MEMORY_LIMIT, nonNullable, RowBatch.data(List.of(Row.of("value", null)))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("not nullable");
            assertThatThrownBy(() -> ArrowBatchCodec.encode(
                            parent, MEMORY_LIMIT, nonNullable, RowBatch.data(List.of(Row.of("value", 1L)))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("requires java.lang.Integer");
            assertThatThrownBy(() -> ArrowBatchCodec.encode(
                            parent,
                            MEMORY_LIMIT,
                            decimal,
                            RowBatch.data(List.of(Row.of("value", new BigDecimal("1.234"))))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("decimal scale");
            assertThatThrownBy(() -> ArrowBatchCodec.encode(
                            parent,
                            MEMORY_LIMIT,
                            decimal,
                            RowBatch.data(List.of(Row.of("value", new BigDecimal("123.45"))))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("decimal precision");
            assertThatThrownBy(() -> ArrowBatchCodec.encode(
                            parent, MEMORY_LIMIT, unsigned, RowBatch.data(List.of(Row.of("value", 1)))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("unsupported Arrow type");
            assertThatThrownBy(() -> ArrowBatchCodec.encode(
                            parent, MEMORY_LIMIT, milliTime, RowBatch.data(List.of(Row.of("value", LocalTime.NOON)))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("unsupported Arrow type");
            assertThatThrownBy(() -> ArrowBatchCodec.encode(
                            parent, MEMORY_LIMIT, milliDate, RowBatch.data(List.of(Row.of("value", LocalDate.EPOCH)))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("unsupported Arrow type");
            assertThat(parent.getAllocatedMemory()).isZero();
        }
    }

    @Test
    void schemaValidationRejectsAmbiguousStructuralFeatures() {
        Field value = new Field("value", FieldType.nullable(ArrowType.Utf8.INSTANCE), List.of());
        Schema duplicate = new Schema(List.of(value, value));
        Schema blank = new Schema(List.of(new Field(" ", FieldType.nullable(ArrowType.Utf8.INSTANCE), List.of())));
        Schema nested = new Schema(List.of(new Field(
                "nested",
                FieldType.nullable(ArrowType.Struct.INSTANCE),
                List.of(new Field("child", FieldType.nullable(ArrowType.Utf8.INSTANCE), List.of())))));
        Schema dictionary = new Schema(List.of(new Field(
                "value",
                new FieldType(
                        true, ArrowType.Utf8.INSTANCE, new DictionaryEncoding(1, false, new ArrowType.Int(32, true))),
                List.of())));
        Schema timezone = new Schema(List.of(new Field(
                "value", FieldType.nullable(new ArrowType.Timestamp(TimeUnit.NANOSECOND, "not/a-zone")), List.of())));

        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT)) {
            assertSchemaRejected(parent, duplicate, "duplicate Arrow field name");
            assertSchemaRejected(parent, blank, "must not be blank");
            assertSchemaRejected(parent, nested, "nested Arrow field is not supported");
            assertSchemaRejected(parent, dictionary, "dictionary-encoded Arrow field is not supported");
            assertSchemaRejected(parent, timezone, "invalid timezone");
            assertThat(parent.getAllocatedMemory()).isZero();
        }
    }

    @Test
    void batchCloseIsIdempotentRejectsAccessAndLeavesParentOpen() {
        RootAllocator parent = new RootAllocator(MEMORY_LIMIT);
        try {
            ArrowBatch batch =
                    ArrowBatchCodec.encode(parent, MEMORY_LIMIT, RowBatch.last(List.of(Row.of("value", "owned"))));
            assertThat(parent.getAllocatedMemory()).isPositive();

            batch.close();
            batch.close();

            assertThat(batch.isClosed()).isTrue();
            assertThat(parent.getAllocatedMemory()).isZero();
            assertThatThrownBy(batch::root)
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessage("Arrow batch is closed");
            assertThatThrownBy(batch::toRowBatch)
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessage("Arrow batch is closed");
            try (ArrowBuf buffer = parent.buffer(8)) {
                assertThat(buffer.capacity()).isEqualTo(8);
            }
        } finally {
            parent.close();
        }
    }

    @Test
    void allocationLimitFailsWithoutLeakingChildMemory() {
        String large = "x".repeat(128 * 1024);
        try (RootAllocator parent = new RootAllocator(MEMORY_LIMIT)) {
            assertThatThrownBy(
                            () -> ArrowBatchCodec.encode(parent, 1024, RowBatch.last(List.of(Row.of("value", large)))))
                    .isInstanceOf(RuntimeException.class);
            assertThat(parent.getAllocatedMemory()).isZero();
            assertThat(parent.getChildAllocators()).isEmpty();
        }
    }

    private static Schema schema(String name, FieldType fieldType) {
        return new Schema(List.of(new Field(name, fieldType, List.of())));
    }

    private static void assertSchemaRejected(RootAllocator parent, Schema schema, String message) {
        assertThatThrownBy(() -> ArrowBatchCodec.encode(parent, MEMORY_LIMIT, schema, RowBatch.end()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining(message);
    }

    private static Row row(Object... entries) {
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        for (int index = 0; index < entries.length; index += 2) {
            values.put((String) entries[index], entries[index + 1]);
        }
        return Row.of(values);
    }

    private static Row rowWithNullValues(Row row) {
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        row.asMap().keySet().forEach(name -> values.put(name, null));
        return Row.of(values);
    }
}
