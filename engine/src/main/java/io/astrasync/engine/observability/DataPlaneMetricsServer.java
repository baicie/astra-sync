package io.astrasync.engine.observability;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import io.micrometer.prometheusmetrics.PrometheusMeterRegistry;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Objects;
import java.util.Optional;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

/** Exposes a Prometheus scrape endpoint only when explicitly configured. */
public final class DataPlaneMetricsServer implements AutoCloseable {
    private static final Duration STOP_DELAY = Duration.ofSeconds(5);
    private static final String PROMETHEUS_MEDIA_TYPE = "text/plain";
    private static final String OPENMETRICS_MEDIA_TYPE = "application/openmetrics-text";
    private static final String PROMETHEUS_CONTENT_TYPE = "text/plain; version=0.0.4; charset=utf-8";
    private static final String OPENMETRICS_CONTENT_TYPE = "application/openmetrics-text; version=1.0.0; charset=utf-8";

    private final HttpServer server;
    private final ExecutorService executor;

    private DataPlaneMetricsServer(HttpServer server, ExecutorService executor) {
        this.server = server;
        this.executor = executor;
    }

    /** Starts an endpoint for a non-empty listen address, or returns empty when metrics are disabled. */
    public static Optional<DataPlaneMetricsServer> start(String listenAddress, PrometheusMeterRegistry registry) {
        Objects.requireNonNull(listenAddress, "listenAddress must not be null");
        Objects.requireNonNull(registry, "registry must not be null");
        if (listenAddress.isBlank()) {
            return Optional.empty();
        }
        InetSocketAddress address = parseAddress(listenAddress);
        try {
            HttpServer server = HttpServer.create(address, 0);
            ExecutorService executor = Executors.newVirtualThreadPerTaskExecutor();
            server.setExecutor(executor);
            server.createContext("/metrics", exchange -> writeScrape(exchange, registry));
            server.start();
            return Optional.of(new DataPlaneMetricsServer(server, executor));
        } catch (IOException exception) {
            throw new IllegalStateException("failed to start metrics endpoint at " + listenAddress, exception);
        }
    }

    /** Returns the bound TCP port. */
    public int port() {
        return server.getAddress().getPort();
    }

    @Override
    public void close() {
        server.stop((int) STOP_DELAY.toSeconds());
        executor.shutdown();
        try {
            if (!executor.awaitTermination(STOP_DELAY.toSeconds(), TimeUnit.SECONDS)) {
                executor.shutdownNow();
            }
        } catch (InterruptedException exception) {
            executor.shutdownNow();
            Thread.currentThread().interrupt();
        }
    }

    private static void writeScrape(HttpExchange exchange, PrometheusMeterRegistry registry) throws IOException {
        if (!"GET".equals(exchange.getRequestMethod())) {
            exchange.sendResponseHeaders(405, -1);
            exchange.close();
            return;
        }
        String contentType = selectContentType(exchange.getRequestHeaders().getFirst("Accept"));
        byte[] body = registry.scrape(contentType).getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().set("Content-Type", contentType);
        exchange.getResponseHeaders().set("Vary", "Accept");
        exchange.sendResponseHeaders(200, body.length);
        exchange.getResponseBody().write(body);
        exchange.close();
    }

    private static String selectContentType(String accept) {
        double openMetricsQuality = quality(accept, OPENMETRICS_MEDIA_TYPE, true);
        if (openMetricsQuality <= 0) {
            return PROMETHEUS_CONTENT_TYPE;
        }
        double prometheusQuality = quality(accept, PROMETHEUS_MEDIA_TYPE, false);
        return openMetricsQuality > prometheusQuality ? OPENMETRICS_CONTENT_TYPE : PROMETHEUS_CONTENT_TYPE;
    }

    private static double quality(String accept, String mediaType, boolean exactMediaTypeOnly) {
        if (accept == null || accept.isBlank()) {
            return 0;
        }
        int targetSeparator = mediaType.indexOf('/');
        if (targetSeparator <= 0 || targetSeparator == mediaType.length() - 1) {
            return 0;
        }
        String targetType = mediaType.substring(0, targetSeparator);
        String targetSubtype = mediaType.substring(targetSeparator + 1);
        Double exactQuality = null;
        Double typeQuality = null;
        Double wildcardQuality = null;
        for (String candidate : accept.split(",")) {
            String[] parts = candidate.split(";");
            String range = parts[0].trim();
            int separator = range.indexOf('/');
            if (separator <= 0 || separator == range.length() - 1) {
                continue;
            }
            String rangeType = range.substring(0, separator);
            String rangeSubtype = range.substring(separator + 1);
            boolean exactMatch = rangeType.equalsIgnoreCase(targetType) && rangeSubtype.equalsIgnoreCase(targetSubtype);
            boolean typeWildcardMatch =
                    !exactMediaTypeOnly && rangeType.equalsIgnoreCase(targetType) && rangeSubtype.equals("*");
            boolean wildcardMatch = !exactMediaTypeOnly && rangeType.equals("*") && rangeSubtype.equals("*");
            if (!exactMatch && !typeWildcardMatch && !wildcardMatch) {
                continue;
            }
            Double candidateQuality = parseQuality(parts);
            if (candidateQuality == null) {
                continue;
            }
            if (exactMatch) {
                exactQuality = highest(exactQuality, candidateQuality);
            } else if (typeWildcardMatch) {
                typeQuality = highest(typeQuality, candidateQuality);
            } else {
                wildcardQuality = highest(wildcardQuality, candidateQuality);
            }
        }
        if (exactQuality != null) {
            return exactQuality;
        }
        if (typeQuality != null) {
            return typeQuality;
        }
        return wildcardQuality == null ? 0 : wildcardQuality;
    }

    private static Double parseQuality(String[] parts) {
        double quality = 1;
        for (int index = 1; index < parts.length; index++) {
            String parameter = parts[index].trim();
            int separator = parameter.indexOf('=');
            if (separator <= 0 || !parameter.substring(0, separator).trim().equalsIgnoreCase("q")) {
                continue;
            }
            try {
                quality = Double.parseDouble(parameter.substring(separator + 1).trim());
                if (!Double.isFinite(quality) || quality < 0 || quality > 1) {
                    return null;
                }
            } catch (NumberFormatException exception) {
                return null;
            }
        }
        return quality;
    }

    private static double highest(Double current, double candidate) {
        return current == null ? candidate : Math.max(current, candidate);
    }

    private static InetSocketAddress parseAddress(String value) {
        int separator = value.lastIndexOf(':');
        if (separator <= 0 || separator == value.length() - 1) {
            throw new IllegalArgumentException("metrics listen address must be host:port");
        }
        String host = value.substring(0, separator);
        try {
            return new InetSocketAddress(host, Integer.parseInt(value.substring(separator + 1)));
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("metrics listen port must be an integer", exception);
        }
    }
}
