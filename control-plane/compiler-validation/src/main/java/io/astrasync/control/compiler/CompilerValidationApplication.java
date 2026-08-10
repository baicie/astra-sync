package io.astrasync.control.compiler;

import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.plan.ConnectorRegistry;
import io.grpc.Server;
import io.grpc.netty.shaded.io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.shaded.io.grpc.netty.NettyServerBuilder;
import io.grpc.netty.shaded.io.netty.handler.ssl.ClientAuth;
import java.io.File;
import java.io.IOException;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.TimeUnit;

public final class CompilerValidationApplication {
    private static final int DEFAULT_PORT = 50052;
    private static final int DEFAULT_MAX_CONCURRENCY = 32;
    private static final int MAX_INBOUND_MESSAGE_BYTES = 1_048_576;

    private CompilerValidationApplication() {}

    public static void main(String[] args) throws Exception {
        Configuration configuration = Configuration.from(System.getenv());
        ConnectorRegistry registry = ConnectorRegistry.discover();
        if (registry.descriptors().isEmpty()) {
            throw new IllegalStateException("no connector artifacts were discovered");
        }
        CompilerValidationGrpcService service = new CompilerValidationGrpcService(
                registry,
                JobSpec.API_VERSION,
                configuration.compilerBuild(),
                configuration.executionProfile(),
                configuration.maximumConcurrency());
        NettyServerBuilder builder = NettyServerBuilder.forPort(configuration.port())
                .maxInboundMessageSize(MAX_INBOUND_MESSAGE_BYTES)
                .permitKeepAliveWithoutCalls(false)
                .addService(service);
        if (configuration.tlsEnabled()) {
            builder.sslContext(GrpcSslContexts.forServer(
                            new File(configuration.certificateFile()), new File(configuration.privateKeyFile()))
                    .trustManager(new File(configuration.clientCaFile()))
                    .clientAuth(ClientAuth.REQUIRE)
                    .build());
        }
        Server server = builder.build().start();
        Runtime.getRuntime().addShutdownHook(new Thread(() -> shutdown(server), "compiler-validation-shutdown"));
        server.awaitTermination();
    }

    private static void shutdown(Server server) {
        try {
            if (!server.shutdown().awaitTermination(10, TimeUnit.SECONDS)) {
                server.shutdownNow();
            }
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            server.shutdownNow();
        }
    }

    record Configuration(
            int port,
            String environment,
            String compilerBuild,
            String executionProfile,
            int maximumConcurrency,
            String certificateFile,
            String privateKeyFile,
            String clientCaFile) {
        static Configuration from(Map<String, String> environment) throws IOException {
            int port = positiveInteger(environment.get("COMPILER_VALIDATION_PORT"), DEFAULT_PORT, "port", 65_535);
            int concurrency = positiveInteger(
                    environment.get("COMPILER_VALIDATION_MAX_CONCURRENCY"),
                    DEFAULT_MAX_CONCURRENCY,
                    "maximum concurrency",
                    1_024);
            String appEnvironment =
                    environment.getOrDefault("APP_ENV", "development").toLowerCase(Locale.ROOT);
            if (!appEnvironment.equals("development")
                    && !appEnvironment.equals("test")
                    && !appEnvironment.equals("production")) {
                throw new IOException("APP_ENV must be development, test, or production");
            }
            String certificate = environment.getOrDefault("COMPILER_VALIDATION_TLS_CERTIFICATE_FILE", "");
            String privateKey = environment.getOrDefault("COMPILER_VALIDATION_TLS_PRIVATE_KEY_FILE", "");
            String clientCa = environment.getOrDefault("COMPILER_VALIDATION_TLS_CLIENT_CA_FILE", "");
            int configuredTlsFields =
                    (certificate.isEmpty() ? 0 : 1) + (privateKey.isEmpty() ? 0 : 1) + (clientCa.isEmpty() ? 0 : 1);
            if (configuredTlsFields != 0 && configuredTlsFields != 3) {
                throw new IOException(
                        "compiler validation TLS certificate, key, and client CA must be configured together");
            }
            if (appEnvironment.equals("production") && configuredTlsFields != 3) {
                throw new IOException("production compiler validation requires mutual TLS");
            }
            return new Configuration(
                    port,
                    appEnvironment,
                    bounded(environment.getOrDefault("COMPILER_BUILD", "0.1.0-SNAPSHOT"), "compiler build"),
                    bounded(environment.getOrDefault("CONNECTOR_EXECUTION_PROFILE", "standard"), "execution profile"),
                    concurrency,
                    certificate,
                    privateKey,
                    clientCa);
        }

        boolean tlsEnabled() {
            return !certificateFile.isEmpty();
        }

        private static int positiveInteger(String value, int defaultValue, String label, int maximum)
                throws IOException {
            if (value == null || value.isBlank()) {
                return defaultValue;
            }
            try {
                int parsed = Integer.parseInt(value);
                if (parsed <= 0 || parsed > maximum) {
                    throw new IOException(label + " is outside the supported range");
                }
                return parsed;
            } catch (NumberFormatException exception) {
                throw new IOException(label + " must be an integer", exception);
            }
        }

        private static String bounded(String value, String label) throws IOException {
            if (value.isBlank() || value.length() > 256) {
                throw new IOException(label + " must be between 1 and 256 characters");
            }
            return value;
        }
    }
}
