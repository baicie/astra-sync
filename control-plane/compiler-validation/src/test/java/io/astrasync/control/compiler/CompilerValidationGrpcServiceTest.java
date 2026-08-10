package io.astrasync.control.compiler;

import static org.assertj.core.api.Assertions.assertThat;

import io.astrasync.compiler.v1.CompilerDeliveryGuarantee;
import io.astrasync.compiler.v1.CompilerExecutionMode;
import io.astrasync.compiler.v1.CompilerTransform;
import io.astrasync.compiler.v1.CompilerValidationIssueCode;
import io.astrasync.compiler.v1.EffectiveConnectorConfig;
import io.astrasync.compiler.v1.ValidateRequest;
import io.astrasync.compiler.v1.ValidateResponse;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.file.CsvConnectorFactory;
import io.astrasync.connector.jdbc.JdbcConnectorFactory;
import io.astrasync.connector.mysql.cdc.MySqlCdcConnectorFactory;
import io.astrasync.connector.postgres.cdc.PostgresCdcConnectorFactory;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.plan.ConnectorRegistry;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

class CompilerValidationGrpcServiceTest {
    private ConnectorRegistry registry;
    private CompilerValidationGrpcService service;

    @BeforeEach
    void setUp() {
        registry = ConnectorRegistry.of(
                new CsvConnectorFactory(),
                new JdbcConnectorFactory(),
                new MySqlCdcConnectorFactory(),
                new PostgresCdcConnectorFactory());
        service = new CompilerValidationGrpcService(registry, JobSpec.API_VERSION, "test-build", "standard", 4);
    }

    @Test
    void compilesValidBatchSpecUsingTheRuntimeCompiler() {
        ValidateResponse result = service.validateRequest(ValidateRequest.newBuilder()
                .setName("csv-copy")
                .setSource(EffectiveConnectorConfig.newBuilder()
                        .setConnector("csv")
                        .putJobOptions("path", "input.csv"))
                .setSink(EffectiveConnectorConfig.newBuilder()
                        .setConnector("csv")
                        .putJobOptions("path", "output.csv"))
                .setDeliveryGuarantee(CompilerDeliveryGuarantee.COMPILER_DELIVERY_GUARANTEE_AT_MOST_ONCE)
                .setMaxBatchRecords(100)
                .setExecutionProfile("standard")
                .build());

        assertThat(result.getValid()).isTrue();
        assertThat(result.getIssuesList()).isEmpty();
        assertThat(result.getExecutionMode()).isEqualTo(CompilerExecutionMode.COMPILER_EXECUTION_MODE_BATCH);
        assertThat(result.getCompilerRevision()).startsWith("sha256:");
        assertThat(result.getInventoryRevision()).startsWith("sha256:");
    }

    @Test
    void compilesCdcSpecWithOnlySecretPresenceMetadata() {
        ConnectorDescriptor mysql = descriptor("mysql-cdc");
        ConnectorDescriptor jdbc = descriptor("jdbc");
        EffectiveConnectorConfig source = EffectiveConnectorConfig.newBuilder()
                .setConnector(mysql.name())
                .setConnectionConfigured(true)
                .setDescriptorRevision(mysql.descriptorRevision())
                .setConnectionSchemaRevision(mysql.connectionSchemaRevision())
                .putConnectionSettings("hostname", "db.internal")
                .putConnectionSettings("database", "orders")
                .addConfiguredSecretFields("password")
                .addConfiguredSecretFields("username")
                .putJobOptions("tables", "orders")
                .putJobOptions("topicPrefix", "orders-cdc")
                .putJobOptions("serverId", "1001")
                .putJobOptions("schemaHistoryFile", "history.dat")
                .build();
        EffectiveConnectorConfig sink = EffectiveConnectorConfig.newBuilder()
                .setConnector(jdbc.name())
                .setConnectionConfigured(true)
                .setDescriptorRevision(jdbc.descriptorRevision())
                .setConnectionSchemaRevision(jdbc.connectionSchemaRevision())
                .putConnectionSettings("url", "jdbc:postgresql://db.internal/orders")
                .build();

        ValidateResponse result = service.validateRequest(ValidateRequest.newBuilder()
                .setName("orders-cdc")
                .setSource(source)
                .setSink(sink)
                .setDeliveryGuarantee(CompilerDeliveryGuarantee.COMPILER_DELIVERY_GUARANTEE_EXACTLY_ONCE)
                .setMaxBatchRecords(1_024)
                .setExecutionProfile("standard")
                .build());

        assertThat(result.getValid()).isTrue();
        assertThat(result.getExecutionMode()).isEqualTo(CompilerExecutionMode.COMPILER_EXECUTION_MODE_CDC);
    }

