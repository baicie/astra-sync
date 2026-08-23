package io.astrasync.engine.worker;

import io.astrasync.engine.observability.DataPlaneMetrics;
import io.astrasync.engine.observability.DataPlaneMetricsServer;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.micrometer.prometheusmetrics.PrometheusMeterRegistry;
import java.lang.reflect.InvocationTargetException;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/** Executable entry point for a configured Worker protocol service. */
public final class WorkerApplication {
    private static final Logger LOG = LoggerFactory.getLogger(WorkerApplication.class);

    private WorkerApplication() {}

    public static void main(String[] args) {
        try {
            Map<String, String> environment = System.getenv();
            WorkerConfiguration configuration = WorkerConfiguration.fromEnvironment(environment);
            WorkerService service = createService(configuration, environment);
            Optional<DataPlaneMetricsServer> metricsServer = startMetricsServer(environment);
            Runtime.getRuntime()
                    .addShutdownHook(new Thread(
                            () -> {
                                metricsServer.ifPresent(DataPlaneMetricsServer::close);
                                service.close();
                            },
                            "astrasync-worker-shutdown"));
            service.start();
            metricsServer.ifPresent(server -> LOG.info("metrics endpoint listening on port {}", server.port()));
            System.out.println("READY workerId=" + configuration.workerId() + " port=" + service.port());
            service.await();
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
        } catch (RuntimeException exception) {
            LOG.error("worker failed to start", exception);
            System.err.println("FAILED to start Worker: " + message(exception));
            System.exit(1);
        }
    }

    static WorkerService createService(WorkerConfiguration configuration, Map<String, String> environment) {
        WorkerConfiguration checked = Objects.requireNonNull(configuration, "configuration must not be null");
        WorkerTaskFactoryProvider provider = loadProvider(checked.taskFactoryProvider());
        BatchTaskFactory taskFactory = Objects.requireNonNull(
                provider.create(Map.copyOf(Objects.requireNonNull(environment, "environment must not be null"))),
                "task factory provider returned null");
        return new WorkerService(checked, taskFactory);
    }

    static Optional<DataPlaneMetricsServer> startMetricsServer(Map<String, String> environment) {
        return startMetricsServer(environment, DataPlaneMetrics.processRegistry());
    }

    static Optional<DataPlaneMetricsServer> startMetricsServer(
            Map<String, String> environment, PrometheusMeterRegistry registry) {
        Map<String, String> checked = Map.copyOf(Objects.requireNonNull(environment, "environment must not be null"));
        return DataPlaneMetricsServer.start(checked.getOrDefault("METRICS_LISTEN_ADDRESS", ""), registry);
    }

    private static WorkerTaskFactoryProvider loadProvider(String className) {
        try {
            Class<?> providerClass =
                    Class.forName(className, true, Thread.currentThread().getContextClassLoader());
            return providerClass
                    .asSubclass(WorkerTaskFactoryProvider.class)
                    .getConstructor()
                    .newInstance();
        } catch (ClassNotFoundException exception) {
            throw new IllegalArgumentException("task factory provider class was not found: " + className, exception);
        } catch (ClassCastException exception) {
            throw new IllegalArgumentException(
                    "task factory provider does not implement WorkerTaskFactoryProvider: " + className, exception);
        } catch (NoSuchMethodException | InstantiationException | IllegalAccessException exception) {
            throw new IllegalArgumentException(
                    "task factory provider must have a public no-argument constructor: " + className, exception);
        } catch (InvocationTargetException exception) {
            throw new IllegalArgumentException(
                    "task factory provider constructor failed: " + className,
                    exception.getCause() == null ? exception : exception.getCause());
        }
    }

    private static String message(RuntimeException exception) {
        return exception.getMessage() == null ? exception.getClass().getSimpleName() : exception.getMessage();
    }
}
