package io.astrasync.connector.file;

import static io.astrasync.connector.api.Capability.BATCH_READ;
import static io.astrasync.connector.api.Capability.BATCH_WRITE;
import static io.astrasync.connector.api.ConnectorRole.SINK;
import static io.astrasync.connector.api.ConnectorRole.SOURCE;
import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.ConnectorConfiguration;
import java.nio.file.Path;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class CsvConnectorFactoryTest {
    @TempDir
    Path tempDirectory;

    private final CsvConnectorFactory factory = new CsvConnectorFactory();

    @Test
    void advertisesOnlyTheImplementedBoundedRoles() {
        assertThat(factory.descriptor().name()).isEqualTo("csv");
        assertThat(factory.descriptor().version()).isEqualTo("1.0.0");
        assertThat(factory.descriptor().roles()).containsExactlyInAnyOrder(SOURCE, SINK);
        assertThat(factory.descriptor().capabilities()).containsExactlyInAnyOrder(BATCH_READ, BATCH_WRITE);
    }

    @Test
    void creationValidatesOptionsWithoutOpeningFiles() {
        Path missingSource = tempDirectory.resolve("missing.csv");
        Path futureSink = tempDirectory.resolve("future.csv");

        assertThat(factory.createSource(configuration(missingSource))).isInstanceOf(CsvBatchSource.class);
        assertThat(factory.createSink(configuration(futureSink))).isInstanceOf(CsvBatchSink.class);
        assertThat(missingSource).doesNotExist();
        assertThat(futureSink).doesNotExist();
    }

    @Test
    void rejectsMissingBlankInvalidAndUnknownOptions() {
        assertThatThrownBy(() -> factory.createSource(ConnectorConfiguration.empty()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("missing required connector option 'path'");
        assertThatThrownBy(() -> factory.createSource(ConnectorConfiguration.of(Map.of("path", "   "))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("'path' must not be blank");
        assertThatThrownBy(() -> factory.createSource(ConnectorConfiguration.of(Map.of("path", "bad\u0000path"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("'path' must be a valid local path");
        assertThatThrownBy(() -> factory.createSink(ConnectorConfiguration.of(
                        Map.of("path", tempDirectory.resolve("out.csv").toString(), "header", "true"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("unknown CSV connector option 'header'");
    }

    @Test
    void supportsOnlyTheDocumentedFailFastPolicy() {
        Path sourcePath = tempDirectory.resolve("input.csv");
        ConnectorConfiguration explicitFail =
                ConnectorConfiguration.of(Map.of("path", sourcePath.toString(), "malformedRowPolicy", "fail"));
        ConnectorConfiguration skip =
                ConnectorConfiguration.of(Map.of("path", sourcePath.toString(), "malformedRowPolicy", "skip"));

        assertThat(factory.createSource(explicitFail)).isInstanceOf(CsvBatchSource.class);
        assertThatThrownBy(() -> factory.createSource(skip))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("supports only 'fail'");
        assertThatThrownBy(() -> factory.createSink(explicitFail))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("unknown CSV connector option 'malformedRowPolicy'");
    }

    @Test
    void internalOptionsDoNotRenderPathOrNullTokenValues() {
        Path sourcePath = tempDirectory.resolve("secret-location.csv");
        CsvConnectorOptions options = CsvConnectorOptions.source(
                ConnectorConfiguration.of(Map.of("path", sourcePath.toString(), "nullValue", "secret-null-token")));

        assertThat(options.toString())
                .contains("path", "nullValue")
                .doesNotContain("secret-location", "secret-null-token");
    }

    private static ConnectorConfiguration configuration(Path path) {
        return ConnectorConfiguration.of(Map.of("path", path.toString()));
    }
}
