package io.astrasync.cli;

import static org.assertj.core.api.Assertions.assertThat;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.astrasync.connector.file.CsvConnectorFactory;
import io.astrasync.engine.kernel.SyncJobException;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.kernel.SyncStage;
import io.astrasync.engine.local.LocalJobRunner;
import io.astrasync.engine.plan.ConnectorRegistry;
import java.io.IOException;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import picocli.CommandLine;

class AstraSyncCliTest {
    @TempDir
    Path tempDirectory;

    @Test
    void exposesHelpVersionAndUsageWithoutAStackTrace() {
        Invocation help = invoke("--help");
        Invocation version = invoke("--version");
        Invocation missingCommand = invoke();
        Invocation unknown = invoke("unknown");

        assertThat(help.exitCode()).isZero();
        assertThat(help.stdout()).contains("Usage: astrasync", "run");
        assertThat(version.exitCode()).isZero();
        assertThat(version.stdout()).contains("AstraSync 0.1.0-SNAPSHOT");
        assertThat(missingCommand.exitCode()).isEqualTo(AstraSyncCli.EXIT_INPUT);
        assertThat(missingCommand.stderr()).contains("Usage: astrasync");
        assertThat(unknown.exitCode()).isEqualTo(AstraSyncCli.EXIT_INPUT);
        assertThat(unknown.stderr()).contains("Unmatched argument").doesNotContain("Exception", "\tat ");
    }

    @Test
    void reportsUnreadableInputAsExitTwo() {
        Invocation invocation =
                invoke("run", tempDirectory.resolve("missing.yaml").toString());

        assertThat(invocation.exitCode()).isEqualTo(AstraSyncCli.EXIT_INPUT);
        assertThat(invocation.stderr())
                .contains("FAILED category=input", "cannot read JobSpec")
                .doesNotContain("Exception", "\tat ");
    }

    @Test
    void reportsParserCompilerAndConnectorConfigurationErrorsAsValidation() throws IOException {
        Path invalid = write("invalid.yaml", "kind: Wrong\n");
        Invocation parseFailure = invoke("run", invalid.toString());

        Path exactOnce = writeJob(
                "exactly-once.yaml",
                tempDirectory.resolve("input.csv"),
                tempDirectory.resolve("output.csv"),
                "exactly-once",
                "");
        Invocation compileFailure = invoke("run", exactOnce.toString());

        Path configuredInput = write("configured.csv", "id\r\n1\r\n");
        Path invalidOptions = writeJob(
                "invalid-options.yaml",
                configuredInput,
                tempDirectory.resolve("configured-output.csv"),
                "at-most-once",
                "      header: 'true'\n");
        Invocation optionFailure = invoke("run", invalidOptions.toString());

        assertThat(parseFailure.exitCode()).isEqualTo(AstraSyncCli.EXIT_VALIDATION);
        assertThat(parseFailure.stderr()).contains("FAILED category=validation").doesNotContain("\tat ");
        assertThat(compileFailure.exitCode()).isEqualTo(AstraSyncCli.EXIT_VALIDATION);
        assertThat(compileFailure.stderr())
                .contains("supports only at-most-once")
                .doesNotContain("\tat ");
        assertThat(optionFailure.exitCode()).isEqualTo(AstraSyncCli.EXIT_VALIDATION);
        assertThat(optionFailure.stderr()).contains("unknown CSV connector option 'header'");
        assertThat(tempDirectory.resolve("configured-output.csv")).doesNotExist();
    }

    @Test
    void runsARealBoundedFileToFileJob() throws IOException {
        Path input = write(
                "input.csv",
                "\uFEFFid,text,nullable,empty\r\n"
                        + "1,\"hello, \"\"world\"\"\",\\N,\r\n"
                        + "2,\"line1\r\nline2 你好\",value,\r\n");
        Path output = tempDirectory.resolve("output.csv");
        Path job = writeJob(
                "job.yaml", input, output, "at-most-once", "      nullValue: '\\N'\n", "      nullValue: '\\N'\n");

        Invocation invocation = invoke("run", job.toString());

        assertThat(invocation.exitCode()).isZero();
        assertThat(invocation.stderr()).isEmpty();
        assertThat(invocation.stdout())
                .contains("SUCCEEDED job=cli-test", "recordsRead=2", "recordsWritten=2", "maxBatchRecords=1")
                .doesNotContain(input.toString(), output.toString());
        assertThat(Files.readString(output, StandardCharsets.UTF_8))
                .isEqualTo("id,text,nullable,empty\r\n"
                        + "1,\"hello, \"\"world\"\"\",\\N,\r\n"
                        + "2,\"line1\r\nline2 你好\",value,\r\n");
    }