    @Test
    void rejectsRawSensitiveJobOptionWithoutEchoingItsValue() {
        String sentinel = "secret-sentinel-do-not-echo";
        ConnectorDescriptor jdbc = descriptor("jdbc");
        EffectiveConnectorConfig source = EffectiveConnectorConfig.newBuilder()
                .setConnector(jdbc.name())
                .setConnectionConfigured(true)
                .setConnectionSchemaRevision(jdbc.connectionSchemaRevision())
                .putConnectionSettings("url", "jdbc:postgresql://db.internal/orders")
                .putJobOptions("query", "select-one")
                .putJobOptions("password", sentinel)
                .build();
        EffectiveConnectorConfig sink = EffectiveConnectorConfig.newBuilder()
                .setConnector("csv")
                .putJobOptions("path", "output.csv")
                .build();

        ValidateResponse result = service.validateRequest(ValidateRequest.newBuilder()
                .setName("sensitive-option")
                .setSource(source)
                .setSink(sink)
                .setDeliveryGuarantee(CompilerDeliveryGuarantee.COMPILER_DELIVERY_GUARANTEE_AT_MOST_ONCE)
                .setMaxBatchRecords(100)
                .setExecutionProfile("standard")
                .build());

        assertThat(result.getValid()).isFalse();
        assertThat(result.getIssuesList())
                .extracting(issue -> issue.getCode())
                .contains(CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_OPTION_OWNERSHIP_INVALID);
        assertThat(new String(result.toByteArray(), StandardCharsets.UTF_8)).doesNotContain(sentinel);
        assertThat(result.toString()).doesNotContain(sentinel);
    }

    @Test
    void reportsRevisionFenceWithoutCompiling() {
        ValidateResponse result = service.validateRequest(baseCsvRequest()
                .setExpectedCompilerRevision("sha256:" + "0".repeat(64))
                .build());

        assertThat(result.getValid()).isFalse();
        assertThat(result.getIssuesList())
                .extracting(issue -> issue.getCode())
                .contains(CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_VALIDATION_REVISION_CHANGED);
        assertThat(result.getExecutionMode()).isEqualTo(CompilerExecutionMode.COMPILER_EXECUTION_MODE_UNSPECIFIED);
    }

    @Test
    void mapsRuntimeCompilerTransformFailureToStableIssue() {
        ValidateResponse result = service.validateRequest(baseCsvRequest()
                .addTransforms(CompilerTransform.newBuilder().setType("rename").putAllOptions(Map.of("from", "a")))
                .build());

        assertThat(result.getValid()).isFalse();
        assertThat(result.getIssuesList()).singleElement().satisfies(issue -> {
            assertThat(issue.getCode())
                    .isEqualTo(CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_TRANSFORM_UNSUPPORTED);
            assertThat(issue.getFieldPath()).isEqualTo("spec.transforms");
        });
    }

    @Test
    void requiresConnectionForJdbcRole() {
        ValidateResponse result = service.validateRequest(ValidateRequest.newBuilder()
                .setName("jdbc-copy")
                .setSource(EffectiveConnectorConfig.newBuilder()
                        .setConnector("jdbc")
                        .putJobOptions("query", "select-one"))
                .setSink(EffectiveConnectorConfig.newBuilder().setConnector("jdbc"))
                .setDeliveryGuarantee(CompilerDeliveryGuarantee.COMPILER_DELIVERY_GUARANTEE_AT_MOST_ONCE)
                .setMaxBatchRecords(100)
                .setExecutionProfile("standard")
                .build());

        assertThat(result.getValid()).isFalse();
        assertThat(result.getIssuesList())
                .extracting(issue -> issue.getCode())
                .containsOnly(CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_CONNECTION_REF_REQUIRED);
    }

    private ValidateRequest.Builder baseCsvRequest() {
        return ValidateRequest.newBuilder()
                .setName("csv-copy")
                .setSource(EffectiveConnectorConfig.newBuilder()
                        .setConnector("csv")
                        .putJobOptions("path", "input.csv"))
                .setSink(EffectiveConnectorConfig.newBuilder()
                        .setConnector("csv")
                        .putJobOptions("path", "output.csv"))
                .setDeliveryGuarantee(CompilerDeliveryGuarantee.COMPILER_DELIVERY_GUARANTEE_AT_MOST_ONCE)
                .setMaxBatchRecords(100)
                .setExecutionProfile("standard");
    }

    private ConnectorDescriptor descriptor(String name) {
        return registry.findDescriptor(name).orElseThrow();
    }
}
