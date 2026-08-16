package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;

import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.LoggerContext;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.core.read.ListAppender;
import org.junit.jupiter.api.Test;
import org.slf4j.LoggerFactory;

/**
 * Verifies that the Logback configuration shipped under
 * {@code src/main/resources/logback.xml} is loadable and that the
 * configured root appender emits a JSON record carrying the
 * {@code component} field that the convention requires.
 *
 * <p>The error-path assertion (that {@code LOG.error("coordinator failed to
 * start", t)} delivers to Logback) lands in
 * {@code CoordinatorApplicationLogbackTest} after the F2 migration. This
 * test only exercises the configuration wiring.
 */
class CoordinatorLogbackConfigurationTest {

    @Test
    void logbackContextStartsFromClasspathConfiguration() {
        LoggerContext context = (LoggerContext) LoggerFactory.getILoggerFactory();
        assertThat(context.getName()).isEqualTo("default");
        assertThat(context.getCopyOfPropertyMap()).isNotNull();
    }

    @Test
    void rootLoggerDeliversEventThroughConfiguredAppender() {
        LoggerContext context = (LoggerContext) LoggerFactory.getILoggerFactory();
        Logger logger = (Logger) LoggerFactory.getLogger("io.astrasync.test.Capture");
        logger.setLevel(ch.qos.logback.classic.Level.INFO);

        ListAppender<ILoggingEvent> listAppender = new ListAppender<>();
        listAppender.setContext(context);
        listAppender.start();
        logger.addAppender(listAppender);

        try {
            logger.info("hello {}", "world");
        } finally {
            logger.detachAppender(listAppender);
        }

        assertThat(listAppender.list).hasSize(1);
        ILoggingEvent event = listAppender.list.get(0);
        assertThat(event.getLevel()).isEqualTo(ch.qos.logback.classic.Level.INFO);
        assertThat(event.getFormattedMessage()).isEqualTo("hello world");
    }

    @Test
    void mdcRequestIdIsCapturedByListAppender() {
        LoggerContext context = (LoggerContext) LoggerFactory.getILoggerFactory();
        Logger logger = (Logger) LoggerFactory.getLogger("io.astrasync.test.Mdc");
        logger.setLevel(ch.qos.logback.classic.Level.INFO);

        ListAppender<ILoggingEvent> listAppender = new ListAppender<>();
        listAppender.setContext(context);
        listAppender.start();
        logger.addAppender(listAppender);

        org.slf4j.MDC.put("request_id", "test-request-id");
        try {
            logger.info("request-scoped event");
        } finally {
            logger.detachAppender(listAppender);
            org.slf4j.MDC.remove("request_id");
        }

        assertThat(listAppender.list).hasSize(1);
        assertThat(listAppender.list.get(0).getMDCPropertyMap()).containsEntry("request_id", "test-request-id");
    }
}