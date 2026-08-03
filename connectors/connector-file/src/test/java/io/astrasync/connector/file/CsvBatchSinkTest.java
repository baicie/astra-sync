package io.astrasync.connector.file;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import java.io.IOException;
import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.LocalTime;
import java.time.OffsetDateTime;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class CsvBatchSinkTest {
    @TempDir
    Path tempDirectory;

    @Test
    void writesDeterministicRfc4180InFirstRowHeaderOrder() throws IOException {
        Path output = tempDirectory.resolve("output.csv");
        BatchSink sink = sink(output, Map.of("nullValue", "\\N"));
        Row first = row("id", "1", "text", "hello, \"world\"", "nullable", null, "empty", "");
        Row second = row("empty", "", "nullable", "value", "text", "line1\r\nline2 你好", "id", "2");

        sink.open();
        sink.writeBatch(RowBatch.data(List.of(first)));
        sink.writeBatch(RowBatch.last(List.of(second)));
        sink.close();

        assertThat(Files.readString(output, StandardCharsets.UTF_8))
                .isEqualTo("id,text,nullable,empty\r\n"
                        + "1,\"hello, \"\"world\"\"\",\\N,\r\n"
                        + "2,\"line1\r\nline2 你好\",value,\r\n");
    }

    @Test
    void refusesToModifyAnExistingTarget() throws IOException {
        Path output = tempDirectory.resolve("existing.csv");
        Files.writeString(output, "original", StandardCharsets.UTF_8);
        BatchSink sink = sink(output, Map.of());

        assertThatThrownBy(sink::open).isInstanceOf(IllegalStateException.class).hasMessageContaining("existing.csv");
        assertThat(Files.readString(output, StandardCharsets.UTF_8)).isEqualTo("original");
    }

    @Test
    void encodesJdbcScalarsDeterministically() throws IOException {
        Path output = tempDirectory.resolve("scalars.csv");
        BatchSink sink = sink(output, Map.of("nullValue", "\\N"));
        sink.open();
        sink.writeBatch(RowBatch.last(List.of(row(
                "boolean",
                true,
                "integer",
                42,
                "decimal",
                new BigDecimal("12.3400"),
                "date",
                LocalDate.of(2026, 8, 3),
                "time",
                LocalTime.of(12, 34, 56),
                "timestamp",
                LocalDateTime.of(2026, 8, 3, 12, 34, 56),
                "offset",
                OffsetDateTime.parse("2026-08-03T12:34:56+08:00"),
                "binary",
                new byte[] {1, 2, 3}))));
        sink.close();

        assertThat(Files.readString(output, StandardCharsets.UTF_8))
                .isEqualTo("boolean,integer,decimal,date,time,timestamp,offset,binary\r\n"
                        + "true,42,12.3400,2026-08-03,12:34:56,2026-08-03T12:34:56,2026-08-03T12:34:56+08:00,AQID\r\n");
    }

    @Test
    void rejectsMissingParentBeforeCreatingOutput() {
        Path output = tempDirectory.resolve("missing").resolve("output.csv");
        BatchSink sink = sink(output, Map.of());

        assertThatThrownBy(sink::open)
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("parent is not an existing directory");
        assertThat(output).doesNotExist();
    }

    @Test
    void rejectsNullWithoutTokenAndLeavesAnHonestPartialFile() throws IOException {
        Path output = tempDirectory.resolve("null.csv");
        BatchSink sink = sink(output, Map.of());
        sink.open();

        assertThatThrownBy(() -> sink.writeBatch(RowBatch.last(List.of(Row.of("value", null)))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("'nullValue' is not configured");
        sink.close();

        assertThat(Files.readString(output, StandardCharsets.UTF_8)).isEqualTo("value\r\n");
    }

    @Test
    void rejectsUnsupportedValuesAndColumnSetChanges() {
        Path typedOutput = tempDirectory.resolve("typed.csv");
        BatchSink typedSink = sink(typedOutput, Map.of());
        typedSink.open();
        assertThatThrownBy(() -> typedSink.writeBatch(RowBatch.last(List.of(Row.of("id", new Object())))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("supported scalar or null");
        typedSink.close();

        Path schemaOutput = tempDirectory.resolve("schema.csv");
        BatchSink schemaSink = sink(schemaOutput, Map.of());
        schemaSink.open();
        schemaSink.writeBatch(RowBatch.data(List.of(row("id", "1", "name", "Ada"))));
        assertThatThrownBy(() -> schemaSink.writeBatch(RowBatch.last(List.of(row("id", "2", "other", "x")))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("must match header columns");
        schemaSink.close();
    }

    @Test
    void validatesLifecycleAndTreatsTerminalEmptyBatchAsNoData() throws IOException {
        Path output = tempDirectory.resolve("empty.csv");
        BatchSink sink = sink(output, Map.of());

        assertThatThrownBy(() -> sink.writeBatch(RowBatch.end()))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is NEW");
        sink.open();
        sink.writeBatch(RowBatch.end());
        assertThatThrownBy(sink::open).isInstanceOf(IllegalStateException.class).hasMessageContaining("state is OPEN");
        sink.close();
        sink.close();
        assertThatThrownBy(() -> sink.writeBatch(RowBatch.end()))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is CLOSED");
        assertThat(Files.readString(output, StandardCharsets.UTF_8)).isEmpty();
    }

    @Test
    void rejectsAnEmptyRowAsAHeaderSource() {
        Path output = tempDirectory.resolve("empty-row.csv");
        BatchSink sink = sink(output, Map.of());
        sink.open();

        assertThatThrownBy(() -> sink.writeBatch(RowBatch.last(List.of(Row.empty()))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("empty Row");
        sink.close();
    }

    private BatchSink sink(Path path, Map<String, String> additionalOptions) {
        java.util.HashMap<String, String> options = new java.util.HashMap<>(additionalOptions);
        options.put("path", path.toString());
        return new CsvConnectorFactory().createSink(ConnectorConfiguration.of(options));
    }

    private static Row row(Object... entries) {
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        for (int index = 0; index < entries.length; index += 2) {
            values.put((String) entries[index], entries[index + 1]);
        }
        return Row.of(values);
    }
}
