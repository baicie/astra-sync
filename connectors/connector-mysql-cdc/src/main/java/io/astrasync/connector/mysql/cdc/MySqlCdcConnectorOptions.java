package io.astrasync.connector.mysql.cdc;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.debezium.DebeziumIdentities;
import java.nio.file.Path;
import java.time.Duration;
import java.util.HashSet;
import java.util.Map;
import java.util.Objects;
import java.util.Properties;
import java.util.Set;
import java.util.regex.Pattern;

final class MySqlCdcConnectorOptions {
    private static final Set<String> ALLOWED = Set.of(
            "hostname",
            "port",
            "username",
            "password",
            "database",
            "tables",
            "topicPrefix",
            "serverId",
            "snapshotMode",
            "schemaHistoryFile",
            "maxBatchSize",
            "maxQueueSize",
            "pollIntervalMillis",
            "heartbeatIntervalMillis",
            "queuedBatches",
            "offsetCommitTimeoutMillis",
            "sslMode");
    private static final Set<String> SNAPSHOT_MODES = Set.of("initial", "when_needed", "never");
    private static final Set<String> PROTECTED_DEBEZIUM_PROPERTIES = Set.of(
            "connector.class",
            "name",
            "topic.prefix",
            "offset.storage",
            "offset.flush.interval.ms",
            "database.hostname",
            "database.port",
            "database.user",
            "database.password",
            "database.server.id",
            "database.include.list",
            "table.include.list",
            "schema.history.internal",
            "schema.history.internal.file.filename");
    private static final Pattern TOPIC_PREFIX = Pattern.compile("[A-Za-z0-9][A-Za-z0-9._-]{0,126}");

    private final Properties properties;
    private final String identity;
    private final int queuedBatches;
    private final Duration commitTimeout;
    private final Path schemaHistoryFile;

    private MySqlCdcConnectorOptions(
            Properties properties, String identity, int queuedBatches, Duration commitTimeout, Path schemaHistoryFile) {
        this.properties = properties;
        this.identity = identity;
        this.queuedBatches = queuedBatches;
        this.commitTimeout = commitTimeout;
        this.schemaHistoryFile = schemaHistoryFile;
    }

    static MySqlCdcConnectorOptions from(ConnectorConfiguration configuration) {
        Objects.requireNonNull(configuration, "configuration must not be null");
        rejectUnknown(configuration);
        String hostname = required(configuration, "hostname");
        int port = boundedInt(configuration, "port", 3306, 1, 65_535);
        String username = required(configuration, "username");
        String password = configuration.required("password");
        String database = required(configuration, "database");
        String tables = required(configuration, "tables");
        String topicPrefix = required(configuration, "topicPrefix");
        if (!TOPIC_PREFIX.matcher(topicPrefix).matches()) {
            throw new IllegalArgumentException("connector option 'topicPrefix' has an invalid Debezium topic prefix");
        }
        long serverId = boundedLong(configuration, "serverId", 1, 4_294_967_295L);
        String snapshotMode = configuration.optional("snapshotMode").orElse("initial");
        if (!SNAPSHOT_MODES.contains(snapshotMode)) {
            throw new IllegalArgumentException(
                    "connector option 'snapshotMode' must be initial, when_needed, or never");
        }
        Path historyFile = Path.of(required(configuration, "schemaHistoryFile"))
                .toAbsolutePath()
                .normalize();
        int maxBatchSize = boundedInt(configuration, "maxBatchSize", 2_048, 1, Integer.MAX_VALUE);
        int maxQueueSize = boundedInt(configuration, "maxQueueSize", 8_192, 2, Integer.MAX_VALUE);
        if (maxQueueSize <= maxBatchSize) {
            throw new IllegalArgumentException("connector option 'maxQueueSize' must be greater than maxBatchSize");
        }
        int queuedBatches = boundedInt(configuration, "queuedBatches", 1, 1, 1_024);
        long commitTimeoutMillis = boundedLong(configuration, "offsetCommitTimeoutMillis", 30_000, 1, 600_000);

        Properties properties = new Properties();
        properties.setProperty("name", "astrasync-mysql-" + topicPrefix.replace('.', '-'));
        properties.setProperty("connector.class", "io.debezium.connector.mysql.MySqlConnector");
        properties.setProperty("topic.prefix", topicPrefix);
        properties.setProperty("database.hostname", hostname);
        properties.setProperty("database.port", Integer.toString(port));
        properties.setProperty("database.user", username);
        properties.setProperty("database.password", password);
        properties.setProperty("database.server.id", Long.toString(serverId));
        properties.setProperty("database.include.list", database);
        properties.setProperty("table.include.list", tables);
        properties.setProperty("snapshot.mode", snapshotMode);
        properties.setProperty("snapshot.locking.mode", "minimal");
        properties.setProperty("schema.history.internal", "io.debezium.storage.file.history.FileSchemaHistory");
        properties.setProperty("schema.history.internal.file.filename", historyFile.toString());
        properties.setProperty("include.schema.changes", "true");
        properties.setProperty("provide.transaction.metadata", "true");
        properties.setProperty("tombstones.on.delete", "false");
        properties.setProperty("max.batch.size", Integer.toString(maxBatchSize));
        properties.setProperty("max.queue.size", Integer.toString(maxQueueSize));
        properties.setProperty(
                "poll.interval.ms", Integer.toString(boundedInt(configuration, "pollIntervalMillis", 500, 1, 60_000)));
        properties.setProperty(
                "heartbeat.interval.ms",
                Integer.toString(boundedInt(configuration, "heartbeatIntervalMillis", 10_000, 0, 3_600_000)));
        configuration.optional("sslMode").ifPresent(value -> properties.setProperty("database.ssl.mode", value));
        applyAdvanced(configuration, properties);

        String identity = DebeziumIdentities.forConfiguration(
                MySqlCdcConnectorFactory.CONNECTOR_NAME,
                Map.of(
                        "hostname", hostname,
                        "port", Integer.toString(port),
                        "database", database,
                        "tables", tables,
                        "topicPrefix", topicPrefix,
                        "serverId", Long.toString(serverId)));
        return new MySqlCdcConnectorOptions(
                properties, identity, queuedBatches, Duration.ofMillis(commitTimeoutMillis), historyFile);
    }

