package io.astrasync.engine.jobspec;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import org.junit.jupiter.api.Test;

class JobSpecParserTest {
    private final JobSpecParser parser = new JobSpecParser();

    @Test
    void parsesEquivalentYamlAndJsonIntoTheSameNormalizedSpec() {
        String yaml =
                """
                apiVersion: sync.astrasync.io/v1
                kind: SyncJob
                metadata:
                  name: local-copy
                spec:
                  source:
                    connector: csv
                    options:
                      zeta: last
                      alpha: first
                  transforms: []
                  sink:
                    connector: jdbc
                    options:
                      table: target_table
                  delivery:
                    guarantee: at-most-once
                  runtime:
                    maxBatchRecords: 128
                """;
        String json =
                """
                {
                  "apiVersion": "sync.astrasync.io/v1",
                  "kind": "SyncJob",
                  "metadata": {"name": "local-copy"},
                  "spec": {
                    "source": {
                      "connector": "csv",
                      "options": {"alpha": "first", "zeta": "last"}
                    },
                    "transforms": [],
                    "sink": {"connector": "jdbc", "options": {"table": "target_table"}},
                    "delivery": {"guarantee": "at-most-once"},
                    "runtime": {"maxBatchRecords": 128}
                  }
                }
                """;

        JobSpec yamlSpec = parser.parse(yaml);
        JobSpec jsonSpec = parser.parse(json);

        assertThat(yamlSpec).isEqualTo(jsonSpec);
        assertThat(new ArrayList<>(yamlSpec.spec().source().options().keySet())).containsExactly("alpha", "zeta");
        assertThatThrownBy(() -> yamlSpec.spec().source().options().put("other", "value"))
                .isInstanceOf(UnsupportedOperationException.class);
        assertThat(yamlSpec.toString()).contains("alpha", "zeta").doesNotContain("first", "last");
    }

    @Test
    void appliesOnlyTheDocumentedDefaults() {
        JobSpec jobSpec = parser.parse(minimalSpec("at-most-once"));

        assertThat(jobSpec.spec().transforms()).isEmpty();
        assertThat(jobSpec.spec().runtime().maxBatchRecords()).isEqualTo(RuntimeSpec.DEFAULT_MAX_BATCH_RECORDS);
        assertThat(jobSpec.spec().runtime().adaptiveBatch().enabled()).isFalse();
        assertThat(jobSpec.spec().runtime().adaptiveParallelism().enabled()).isFalse();
        assertThat(jobSpec.spec().source().options()).isEmpty();
    }

    @Test
    void parsesAdaptiveBatchAndParallelismSettings() {
        JobSpec jobSpec = parser.parse(minimalSpec("at-most-once")
                .replace(
                        "    guarantee: at-most-once",
                        "    guarantee: at-most-once\n"
                                + "  runtime:\n"
                                + "    maxBatchRecords: 128\n"
                                + "    adaptiveBatch:\n"
                                + "      minBatchRecords: 8\n"
                                + "      initialBatchRecords: 32\n"
                                + "      targetBatchNanos: 1000000\n"
                                + "      adjustmentCooldownSamples: 2\n"
                                + "    adaptiveParallelism:\n"
                                + "      minParallelism: 1\n"
                                + "      initialParallelism: 2\n"
                                + "      maxParallelism: 4\n"
                                + "      targetTaskNanos: 2000000\n"
                                + "      adjustmentCooldownSamples: 3"));

        assertThat(jobSpec.spec().runtime().adaptiveBatch())
                .extracting(
                        AdaptiveBatchSpec::minBatchRecords,
                        AdaptiveBatchSpec::initialBatchRecords,
                        AdaptiveBatchSpec::targetBatchNanos,
                        AdaptiveBatchSpec::adjustmentCooldownSamples)
                .containsExactly(8, 32, 1_000_000L, 2);
        assertThat(jobSpec.spec().runtime().adaptiveParallelism())
                .extracting(
                        AdaptiveParallelismSpec::minParallelism,
                        AdaptiveParallelismSpec::initialParallelism,
                        AdaptiveParallelismSpec::maxParallelism,
                        AdaptiveParallelismSpec::targetTaskNanos,
                        AdaptiveParallelismSpec::adjustmentCooldownSamples)
                .containsExactly(1, 2, 4, 2_000_000L, 3);
    }

