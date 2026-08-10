package io.astrasync.engine.runtime;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectionRequirement;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorOptionDescriptor;
import io.astrasync.connector.api.ConnectorOptionOwner;
import io.astrasync.connector.api.ConnectorOptionSensitivity;
import io.astrasync.connector.api.ConnectorOptionType;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.engine.jobspec.ConnectorSpec;
import io.astrasync.engine.jobspec.DeliveryGuarantee;
import io.astrasync.engine.jobspec.DeliverySpec;
import io.astrasync.engine.jobspec.JobConfiguration;
import io.astrasync.engine.jobspec.JobMetadata;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.jobspec.RuntimeSpec;
import io.astrasync.engine.plan.ConnectorRegistry;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class RuntimeCredentialLoaderTest {
    private static final String COMPILER_REVISION =
            "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
    private static final String JOB_UID = "11111111-1111-4111-8111-111111111111";
    private static final long EPOCH = 7;

    @TempDir
    Path credentialDirectory;

    @Test
    void mergesDefaultsConnectionAndJobOptionsForBothRoles() throws IOException {
        Fixture fixture = fixture();
        writeEnvelope(fixture, ConnectorRole.SOURCE, "source-password", "jdbc:h2:mem:source");
        writeEnvelope(fixture, ConnectorRole.SINK, "sink-password", "jdbc:h2:mem:sink");

        JobSpec merged = RuntimeCredentialLoader.load(fixture.job(), fixture.registry(), environment());

        assertThat(merged.spec().source().options())
                .containsEntry("batchSize", "100")
                .containsEntry("table", "source_table")
                .containsEntry("url", "jdbc:h2:mem:source")
                .containsEntry("password", "source-password");
        assertThat(merged.spec().sink().options())
                .containsEntry("batchSize", "100")
                .containsEntry("table", "sink_table")
                .containsEntry("url", "jdbc:h2:mem:sink")
                .containsEntry("password", "sink-password");
        assertThat(merged.spec().source().toString()).doesNotContain("source-password", "jdbc:h2:mem:source");
    }

    @Test
    void leavesLocalExecutionUnchangedWithoutTrustedSchedulerMetadata() {
        Fixture fixture = fixture();

        assertThat(RuntimeCredentialLoader.load(fixture.job(), fixture.registry(), Map.of()))
                .isSameAs(fixture.job());
    }

    @Test
    void rejectsMissingRequiredRoleEnvelope() throws IOException {
        Fixture fixture = fixture();
        writeEnvelope(fixture, ConnectorRole.SOURCE, "source-password", "jdbc:h2:mem:source");

        assertThatThrownBy(() -> RuntimeCredentialLoader.load(fixture.job(), fixture.registry(), environment()))
                .isInstanceOf(RuntimeCredentialLoader.RuntimeCredentialException.class)
                .hasMessageContaining("required runtime credential envelope is missing");
    }

    @Test
    void rejectsSecretDisguisedAsPersistedSettingWithoutEchoingItsValue() throws IOException {
        Fixture fixture = fixture();
        String sentinel = "password-sentinel-that-must-not-leak";
        writeRawEnvelope(
                fixture,
                ConnectorRole.SOURCE,
                options(
                        option("password", "CONNECTION_SETTING", sentinel),
                        option("url", "CONNECTION_SETTING", "jdbc:h2:mem:source")));
        writeEnvelope(fixture, ConnectorRole.SINK, "sink-password", "jdbc:h2:mem:sink");

        assertThatThrownBy(() -> RuntimeCredentialLoader.load(fixture.job(), fixture.registry(), environment()))
                .isInstanceOf(RuntimeCredentialLoader.RuntimeCredentialException.class)
                .hasMessageContaining("option source is invalid")
                .hasMessageNotContaining(sentinel)
                .hasMessageNotContaining(encoded(sentinel));
    }

    @Test
    void rejectsDuplicateJsonMembersAndInvalidUtf8() throws IOException {
        Fixture fixture = fixture();
        String duplicate = envelope(
                        fixture,
                        ConnectorRole.SOURCE,
                        options(
                                option("password", "PROVIDER", "source-password"),
                                option("url", "CONNECTION_SETTING", "jdbc:h2:mem:source")))
                .replace("\"schemaVersion\":1", "\"schemaVersion\":1,\"schemaVersion\":1");
        Files.writeString(credentialDirectory.resolve("source.json"), duplicate, StandardCharsets.UTF_8);
        writeEnvelope(fixture, ConnectorRole.SINK, "sink-password", "jdbc:h2:mem:sink");

        assertThatThrownBy(() -> RuntimeCredentialLoader.load(fixture.job(), fixture.registry(), environment()))
                .isInstanceOf(RuntimeCredentialLoader.RuntimeCredentialException.class)
                .hasMessageContaining("cannot be read or decoded");

        writeRawEnvelope(
                fixture,
                ConnectorRole.SOURCE,
                "[{\"key\":\"password\",\"source\":\"PROVIDER\",\"value\":\"/w==\"},"
                        + option("url", "CONNECTION_SETTING", "jdbc:h2:mem:source") + "]");
        assertThatThrownBy(() -> RuntimeCredentialLoader.load(fixture.job(), fixture.registry(), environment()))
                .isInstanceOf(RuntimeCredentialLoader.RuntimeCredentialException.class)
                .hasMessageContaining("not valid UTF-8");
    }

    @Test
    void rejectsExecutionAndArtifactRevisionMismatch() throws IOException {
        Fixture fixture = fixture();
        String wrongEpoch = envelope(
                        fixture,
                        ConnectorRole.SOURCE,
                        options(
                                option("password", "PROVIDER", "source-password"),
                                option("url", "CONNECTION_SETTING", "jdbc:h2:mem:source")))
                .replace("\"epoch\":7", "\"epoch\":8");
        Files.writeString(credentialDirectory.resolve("source.json"), wrongEpoch, StandardCharsets.UTF_8);
        writeEnvelope(fixture, ConnectorRole.SINK, "sink-password", "jdbc:h2:mem:sink");

        assertThatThrownBy(() -> RuntimeCredentialLoader.load(fixture.job(), fixture.registry(), environment()))
                .isInstanceOf(RuntimeCredentialLoader.RuntimeCredentialException.class)
                .hasMessageContaining("identity or revision does not match");
    }

    private Map<String, String> environment() {
        return Map.of(
                RuntimeCredentialLoader.CREDENTIAL_DIRECTORY_ENVIRONMENT,
                credentialDirectory.toString(),
                RuntimeCredentialLoader.JOB_UID_ENVIRONMENT,
                JOB_UID,
                RuntimeCredentialLoader.EXECUTION_EPOCH_ENVIRONMENT,
                Long.toString(EPOCH),
                RuntimeCredentialLoader.COMPILER_REVISION_ENVIRONMENT,
                COMPILER_REVISION);
    }

    private void writeEnvelope(Fixture fixture, ConnectorRole role, String password, String url) throws IOException {
        writeRawEnvelope(
                fixture,
                role,
                options(option("password", "PROVIDER", password), option("url", "CONNECTION_SETTING", url)));
    }

    private void writeRawEnvelope(Fixture fixture, ConnectorRole role, String options) throws IOException {
        Files.writeString(
                credentialDirectory.resolve(role.name().toLowerCase() + ".json"),
                envelope(fixture, role, options),
                StandardCharsets.UTF_8);
    }

    private static String envelope(Fixture fixture, ConnectorRole role, String options) {
        return "{\"schemaVersion\":1,\"jobUid\":\"" + JOB_UID + "\",\"epoch\":" + EPOCH
                + ",\"role\":\"" + role.name() + "\",\"connectionUid\":\""
                + (role == ConnectorRole.SOURCE
                        ? "22222222-2222-4222-8222-222222222222"
                        : "33333333-3333-4333-8333-333333333333")
                + "\",\"generation\":3,\"descriptorRevision\":\""
                + fixture.descriptor().descriptorRevision() + "\",\"compilerRevision\":\"" + COMPILER_REVISION
                + "\",\"connectionSchemaRevision\":\"" + fixture.descriptor().connectionSchemaRevision()
                + "\",\"providerKind\":\"KUBERNETES_SECRET_V1\",\"providerObjectUid\":\""
                + UUID.randomUUID() + "\",\"providerVersionToken\":\"rv-42\",\"options\":" + options + "}";
    }

    private static String options(String... values) {
        return "[" + String.join(",", values) + "]";
    }

    private static String option(String key, String source, String value) {
        return "{\"key\":\"" + key + "\",\"source\":\"" + source + "\",\"value\":\"" + encoded(value) + "\"}";
    }

    private static String encoded(String value) {
        return Base64.getEncoder().encodeToString(value.getBytes(StandardCharsets.UTF_8));
    }

    private static Fixture fixture() {
        ConnectorDescriptor.Builder builder = ConnectorDescriptor.builder(
                        "fixture",
                        "1.0.0",
                        Set.of(ConnectorRole.SOURCE, ConnectorRole.SINK),
                        Set.of(Capability.BATCH_READ, Capability.BATCH_WRITE))
                .connectionRequirement(ConnectorRole.SOURCE, ConnectionRequirement.REQUIRED)
                .connectionRequirement(ConnectorRole.SINK, ConnectionRequirement.REQUIRED);
        builder.option(ConnectorOptionDescriptor.builder(
                        "url",
                        ConnectorOptionType.STRING,
                        ConnectorOptionOwner.CONNECTION,
                        ConnectorRole.SOURCE,
                        ConnectorRole.SINK)
                .required()
                .sensitivity(ConnectorOptionSensitivity.RESTRICTED)
                .patternKey("jdbc.url")
                .build());
        builder.option(ConnectorOptionDescriptor.builder(
                        "password",
                        ConnectorOptionType.STRING,
                        ConnectorOptionOwner.CONNECTION,
                        ConnectorRole.SOURCE,
                        ConnectorRole.SINK)
                .required()
                .sensitivity(ConnectorOptionSensitivity.SECRET)
                .build());
        builder.option(ConnectorOptionDescriptor.builder(
                        "table",
                        ConnectorOptionType.STRING,
                        ConnectorOptionOwner.JOB,
                        ConnectorRole.SOURCE,
                        ConnectorRole.SINK)
                .required()
                .patternKey("sql.table")
                .build());
        builder.option(ConnectorOptionDescriptor.builder(
                        "batchSize",
                        ConnectorOptionType.INTEGER,
                        ConnectorOptionOwner.JOB,
                        ConnectorRole.SOURCE,
                        ConnectorRole.SINK)
                .numericBounds(1, 10_000)
                .defaultValue("100")
                .build());
        ConnectorDescriptor descriptor = builder.build();
        ConnectorRegistry registry = new ConnectorRegistry(List.of(new FixtureConnectorFactory(descriptor)));
        JobSpec job = new JobSpec(
                JobSpec.API_VERSION,
                JobSpec.KIND,
                new JobMetadata("runtime-loader"),
                new JobConfiguration(
                        new ConnectorSpec("fixture", Map.of("table", "source_table")),
                        List.of(),
                        new ConnectorSpec("fixture", Map.of("table", "sink_table")),
                        new DeliverySpec(DeliveryGuarantee.AT_MOST_ONCE),
                        RuntimeSpec.defaults()));
        return new Fixture(descriptor, registry, job);
    }

    private record Fixture(ConnectorDescriptor descriptor, ConnectorRegistry registry, JobSpec job) {}

    private record FixtureConnectorFactory(ConnectorDescriptor descriptor)
            implements io.astrasync.connector.api.ConnectorFactory {}
}
