package io.astrasync.connector.file;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import java.io.BufferedWriter;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.StandardOpenOption;
import java.util.ArrayList;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.TreeSet;
import org.apache.commons.csv.CSVFormat;
import org.apache.commons.csv.CSVPrinter;

final class CsvBatchSink implements BatchSink {
    private final CsvConnectorOptions options;

    private State state = State.NEW;
    private CSVPrinter printer;
    private List<String> headers;
    private Set<String> headerSet;

    CsvBatchSink(CsvConnectorOptions options) {
        this.options = options;
    }

    @Override
    public void open() {
        requireState(State.NEW, "open");
        BufferedWriter writer = null;
        try {
            if (!Files.isDirectory(options.path().getParent())) {
                throw new IllegalArgumentException("CSV sink parent is not an existing directory: "
                        + options.path().getParent());
            }
            writer = Files.newBufferedWriter(
                    options.path(), StandardCharsets.UTF_8, StandardOpenOption.CREATE_NEW, StandardOpenOption.WRITE);
            printer = new CSVPrinter(writer, sinkFormat());
            state = State.OPEN;
        } catch (IOException | RuntimeException exception) {
            state = State.CLOSED;
            closeFailedOpen(writer, exception);
            throw new IllegalStateException(
                    "failed to open CSV sink '" + options.path() + "': " + exception.getMessage(), exception);
        }
    }

    @Override
    public void writeBatch(RowBatch batch) {
        requireState(State.OPEN, "write");
        Objects.requireNonNull(batch, "batch must not be null");
        if (batch.rows().isEmpty()) {
            return;
        }

        try {
            for (Row row : batch.rows()) {
                if (headers == null) {
                    establishHeaders(row);
                }
                validateColumns(row);
                printer.printRecord(encodedValues(row));
            }
            printer.flush();
        } catch (IOException exception) {
            throw new UncheckedIOException("failed to write CSV sink '" + options.path() + "'", exception);
        }
    }

    @Override
    public void close() {
        if (state == State.CLOSED) {
            return;
        }
        CSVPrinter openedPrinter = printer;
        state = State.CLOSED;
        printer = null;
        if (openedPrinter != null) {
            try {
                openedPrinter.close(true);
            } catch (IOException exception) {
                throw new UncheckedIOException("failed to close CSV sink '" + options.path() + "'", exception);
            }
        }
    }

    private void establishHeaders(Row row) throws IOException {
        if (row.size() == 0) {
            throw new IllegalArgumentException("CSV sink cannot derive a header from an empty Row");
        }
        headers = List.copyOf(row.asMap().keySet());
        headerSet = Set.copyOf(headers);
        printer.printRecord(headers);
    }

    private void validateColumns(Row row) {
        Set<String> columns = new TreeSet<>(row.asMap().keySet());
        if (!columns.equals(headerSet)) {
            throw new IllegalArgumentException(
                    "CSV sink row columns must match header columns " + headers + "; received " + columns);
        }
    }

    private List<String> encodedValues(Row row) {
        List<String> values = new ArrayList<>(headers.size());
        for (String header : headers) {
            Object value = row.get(header);
            if (value == null) {
                if (options.nullValue() == null) {
                    throw new IllegalArgumentException(
                            "CSV sink column '" + header + "' is null but option 'nullValue' is not configured");
                }
                values.add(null);
            } else if (value instanceof String stringValue) {
                values.add(stringValue);
            } else {
                throw new IllegalArgumentException(
                        "CSV sink column '" + header + "' must contain a String or null; received "
                                + value.getClass().getName());
            }
        }
        return values;
    }

    private CSVFormat sinkFormat() {
        CSVFormat.Builder builder = CSVFormat.RFC4180
                .builder()
                .setRecordSeparator("\r\n")
                .setIgnoreSurroundingSpaces(false)
                .setTrim(false);
        if (options.nullValue() != null) {
            builder.setNullString(options.nullValue());
        }
        return builder.build();
    }

    private static void closeFailedOpen(BufferedWriter writer, Exception failure) {
        if (writer == null) {
            return;
        }
        try {
            writer.close();
        } catch (IOException closeFailure) {
            failure.addSuppressed(closeFailure);
        }
    }

    private void requireState(State expected, String operation) {
        if (state != expected) {
            throw new IllegalStateException("cannot " + operation + " CSV sink while state is " + state);
        }
    }

    private enum State {
        NEW,
        OPEN,
        CLOSED
    }
}