    Properties properties() {
        Properties copy = new Properties();
        copy.putAll(properties);
        return copy;
    }

    String identity() {
        return identity;
    }

    int queuedBatches() {
        return queuedBatches;
    }

    Duration commitTimeout() {
        return commitTimeout;
    }

    Path schemaHistoryFile() {
        return schemaHistoryFile;
    }

    @Override
    public String toString() {
        return "MySqlCdcConnectorOptions{propertyKeys=" + properties.stringPropertyNames() + '}';
    }

    private static void rejectUnknown(ConnectorConfiguration configuration) {
        Set<String> unknown = new HashSet<>(configuration.asMap().keySet());
        unknown.removeAll(ALLOWED);
        unknown.removeIf(key -> key.startsWith("debezium."));
        if (!unknown.isEmpty()) {
            throw new IllegalArgumentException(
                    "unknown MySQL CDC connector option '" + unknown.iterator().next() + "'");
        }
    }

    private static void applyAdvanced(ConnectorConfiguration configuration, Properties properties) {
        configuration.asMap().forEach((key, value) -> {
            if (!key.startsWith("debezium.")) {
                return;
            }
            String debeziumKey = key.substring("debezium.".length());
            if (debeziumKey.isBlank() || PROTECTED_DEBEZIUM_PROPERTIES.contains(debeziumKey)) {
                throw new IllegalArgumentException("Debezium property cannot be overridden: " + debeziumKey);
            }
            properties.setProperty(debeziumKey, value);
        });
    }

    private static String required(ConnectorConfiguration configuration, String key) {
        String value = configuration.required(key);
        if (value.isBlank()) {
            throw new IllegalArgumentException("connector option '" + key + "' must not be blank");
        }
        return value;
    }

    private static int boundedInt(
            ConnectorConfiguration configuration, String key, int defaultValue, int minimum, int maximum) {
        int value = configuration.contains(key) ? configuration.getInt(key) : defaultValue;
        if (value < minimum || value > maximum) {
            throw new IllegalArgumentException(
                    "connector option '" + key + "' must be between " + minimum + " and " + maximum);
        }
        return value;
    }

    private static long boundedLong(ConnectorConfiguration configuration, String key, long minimum, long maximum) {
        return boundedLong(configuration, key, null, minimum, maximum);
    }

    private static long boundedLong(
            ConnectorConfiguration configuration, String key, long defaultValue, long minimum, long maximum) {
        return boundedLong(configuration, key, Long.valueOf(defaultValue), minimum, maximum);
    }

    private static long boundedLong(
            ConnectorConfiguration configuration, String key, Long defaultValue, long minimum, long maximum) {
        String raw = configuration.optional(key).orElse(defaultValue == null ? null : defaultValue.toString());
        if (raw == null) {
            throw new IllegalArgumentException("missing required connector option '" + key + "'");
        }
        long value;
        try {
            value = Long.parseLong(raw);
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("connector option '" + key + "' must be an integer", exception);
        }
        if (value < minimum || value > maximum) {
            throw new IllegalArgumentException(
                    "connector option '" + key + "' must be between " + minimum + " and " + maximum);
        }
        return value;
    }
}
