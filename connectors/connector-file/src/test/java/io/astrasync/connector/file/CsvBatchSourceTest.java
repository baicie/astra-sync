package io.astrasync.connector.file;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.source.BatchSource;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class CsvBatchSourceTest {
    @TempDir
    Path tempDirectory;

    @Test
    void readsBomQuotedMultilineNullEmptyAndUnicodeValuesInBoundedBatches() throws IOException {
        Path input = write(
                "complex.csv",
                "\uFEFFid,text,nullable,empty\r\n"
                        + "1,\"hello, \"\"world\"\"\",\\N,\r\n"
                        + "2,\"line1\r\nline2 你好\",value,\r\n");
        BatchSource source = source(input, Map.of("nullValue", "\\N"));

        source.open();
        RowBatch first = source.readBatch(1);
        RowBatch second = source.readBatch(1);
        RowBatch end = source.readBatch(1);
        source.close();

        assertThat(first.size()).isEqualTo(1);
        assertThat(first.endOfInput()).isFalse();
        assertThat(first.rows().getFirst().asMap().keySet()).containsExactly("id", "text", "nullable", "empty");
        assertThat(first.rows().getFirst().get("text")).isEqualTo("hello, \"world\"");
        assertThat(first.rows().getFirst().get("nullable")).isNull();
        assertThat(first.rows().getFirst().get("empty")).isEqualTo("");
        assertThat(second.size()).isEqualTo(1);
        assertThat(second.endOfInput()).isFalse();
        assertThat(second.rows().getFirst().get("text")).isEqualTo("line1\r\nline2 你好");
        assertThat(end).isEqualTo(RowBatch.end());
    }

    @Test
    void returnsALastPartialBatchWithoutLoadingTheWholeFile() throws IOException {
        Path input = write("partial.csv", "id\r\n1\r\n2\r\n3\r\n");
        BatchSource source = source(input, Map.of());

        source.open();
        RowBatch first = source.readBatch(2);
        RowBatch last = source.readBatch(2);

        assertThat(first.size()).isEqualTo(2);
        assertThat(first.endOfInput()).isFalse();
        assertThat(last.size()).isEqualTo(1);
        assertThat(last.endOfInput()).isTrue();
        assertThatThrownBy(() -> source.readBatch(2))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is ENDED");
        source.close();
    }

    @Test
    void rejectsMissingBlankAndDuplicateHeadersDuringOpen() throws IOException {
        Path missing = write("missing-header.csv", "");
        Path blank = write("blank-header.csv", "id, \r\n1,value\r\n");
        Path duplicate = write("duplicate-header.csv", "id,id\r\n1,2\r\n");

        assertOpenFailure(missing, "header");
        assertOpenFailure(blank, "header");
        assertOpenFailure(duplicate, "header");
    }

    @Test
    void rejectsInconsistentWidthsWithRecordAndLineEvidence() throws IOException {
        Path input = write("width.csv", "id,name\r\n1,Ada\r\n2\r\n");
        BatchSource source = source(input, Map.of());
        source.open();

        assertThatThrownBy(() -> source.readBatch(10))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("width.csv", "record 2", "expected 2 fields but found 1");
        source.close();
    }

    @Test
    void rejectsLexicallyMalformedCsvWithLocationEvidence() throws IOException {
        Path input = write("lexical.csv", "id,text\r\n1,\"unterminated\r\n");
        BatchSource source = source(input, Map.of());
        source.open();

        assertThatThrownBy(() -> source.readBatch(10))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("lexical.csv", "record", "line");
        source.close();
    }

    @Test
    void validatesLifecycleBatchLimitAndRegularFileAtOpen() throws IOException {
        Path input = write("lifecycle.csv", "id\r\n1\r\n");
        BatchSource source = source(input, Map.of());

        assertThatThrownBy(() -> source.readBatch(1))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is NEW");
        source.open();
        assertThatThrownBy(() -> source.readBatch(0))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("maxRows must be positive");
        assertThatThrownBy(source::open)
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is OPEN");
        source.close();
        source.close();
        assertThatThrownBy(() -> source.readBatch(1))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is CLOSED");

        BatchSource directorySource = source(tempDirectory, Map.of());
        assertThatThrownBy(directorySource::open)
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("not a regular file");
    }

    @Test
    void headerOnlyInputProducesATerminalEmptyBatch() throws IOException {
        Path input = write("header-only.csv", "id,name\r\n");
        BatchSource source = source(input, Map.of());
        source.open();

        assertThat(source.readBatch(5)).isEqualTo(RowBatch.end());
        source.close();
    }

    @Test
    void doesNotEagerlyAllocateTheEntireRequestedMaximum() throws IOException {
        Path input = write("large-request.csv", "id\r\n1\r\n");
        BatchSource source = source(input, Map.of());
        source.open();

        assertThat(source.readBatch(Integer.MAX_VALUE).rows())
                .extracting(row -> row.get("id"))
                .containsExactly("1");
        source.close();
    }

    private void assertOpenFailure(Path path, String message) {
        BatchSource source = source(path, Map.of());
        assertThatThrownBy(source::open)
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining(path.getFileName().toString(), message);
    }

    private BatchSource source(Path path, Map<String, String> additionalOptions) {
        java.util.HashMap<String, String> options = new java.util.HashMap<>(additionalOptions);
        options.put("path", path.toString());
        return new CsvConnectorFactory().createSource(ConnectorConfiguration.of(options));
    }

    private Path write(String name, String content) throws IOException {
        Path path = tempDirectory.resolve(name);
        Files.writeString(path, content, StandardCharsets.UTF_8);
        return path;
    }
}