    @Test
    void rejectsMalformedAdaptiveSettings() {
        assertFailure(
                minimalSpec("at-most-once")
                        .replace(
                                "    guarantee: at-most-once",
                                "    guarantee: at-most-once\n"
                                        + "  runtime:\n"
                                        + "    maxBatchRecords: 4\n"
                                        + "    adaptiveBatch:\n"
                                        + "      minBatchRecords: 8\n"
                                        + "      initialBatchRecords: 8\n"
                                        + "      targetBatchNanos: 1\n"
                                        + "      adjustmentCooldownSamples: 0"),
                "$.spec.runtime.adaptiveBatch",
                "maxBatchRecords");
        assertFailure(
                minimalSpec("at-most-once")
                        .replace(
                                "    guarantee: at-most-once",
                                "    guarantee: at-most-once\n"
                                        + "  runtime:\n"
                                        + "    adaptiveParallelism:\n"
                                        + "      minParallelism: 2\n"
                                        + "      initialParallelism: 1\n"
                                        + "      maxParallelism: 2\n"
                                        + "      targetTaskNanos: 1\n"
                                        + "      adjustmentCooldownSamples: 0"),
                "$.spec.runtime.adaptiveParallelism",
                "initialParallelism");
        assertFailure(
                minimalSpec("at-most-once")
                        .replace(
                                "    guarantee: at-most-once",
                                "    guarantee: at-most-once\n"
                                        + "  runtime:\n"
                                        + "    adaptiveBatch:\n"
                                        + "      minBatchRecords: 1\n"
                                        + "      initialBatchRecords: 1\n"
                                        + "      targetBatchNanos: 1\n"
                                        + "      adjustmentCooldownSamples: 0\n"
                                        + "      unexpected: true"),
                "$.spec.runtime.adaptiveBatch.unexpected",
                "unknown field");
    }

    @Test
    void rejectsUnknownFieldsWithTheirPath() {
        assertFailure(
                minimalSpec("at-most-once").replace("connector: csv", "connector: csv\n    conector: typo"),
                "$.spec.source.conector",
                "unknown field");
    }

    @Test
    void rejectsDuplicateFieldsWithTheirPath() {
        assertFailure(
                minimalSpec("at-most-once").replace("connector: csv", "connector: csv\n    connector: duplicate"),
                "$.spec.source.connector",
                "duplicate field");
    }

    @Test
    void rejectsUnsupportedVersionAndKind() {
        assertFailure(
                minimalSpec("at-most-once").replace(JobSpec.API_VERSION, "sync.astrasync.io/v2"),
                "$.apiVersion",
                "unsupported apiVersion");
        assertFailure(
                minimalSpec("at-most-once").replace("kind: SyncJob", "kind: OtherJob"), "$.kind", "unsupported kind");
    }

    @Test
    void rejectsConnectorNamesOutsideTheCanonicalDescriptorSyntax() {
        assertFailure(
                minimalSpec("at-most-once").replace("connector: csv", "connector: csv-"),
                "$.spec.source.connector",
                "invalid canonical connector name");
        assertFailure(
                minimalSpec("at-most-once").replace("connector: csv", "connector: " + "a".repeat(129)),
                "$.spec.source.connector",
                "invalid canonical connector name");
    }

    @Test
    void rejectsTypedConnectorOptionsAndScalarCoercion() {
        assertFailure(
                minimalSpec("at-most-once")
                        .replace("connector: csv", "connector: csv\n    options:\n      header: true"),
                "$.spec.source.options.header",
                "option value must be a string");
        assertFailure(
                minimalSpec("at-most-once")
                        .replace(
                                "  delivery:\n    guarantee",
                                "  runtime:\n    maxBatchRecords: '10'\n  delivery:\n    guarantee"),
                "$.spec.runtime.maxBatchRecords",
                "must be a 32-bit integer");
    }

    @Test
    void rejectsMissingRequiredFieldsAndInvalidNames() {
        assertFailure(
                minimalSpec("at-most-once").replace("metadata:\n  name: local-copy", "metadata: {}"),
                "$.metadata.name",
                "is required");
        assertFailure(
                minimalSpec("at-most-once").replace("local-copy", "Local_Copy"),
                "$.metadata.name",
                "lowercase DNS label");
        assertFailure(
                minimalSpec("at-most-once").replace("local-copy", "local-copy-"),
                "$.metadata.name",
                "lowercase DNS label");

        assertThat(parser.parse(minimalSpec("at-most-once").replace("local-copy", "0-local-copy")))
                .extracting(jobSpec -> jobSpec.metadata().name())
                .isEqualTo("0-local-copy");
    }

    @Test
    void rejectsMultipleDocuments() {
        assertFailure(minimalSpec("at-most-once") + "\n---\n" + minimalSpec("at-most-once"), "$", "multiple documents");
    }

    private void assertFailure(String document, String path, String message) {
        assertThatThrownBy(() -> parser.parse(document))
                .isInstanceOfSatisfying(JobSpecParseException.class, exception -> {
                    assertThat(exception.path()).isEqualTo(path);
                    assertThat(exception).hasMessageContaining(message);
                });
    }

    private static String minimalSpec(String guarantee) {
        return """
                apiVersion: sync.astrasync.io/v1
                kind: SyncJob
                metadata:
                  name: local-copy
                spec:
                  source:
                    connector: csv
                  sink:
                    connector: csv
                  delivery:
                    guarantee: %s
                """
                .formatted(guarantee);
    }
}
