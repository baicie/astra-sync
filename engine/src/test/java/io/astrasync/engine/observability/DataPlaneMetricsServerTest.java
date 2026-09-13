package io.astrasync.engine.observability;

import static org.assertj.core.api.Assertions.assertThat;

import io.micrometer.prometheusmetrics.PrometheusConfig;
import io.micrometer.prometheusmetrics.PrometheusMeterRegistry;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import org.junit.jupiter.api.Test;

class DataPlaneMetricsServerTest {
    @Test
    void doesNotBindWhenListenAddressIsBlank() {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);

        assertThat(DataPlaneMetricsServer.start("", registry)).isEmpty();
    }

    @Test
    void exposesRegisteredMetricsWhenListenAddressIsConfigured() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {
            new DataPlaneMetrics(registry).recordRecordsRead("1f36d9c6-77a2-4e83-8c7d-dc59b9b94a52", 2);

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type"))
                    .hasValueSatisfying(value -> assertThat(value).contains("text/plain; version=0.0.4"));
            assertThat(response.headers().firstValue("Vary")).contains("Accept");
            assertThat(response.body()).contains("worker_records_read_total");
        }
    }

    @Test
    void negotiatesOpenMetricsWhenRequested() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {
            new DataPlaneMetrics(registry).recordRecordsRead("1f36d9c6-77a2-4e83-8c7d-dc59b9b94a52", 2);

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text; version=1.0.0")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type")).hasValueSatisfying(value -> assertThat(value)
                    .contains("application/openmetrics-text; version=1.0.0"));
            assertThat(response.body()).contains("# EOF");
        }
    }

    @Test
    void keepsPrometheusTextWhenOpenMetricsIsExplicitlyRejected() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {
            new DataPlaneMetrics(registry).recordRecordsRead("1f36d9c6-77a2-4e83-8c7d-dc59b9b94a52", 2);

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text;q=0, text/plain;q=1")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type"))
                    .hasValueSatisfying(value -> assertThat(value).contains("text/plain; version=0.0.4"));
            assertThat(response.body()).doesNotContain("# EOF");
        }
    }

    @Test
    void prefersPrometheusTextWhenItHasHigherQualityValue() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text;q=0.5, text/plain;q=1")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type"))
                    .hasValueSatisfying(value -> assertThat(value).contains("text/plain; version=0.0.4"));
            assertThat(response.body()).doesNotContain("# EOF");
        }
    }

    @Test
    void negotiatesOpenMetricsWhenItHasHigherQualityValue() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text;q=1, text/plain;q=0.5")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type")).hasValueSatisfying(value -> assertThat(value)
                    .contains("application/openmetrics-text; version=1.0.0"));
            assertThat(response.body()).contains("# EOF");
        }
    }

    @Test
    void keepsPrometheusTextWhenQualityValuesTie() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text;q=0.5, text/plain;q=0.5")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type"))
                    .hasValueSatisfying(value -> assertThat(value).contains("text/plain; version=0.0.4"));
            assertThat(response.body()).doesNotContain("# EOF");
        }
    }

    @Test
    void keepsPrometheusTextWhenOpenMetricsQualityIsInvalid() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text;q=invalid")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type"))
                    .hasValueSatisfying(value -> assertThat(value).contains("text/plain; version=0.0.4"));
            assertThat(response.body()).doesNotContain("# EOF");
        }
    }

    @Test
    void prefersPrometheusTextWhenTypeWildcardHasHigherQuality() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text;q=0.5, text/*;q=1")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type"))
                    .hasValueSatisfying(value -> assertThat(value).contains("text/plain; version=0.0.4"));
            assertThat(response.body()).doesNotContain("# EOF");
        }
    }

    @Test
    void prefersExactPrometheusQualityOverGlobalWildcard() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text;q=0.8, text/plain;q=0.5, */*;q=1")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type")).hasValueSatisfying(value -> assertThat(value)
                    .contains("application/openmetrics-text; version=1.0.0"));
            assertThat(response.body()).contains("# EOF");
        }
    }

    @Test
    void prefersOpenMetricsWhenGlobalWildcardHasLowerQuality() throws IOException, InterruptedException {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        try (DataPlaneMetricsServer server =
                DataPlaneMetricsServer.start("127.0.0.1:0", registry).orElseThrow()) {

            HttpResponse<String> response = HttpClient.newHttpClient()
                    .send(
                            HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.port() + "/metrics"))
                                    .header("Accept", "application/openmetrics-text;q=1, */*;q=0.5")
                                    .GET()
                                    .build(),
                            HttpResponse.BodyHandlers.ofString());

            assertThat(response.statusCode()).isEqualTo(200);
            assertThat(response.headers().firstValue("Content-Type")).hasValueSatisfying(value -> assertThat(value)
                    .contains("application/openmetrics-text; version=1.0.0"));
            assertThat(response.body()).contains("# EOF");
        }
    }
}