    @Test
    void emitsOneMachineReadableSuccessObjectWithoutConnectorValues() throws IOException {
        Path input = write("json-input.csv", "id\r\n1\r\n");
        Path output = tempDirectory.resolve("json-output.csv");
        Path job = writeJob("json-job.yaml", input, output, "at-most-once", "");

        Invocation invocation = invoke("run", "--metrics", "json", job.toString());
        JsonNode report = new ObjectMapper().readTree(invocation.stdout());

        assertThat(invocation.exitCode()).isZero();
        assertThat(invocation.stderr()).isEmpty();
        assertThat(report.isObject()).isTrue();
        assertThat(report.get("status").asText()).isEqualTo("SUCCEEDED");
        assertThat(report.get("job").asText()).isEqualTo("cli-test");
        assertThat(report.get("deliveryGuarantee").asText()).isEqualTo("at-most-once");
        assertThat(report.get("recordsRead").asInt()).isEqualTo(1);
        assertThat(report.get("recordsWritten").asInt()).isEqualTo(1);
        assertThat(invocation.stdout()).doesNotContain(input.toString(), output.toString(), "password", "secret");
    }

    @Test
    void emitsRedactedJsonRuntimeFailureWithPartialCounters() throws IOException {
        Path input = write("json-malformed.csv", "id,name\r\n1,Ada\r\n2\r\n");
        Path output = tempDirectory.resolve("json-partial.csv");
        Path job = writeJob("json-malformed-job.yaml", input, output, "at-most-once", "");

        Invocation invocation = invoke("run", "--metrics", "json", job.toString());
        JsonNode report = new ObjectMapper().readTree(invocation.stderr());

        assertThat(invocation.exitCode()).isEqualTo(AstraSyncCli.EXIT_RUNTIME);
        assertThat(invocation.stdout()).isEmpty();
        assertThat(report.get("status").asText()).isEqualTo("FAILED");
        assertThat(report.get("category").asText()).isEqualTo("runtime");
        assertThat(report.get("stage").asText()).isEqualTo("SOURCE_READ");
        assertThat(report.get("recordsRead").asInt()).isEqualTo(1);
        assertThat(report.get("recordsWritten").asInt()).isEqualTo(1);
        assertThat(invocation.stderr()).doesNotContain(input.toString(), output.toString(), "password", "secret");
    }

    @Test
    void mapsCancellationToJsonAndExitCodeFive() throws IOException {
        Path input = write("json-cancel-input.csv", "id\r\n1\r\n");
        Path output = tempDirectory.resolve("json-cancel-output.csv");
        Path job = writeJob("json-cancel-job.yaml", input, output, "at-most-once", "");
        SyncJobException cancellation =
                new SyncJobException(SyncStage.CANCELLED, "job cancelled", null, new SyncResult(1, 0, 1, 1, 1));

        Invocation invocation = invokeWithRunner(
                () -> {
                    throw cancellation;
                },
                "run",
                "--metrics",
                "json",
                job.toString());
        JsonNode report = new ObjectMapper().readTree(invocation.stderr());

        assertThat(invocation.exitCode()).isEqualTo(AstraSyncCli.EXIT_CANCELLED);
        assertThat(report.get("status").asText()).isEqualTo("FAILED");
        assertThat(report.get("category").asText()).isEqualTo("cancelled");
        assertThat(report.get("stage").asText()).isEqualTo("CANCELLED");
        assertThat(report.get("recordsRead").asInt()).isEqualTo(1);
        assertThat(invocation.stderr()).doesNotContain(input.toString(), output.toString(), "password", "secret");
    }

