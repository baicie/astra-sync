package io.astrasync.control.compiler;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.io.IOException;
import java.util.Map;
import org.junit.jupiter.api.Test;

class CompilerValidationApplicationTest {
    @Test
    void usesBoundedDevelopmentDefaults() throws Exception {
        CompilerValidationApplication.Configuration configuration =
                CompilerValidationApplication.Configuration.from(Map.of());

        assertThat(configuration.port()).isEqualTo(50052);
        assertThat(configuration.executionProfile()).isEqualTo("standard");
        assertThat(configuration.maximumConcurrency()).isEqualTo(32);
        assertThat(configuration.tlsEnabled()).isFalse();
    }

    @Test
    void productionRequiresMutualTls() {
        assertThatThrownBy(() -> CompilerValidationApplication.Configuration.from(Map.of("APP_ENV", "production")))
                .isInstanceOf(IOException.class)
                .hasMessageContaining("mutual TLS");
    }

    @Test
    void partialTlsConfigurationFailsClosed() {
        assertThatThrownBy(() -> CompilerValidationApplication.Configuration.from(
                        Map.of("COMPILER_VALIDATION_TLS_CERTIFICATE_FILE", "server.crt")))
                .isInstanceOf(IOException.class)
                .hasMessageContaining("configured together");
    }

    @Test
    void acceptsBoundedProductionConfigurationWithMutualTls() throws Exception {
        CompilerValidationApplication.Configuration configuration =
                CompilerValidationApplication.Configuration.from(Map.of(
                        "APP_ENV", "PRODUCTION",
                        "COMPILER_VALIDATION_PORT", "50443",
                        "COMPILER_VALIDATION_MAX_CONCURRENCY", "64",
                        "COMPILER_BUILD", "release-20",
                        "CONNECTOR_EXECUTION_PROFILE", "hosted",
                        "COMPILER_VALIDATION_TLS_CERTIFICATE_FILE", "server.crt",
                        "COMPILER_VALIDATION_TLS_PRIVATE_KEY_FILE", "server.key",
                        "COMPILER_VALIDATION_TLS_CLIENT_CA_FILE", "client-ca.crt"));

        assertThat(configuration.port()).isEqualTo(50443);
        assertThat(configuration.environment()).isEqualTo("production");
        assertThat(configuration.compilerBuild()).isEqualTo("release-20");
        assertThat(configuration.executionProfile()).isEqualTo("hosted");
        assertThat(configuration.maximumConcurrency()).isEqualTo(64);
        assertThat(configuration.tlsEnabled()).isTrue();
    }

    @Test
    void rejectsUnsupportedEnvironmentAndInvalidNumbers() {
        assertThatThrownBy(() -> CompilerValidationApplication.Configuration.from(Map.of("APP_ENV", "staging")))
                .isInstanceOf(IOException.class)
                .hasMessageContaining("APP_ENV");
        assertThatThrownBy(() ->
                        CompilerValidationApplication.Configuration.from(Map.of("COMPILER_VALIDATION_PORT", "70000")))
                .isInstanceOf(IOException.class)
                .hasMessageContaining("outside the supported range");
        assertThatThrownBy(() -> CompilerValidationApplication.Configuration.from(
                        Map.of("COMPILER_VALIDATION_MAX_CONCURRENCY", "many")))
                .isInstanceOf(IOException.class)
                .hasMessageContaining("must be an integer");
    }

    @Test
    void rejectsBlankAndOversizedRevisionLabels() {
        assertThatThrownBy(() -> CompilerValidationApplication.Configuration.from(Map.of("COMPILER_BUILD", " ")))
                .isInstanceOf(IOException.class)
                .hasMessageContaining("compiler build");
        assertThatThrownBy(() -> CompilerValidationApplication.Configuration.from(
                        Map.of("CONNECTOR_EXECUTION_PROFILE", "x".repeat(257))))
                .isInstanceOf(IOException.class)
                .hasMessageContaining("execution profile");
    }
}
