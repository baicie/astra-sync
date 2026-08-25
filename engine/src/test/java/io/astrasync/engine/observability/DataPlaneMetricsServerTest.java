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
            assertThat(response.body()).contains("worker_records_read_total");
        }
    }
}
