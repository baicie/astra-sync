package io.astrasync.connector.file;

import io.astrasync.connector.api.ConnectorConfiguration;
import java.nio.file.InvalidPathException;
import java.nio.file.Path;
import java.util.Objects;
import java.util.Set;

record CsvConnectorOptions(Path path, String nullValue) {
    private static final String PATH = "path";
    private static final String NULL_VALUE = "nullValue";
    private static final String MALFORMED_ROW_POLICY = "malformedRowPolicy";
    private static final String FAIL_POLICY = "fail";

    CsvConnectorOptions {
        path = Objects.requireNonNull(path, "path must not be null");
    }

    static CsvConnectorOptions source(ConnectorConfiguration configuration) {
        Objects.requireNonNull(configuration, "configuration must not be null");
        rejectUnknown(configuration, Set.of(PATH, NULL_VALUE, MALFORMED_ROW_POLICY));
        String policy = configuration.optional(MALFORMED_ROW_POLICY).orElse(FAIL_POLICY);
        if (!FAIL_POLICY.equals(policy)) {
            throw new IllegalArgumentException("connector option 'malformedRowPolicy' supports only 'fail' in Phase 0");
        }
        return common(configuration);
    }

    static CsvConnectorOptions sink(ConnectorConfiguration configuration) {
        Objects.requireNonNull(configuration, "configuration must not be null");
        rejectUnknown(configuration, Set.of(PATH, NULL_VALUE));
        return common(configuration);
    }

    private static CsvConnectorOptions common(ConnectorConfiguration configuration) {
        String configuredPath = configuration.required(PATH);
        if (configuredPath.isBlank()) {
            throw new IllegalArgumentException("connector option 'path' must not be blank");
        }
        try {
            Path path = Path.of(configuredPath).toAbsolutePath().normalize();
            return new CsvConnectorOptions(
                    path, configuration.optional(NULL_VALUE).orElse(null));
        } catch (InvalidPathException exception) {
            throw new IllegalArgumentException("connector option 'path' must be a valid local path", exception);
        }
    }

    private static void rejectUnknown(ConnectorConfiguration configuration, Set<String> allowed) {
        for (String key : configuration.asMap().keySet()) {
            if (!allowed.contains(key)) {
                throw new IllegalArgumentException("unknown CSV connector option '" + key + "'");
            }
        }
    }

    @Override
    public String toString() {
        return "CsvConnectorOptions{keys=[path" + (nullValue == null ? "" : ", nullValue") + "]}";
    }
}
