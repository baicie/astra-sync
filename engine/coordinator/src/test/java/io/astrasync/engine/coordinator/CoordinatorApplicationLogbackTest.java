package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.LoggerContext;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.core.read.ListAppender;
import io.astrasync.engine.worker.JdbcWorkerTaskFactoryProvider;
import io.astrasync.engine.worker.WorkerConfiguration;
import io.astrasync.engine.worker.WorkerService;
import java.nio.file.Files;
import java.nio.file.Path;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.SQLException;
import java.sql.Statement;
import java.time.Duration;
import java.util.List;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

/**
 * Asserts that the F2 migration emits SLF4J ERROR events on the failure
 * path of {@link CoordinatorApplication}. The test wires a
 * {@link ListAppender} to the same logger that
 * {@code CoordinatorApplication} uses, invokes {@code run(configuration)}
 * with a non-existent JobSpec path, passes the rejection to the production
 * error logger used by {@code main}, and asserts the appender captured an
 * ERROR event whose logger name matches the class under test.
 */
class CoordinatorApplicationLogbackTest {

    @TempDir
    Path tempDirectory;

    @Test
    void runFailureEmitsSlf4jErrorEvent() {
        ListAppender<ILoggingEvent> appender = attachAppender();
        try {
            Path jobSpec = tempDirectory.resolve("missing.yaml");
            CoordinatorConfiguration configuration = new CoordinatorConfiguration(
                    jobSpec,
                    tempDirectory.resolve("progress"),
                    List.of(new WorkerEndpoint("worker-0", "127.0.0.1", 1)),
                    Duration.ofSeconds(1),
                    1,
                    1);

            try {
                CoordinatorApplication.run(configuration);
            } catch (RuntimeException expected) {
                CoordinatorApplication.logFailure(expected);
            }

            assertThat(appender.list).isNotEmpty();
            ILoggingEvent event = appender.list.get(appender.list.size() - 1);
            assertThat(event.getLevel()).isEqualTo(ch.qos.logback.classic.Level.ERROR);
            assertThat(event.getLoggerName()).isEqualTo(CoordinatorApplication.class.getName());
            assertThat(event.getMessage()).isEqualTo("coordinator failed to start or execute");
            assertThat(event.getThrowableProxy()).isNotNull();
        } finally {
            Logger coordinator = (Logger) org.slf4j.LoggerFactory.getLogger(CoordinatorApplication.class);
            coordinator.detachAppender(appender);
        }
    }

    @Test
    void runWithRemoteFailureExposesExceptionToCatchBlock() throws Exception {
        String url = jdbcUrl();
        initializeDatabase(url);
        Path jobSpec = writeJob(url);
        ListAppender<ILoggingEvent> ignored = attachAppender();

        try (WorkerService worker0 = worker("worker-0")) {
            worker0.start();
            CoordinatorConfiguration configuration = new CoordinatorConfiguration(
                    jobSpec,
                    tempDirectory.resolve("progress"),
                    List.of(new WorkerEndpoint("worker-0", "127.0.0.1", worker0.port())),
                    Duration.ofSeconds(2),
                    1,
                    1);

            assertThatThrownBy(() -> CoordinatorApplication.run(configuration))
                    .hasRootCauseMessage("remote task failed: planned failure");
        }
    }

    private ListAppender<ILoggingEvent> attachAppender() {
        LoggerContext context = (LoggerContext) org.slf4j.LoggerFactory.getILoggerFactory();
        Logger coordinator = (Logger) org.slf4j.LoggerFactory.getLogger(CoordinatorApplication.class);
        ListAppender<ILoggingEvent> appender = new ListAppender<>();
        appender.setContext(context);
        appender.start();
        coordinator.addAppender(appender);
        return appender;
    }

    private static WorkerService worker(String workerId) {
        return new WorkerService(
                new WorkerConfiguration(workerId, 0, JdbcWorkerTaskFactoryProvider.class.getName(), 1, 0, 4), split -> {
                    throw new IllegalStateException("planned failure");
                });
    }

    private Path writeJob(String url) throws Exception {
        Path path = tempDirectory.resolve("cli-test.yaml");
        Files.writeString(
                path,
                """
                apiVersion: sync.astrasync.io/v1
                kind: SyncJob
                metadata:
                  name: cli-test
                spec:
                  source:
                    connector: jdbc
                    options:
                      url: '%s'
                      table: SOURCE_DATA
                      splitColumn: ID
                      splitCount: '2'
                  sink:
                    connector: jdbc
                    options:
                      url: '%s'
                      table: TARGET_DATA
                  delivery:
                    guarantee: at-most-once
                  runtime:
                    maxBatchRecords: 2
                """
                        .formatted(url.replace("'", "''"), url.replace("'", "''")),
                java.nio.charset.StandardCharsets.UTF_8);
        return path;
    }

    private static void initializeDatabase(String url) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                Statement statement = connection.createStatement()) {
            statement.execute("CREATE TABLE SOURCE_DATA (ID INT NOT NULL, NAME VARCHAR(40))");
            statement.execute("CREATE TABLE TARGET_DATA (ID INT PRIMARY KEY, NAME VARCHAR(40))");
            statement.execute("INSERT INTO SOURCE_DATA VALUES (1, 'Ada'), (2, 'Lin'), (3, 'Kai'), (4, 'May')");
        }
    }

    private static String jdbcUrl() {
        return "jdbc:h2:mem:coord_logback_" + UUID.randomUUID().toString().replace('-', '_')
                + ";MODE=PostgreSQL;DB_CLOSE_DELAY=-1";
    }
}
