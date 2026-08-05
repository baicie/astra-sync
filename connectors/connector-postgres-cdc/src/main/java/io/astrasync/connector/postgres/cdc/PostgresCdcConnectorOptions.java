package io.astrasync.connector.postgres.cdc;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.debezium.DebeziumIdentities;
import java.time.Duration;
import java.util.HashSet;
import java.util.Map;
import java.util.Objects;
import java.util.Properties;
import java.util.Set;
import java.util.regex.Pattern;

final class PostgresCdcConnectorOptions {
    private static final Set<String> ALLOWED = Set.of(
            "hostname",
            "port",
            "username",
            "password",
            "database",
            "schemas",
            "tables",
            "topicPrefix",
            "slotName",
            "publicationName",
            "snapshotMode",
            "maxBatchSize",
            "maxQueueSize",
            "pollIntervalMillis",
            "heartbeatIntervalMillis",
            "queuedBatches",
            "offsetCommitTimeoutMillis",
            "sslMode");
    private static final Set<String> SNAPSHOT_MODES = Set.of("initial", "always", "never", "initial_only");
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
            "database.dbname",
            "schema.include.list",
            "table.include.list",
            "slot.name",
            "publication.name");
    private static final Pattern TOPIC_PREFIX = Pattern.compile("[A-Za-z0-9][A-Za-z0-9._-]{0,126}");
    private static final Pattern REPLICATION_IDENTIFIER = Pattern.compile("[a-z][a-z0-9_]{0,62}");

    private final Properties properties;
    private final String identity;
    private final int queuedBatches;
    private final Duration commitTimeout;

    private PostgresCdcConnectorOptions(
            Properties properties, String identity, int queuedBatches, Duration commitTimeout) {
        this.properties = properties;
        this.identity = identity;
        this.queuedBatches = queuedBatches;
        this.commitTimeout = commitTimeout;
    }

    static PostgresCdcConnectorOptions from(ConnectorConfiguration configuration) {
        Objects.requireNonNull(configuration, "configuration must not be null");
        rejectUnknown(configuration);
        String hostname = required(configuration, "hostname");
        int port = boundedInt(configuration, "port", 5432, 1, 65_535);
        String username = required(configuration, "username");
        String password = configuration.required("password");
        String database = required(configuration, "database");
        String schemas = configuration.optional("schemas").orElse("public");
        String tables = required(configuration, "tables");
        String topicPrefix = required(configuration, "topicPrefix");
        if (!TOPIC_PREFIX.matcher(topicPrefix).matches()) {
            throw new IllegalArgumentException("connector option 'topicPrefix' has an invalid Debezium topic prefix");
        }
        String slotName = required(configuration, "slotName");
        String publicationName = required(configuration, "publicationName");
        validateReplicationIdentifier("slotName", slotName);
        validateReplicationIdentifier("publicationName", publicationName);
        String snapshotMode = configuration.optional("snapshotMode").orElse("initial");
        if (!SNAPSHOT_MODES.contains(snapshotMode)) {
            throw new IllegalArgumentException(
                    "connector option 'snapshotMode' must be initial, always, never, or initial_only");
        }
        int maxBatchSize = boundedInt(configuration, "maxBatchSize", 2_048, 1, Integer.MAX_VALUE);
        int maxQueueSize = boundedInt(configuration, "maxQueueSize", 8_192, 2, Integer.MAX_VALUE);
        if (maxQueueSize <= maxBatchSize) {
            throw new IllegalArgumentException("connector option 'maxQueueSize' must be greater than maxBatchSize");
        }
        int queuedBatches = boundedInt(configuration, "queuedBatches", 1, 1, 1_024);
        int commitTimeoutMillis = boundedInt(configuration, "offsetCommitTimeoutMillis", 30_000, 1, 600_000);

        Properties properties = new Properties();
        properties.setProperty("name", "astrasync-postgres-" + topicPrefix.replace('.', '-'));
        properties.setProperty("connector.class", "io.debezium.connector.postgresql.PostgresConnector");
        properties.setProperty("plugin.name", "pgoutput");
        properties.setProperty("topic.prefix", topicPrefix);
        properties.setProperty("database.hostname", hostname);
        properties.setProperty("database.port", Integer.toString(port));
        properties.setProperty("database.user", username);
        properties.setProperty("database.password", password);
        properties.setProperty("database.dbname", database);
        properties.setProperty("schema.include.list", schemas);
        properties.setProperty("table.include.list", tables);
        properties.setProperty("slot.name", slotName);
        properties.setProperty("slot.drop.on.stop", "false");
        properties.setProperty("publication.name", publicationName);
        properties.setProperty("publication.autocreate.mode", "filtered");
        properties.setProperty("snapshot.mode", snapshotMode);
        properties.setProperty("provide.transaction.metadata", "true");
        properties.setProperty("tombstones.on.delete", "false");
        properties.setProperty("max.batch.size", Integer.toString(maxBatchSize));
        properties.setProperty("max.queue.size", Integer.toString(maxQueueSize));
        properties.setProperty(
                "poll.interval.ms", Integer.toString(boundedInt(configuration, "pollIntervalMillis", 500, 1, 60_000)));
        properties.setProperty(
                "heartbeat.interval.ms",
                Integer.toString(boundedInt(configuration, "heartbeatIntervalMillis", 10_000, 0, 3_600_000)));
        configuration.optional("sslMode").ifPresent(value -> properties.setProperty("database.sslmode", value));
        applyAdvanced(configuration, properties);

        String identity = DebeziumIdentities.forConfiguration(
                PostgresCdcConnectorFactory.CONNECTOR_NAME,
                Map.of(
                        "hostname", hostname,
                        "port", Integer.toString(port),
                        "database", database,
                        "schemas", schemas,
                        "tables", tables,
                        "topicPrefix", topicPrefix,
                        "slotName", slotName,
                        "publicationName", publicationName));
        return new PostgresCdcConnectorOptions(
                properties, identity, queuedBatches, Duration.ofMillis(commitTimeoutMillis));
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

    @Override
    public String toString() {
        return "PostgresCdcConnectorOptions{propertyKeys=" + properties.stringPropertyNames() + '}';
    }

    private static void rejectUnknown(ConnectorConfiguration configuration) {
        Set<String> unknown = new HashSet<>(configuration.asMap().keySet());
        unknown.removeAll(ALLOWED);
        unknown.removeIf(key -> key.startsWith("debezium."));
        if (!unknown.isEmpty()) {
            throw new IllegalArgumentException("unknown PostgreSQL CDC connector option '"
                    + unknown.iterator().next() + "'");
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

    private static void validateReplicationIdentifier(String key, String value) {
        if (!REPLICATION_IDENTIFIER.matcher(value).matches()) {
            throw new IllegalArgumentException(
                    "connector option '" + key + "' must be a lowercase PostgreSQL identifier");
        }
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
}