    @Test
    void preservesExistingOutputAndReportsRuntimeStage() throws IOException {
        Path input = write("existing-input.csv", "id\r\n1\r\n");
        Path output = write("existing-output.csv", "original");
        Path job = writeJob("existing-job.yaml", input, output, "at-most-once", "");

        Invocation invocation = invoke("run", job.toString());

        assertThat(invocation.exitCode()).isEqualTo(AstraSyncCli.EXIT_RUNTIME);
        assertThat(invocation.stderr())
                .contains("FAILED category=runtime", "stage=SINK_OPEN", "recordsRead=0", "recordsWritten=0")
                .doesNotContain("\tat ");
        assertThat(Files.readString(output, StandardCharsets.UTF_8)).isEqualTo("original");
    }

    @Test
    void refusesToUseTheInputFileAsItsOwnOutput() throws IOException {
        Path input = write("same-path.csv", "id\r\n1\r\n");
        Path job = writeJob("same-path-job.yaml", input, input, "at-most-once", "");

        Invocation invocation = invoke("run", job.toString());

        assertThat(invocation.exitCode()).isEqualTo(AstraSyncCli.EXIT_RUNTIME);
        assertThat(invocation.stderr()).contains("stage=SINK_OPEN");
        assertThat(Files.readString(input, StandardCharsets.UTF_8)).isEqualTo("id\r\n1\r\n");
    }

    @Test
    void leavesAndReportsPartialOutputAfterALaterMalformedRecord() throws IOException {
        Path input = write("malformed.csv", "id,name\r\n1,Ada\r\n2\r\n");
        Path output = tempDirectory.resolve("partial.csv");
        Path job = writeJob("malformed-job.yaml", input, output, "at-most-once", "");

        Invocation invocation = invoke("run", job.toString());

        assertThat(invocation.exitCode()).isEqualTo(AstraSyncCli.EXIT_RUNTIME);
        assertThat(invocation.stderr())
                .contains("FAILED category=runtime", "stage=SOURCE_READ", "recordsRead=1", "recordsWritten=1")
                .doesNotContain("\tat ");
        assertThat(Files.readString(output, StandardCharsets.UTF_8)).isEqualTo("id,name\r\n1,Ada\r\n");
    }

    private Invocation invoke(String... args) {
        return invokeWithRunner(() -> new LocalJobRunner(ConnectorRegistry.of(new CsvConnectorFactory())), args);
    }

    private Invocation invokeWithRunner(java.util.function.Supplier<LocalJobRunner> runner, String... args) {
        StringWriter stdout = new StringWriter();
        StringWriter stderr = new StringWriter();
        CommandLine commandLine =
                AstraSyncCli.newCommandLine(runner, new PrintWriter(stdout, true), new PrintWriter(stderr, true));
        int exitCode = commandLine.execute(args);
        return new Invocation(exitCode, stdout.toString(), stderr.toString());
    }

    private Path writeJob(String name, Path input, Path output, String guarantee, String sourceExtraOptions)
            throws IOException {
        return writeJob(name, input, output, guarantee, sourceExtraOptions, "");
    }

    private Path writeJob(
            String name, Path input, Path output, String guarantee, String sourceExtraOptions, String sinkExtraOptions)
            throws IOException {
        String document =
                """
                apiVersion: sync.astrasync.io/v1
                kind: SyncJob
                metadata:
                  name: cli-test
                spec:
                  source:
                    connector: csv
                    options:
                      path: '%s'
                %s  sink:
                    connector: csv
                    options:
                      path: '%s'
                %s  delivery:
                    guarantee: %s
                  runtime:
                    maxBatchRecords: 1
                """
                        .formatted(
                                yamlSingleQuoted(input),
                                sourceExtraOptions,
                                yamlSingleQuoted(output),
                                sinkExtraOptions,
                                guarantee);
        return write(name, document);
    }

    private Path write(String name, String content) throws IOException {
        Path path = tempDirectory.resolve(name);
        Files.writeString(path, content, StandardCharsets.UTF_8);
        return path;
    }

    private static String yamlSingleQuoted(Path path) {
        return path.toString().replace("'", "''");
    }

    private record Invocation(int exitCode, String stdout, String stderr) {}
}
