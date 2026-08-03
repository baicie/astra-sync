package io.astrasync.connector.file;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.source.BatchSource;
import java.io.IOException;
import java.io.PushbackReader;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Set;
import org.apache.commons.csv.CSVFormat;
import org.apache.commons.csv.CSVParser;
import org.apache.commons.csv.CSVRecord;
import org.apache.commons.csv.DuplicateHeaderMode;

final class CsvBatchSource implements BatchSource {
    private static final int UTF_8_BOM = '\uFEFF';

    private final CsvConnectorOptions options;

    private State state = State.NEW;
    private CSVParser parser;
    private Iterator<CSVRecord> records;
    private List<String> headers = List.of();

    CsvBatchSource(CsvConnectorOptions options) {
        this.options = options;
    }

    @Override
    public void open() {
        requireState(State.NEW, "open");
        PushbackReader openedReader = null;
        CSVParser openedParser = null;
        try {
            if (!Files.isRegularFile(options.path())) {
                throw new IllegalArgumentException("CSV source is not a regular file: " + options.path());
            }
            openedReader = new PushbackReader(Files.newBufferedReader(options.path(), StandardCharsets.UTF_8), 1);
            removeBom(openedReader);
            openedParser = CSVParser.parse(openedReader, sourceFormat());
            List<String> parsedHeaders = List.copyOf(openedParser.getHeaderNames());
            validateHeaders(parsedHeaders);

            parser = openedParser;
            records = openedParser.iterator();
            headers = parsedHeaders;
            state = State.OPEN;
        } catch (IOException | RuntimeException exception) {
            state = State.CLOSED;
            closeFailedOpen(openedParser, openedReader, exception);
            throw new IllegalStateException(
                    "failed to open CSV source '" + options.path() + "': " + exception.getMessage(), exception);
        }
    }

    @Override
    public RowBatch readBatch(int maxRows) {
        requireState(State.OPEN, "read");
        if (maxRows <= 0) {
            throw new IllegalArgumentException("maxRows must be positive");
        }

        List<Row> rows = new ArrayList<>(Math.min(maxRows, 1_024));
        while (rows.size() < maxRows) {
            CSVRecord record = nextRecord();
            if (record == null) {
                state = State.ENDED;
                return RowBatch.last(rows);
            }
            if (!record.isConsistent() || record.size() != headers.size()) {
                throw malformedRecord(record, "expected " + headers.size() + " fields but found " + record.size());
            }
            LinkedHashMap<String, Object> values = new LinkedHashMap<>();
            for (int index = 0; index < headers.size(); index++) {
                values.put(headers.get(index), record.get(index));
            }
            rows.add(Row.of(values));
        }
        return RowBatch.data(rows);
    }

    @Override
    public void close() {
        if (state == State.CLOSED) {
            return;
        }
        CSVParser openedParser = parser;
        state = State.CLOSED;
        parser = null;
        records = null;
        headers = List.of();
        if (openedParser != null) {
            try {
                openedParser.close();
            } catch (IOException exception) {
                throw new UncheckedIOException("failed to close CSV source '" + options.path() + "'", exception);
            }
        }
    }

    private CSVRecord nextRecord() {
        try {
            if (!records.hasNext()) {
                return null;
            }
            return records.next();
        } catch (RuntimeException exception) {
            throw new IllegalStateException(
                    "malformed CSV source '" + options.path() + "' near record " + (parser.getRecordNumber() + 1)
                            + ", line " + parser.getCurrentLineNumber() + ": " + exception.getMessage(),
                    exception);
        }
    }

    private IllegalStateException malformedRecord(CSVRecord record, String detail) {
        return new IllegalStateException("malformed CSV source '" + options.path() + "' at record "
                + record.getRecordNumber() + ", line " + parser.getCurrentLineNumber() + ": " + detail);
    }

    private CSVFormat sourceFormat() {
        CSVFormat.Builder builder = CSVFormat.RFC4180
                .builder()
                .setHeader()
                .setSkipHeaderRecord(true)
                .setAllowMissingColumnNames(false)
                .setDuplicateHeaderMode(DuplicateHeaderMode.DISALLOW)
                .setIgnoreEmptyLines(false)
                .setIgnoreSurroundingSpaces(false)
                .setTrim(false)
                .setLenientEof(false)
                .setTrailingData(false);
        if (options.nullValue() != null) {
            builder.setNullString(options.nullValue());
        }
        return builder.build();
    }

    private static void removeBom(PushbackReader reader) throws IOException {
        int first = reader.read();
        if (first >= 0 && first != UTF_8_BOM) {
            reader.unread(first);
        }
    }

    private static void validateHeaders(List<String> headers) {
        if (headers.isEmpty()) {
            throw new IllegalArgumentException("CSV source must contain a header record");
        }
        Set<String> names = new HashSet<>();
        for (String header : headers) {
            if (header.isBlank()) {
                throw new IllegalArgumentException("CSV header names must not be blank");
            }
            if (!names.add(header)) {
                throw new IllegalArgumentException("duplicate CSV header name: " + header);
            }
        }
    }

    private static void closeFailedOpen(CSVParser parser, PushbackReader reader, Exception failure) {
        try {
            if (parser != null) {
                parser.close();
            } else if (reader != null) {
                reader.close();
            }
        } catch (IOException closeFailure) {
            failure.addSuppressed(closeFailure);
        }
    }

    private void requireState(State expected, String operation) {
        if (state != expected) {
            throw new IllegalStateException("cannot " + operation + " CSV source while state is " + state);
        }
    }

    private enum State {
        NEW,
        OPEN,
        ENDED,
        CLOSED
    }
}
