package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.sun.net.httpserver.HttpServer;
import java.net.InetSocketAddress;
import java.net.URI;
import java.time.Duration;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

class ExecutionHeartbeatTest {
    @Test
    @SuppressWarnings("try")
    void sendsInitialAndPeriodicAuthenticatedHeartbeats() throws Exception {
        AtomicInteger requests = new AtomicInteger();
        AtomicReference<String> authorization = new AtomicReference<>();
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/heartbeat", exchange -> {
            authorization.set(exchange.getRequestHeaders().getFirst("Authorization"));
            requests.incrementAndGet();
            exchange.sendResponseHeaders(204, -1);
            exchange.close();
        });
        server.start();
        String token = UUID.randomUUID().toString();
        URI endpoint = URI.create("http://127.0.0.1:" + server.getAddress().getPort() + "/heartbeat");
        try (ExecutionHeartbeat ignored = ExecutionHeartbeat.start(
                Optional.of(new ExecutionHeartbeatConfiguration(endpoint, token, Duration.ofMillis(50))))) {
            long deadline = System.nanoTime() + Duration.ofSeconds(2).toNanos();
            while (requests.get() < 2 && System.nanoTime() < deadline) {
                Thread.sleep(20);
            }
        } finally {
            server.stop(0);
        }

        assertThat(requests.get()).isGreaterThanOrEqualTo(2);
        assertThat(authorization.get()).isEqualTo("Bearer " + token);
    }

    @Test
    void failsStartupWhenInitialHeartbeatIsRejected() throws Exception {
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/heartbeat", exchange -> {
            exchange.sendResponseHeaders(401, -1);
            exchange.close();
        });
        server.start();
        URI endpoint = URI.create("http://127.0.0.1:" + server.getAddress().getPort() + "/heartbeat");
        try {
            ExecutionHeartbeatConfiguration configuration = new ExecutionHeartbeatConfiguration(
                    endpoint, UUID.randomUUID().toString(), Duration.ofSeconds(1));
            assertThatThrownBy(() -> ExecutionHeartbeat.start(Optional.of(configuration)))
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessage("control plane returned HTTP 401");
        } finally {
            server.stop(0);
        }
    }
}
