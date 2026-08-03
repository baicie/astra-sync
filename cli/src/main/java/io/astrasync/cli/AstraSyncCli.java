package io.astrasync.cli;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.astrasync.connector.file.CsvConnectorFactory;
import io.astrasync.connector.jdbc.JdbcConnectorFactory;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.jobspec.JobSpecParser;
import io.astrasync.engine.kernel.SyncJobException;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.kernel.SyncStage;
import io.astrasync.engine.local.LocalJobRunner;
import io.astrasync.engine.local.LocalRunResult;
import io.astrasync.engine.plan.ConnectorRegistry;
import java.io.IOException;
import java.io.PrintWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.Objects;
import java.util.concurrent.Callable;
import java.util.function.Supplier;
import picocli.CommandLine;
import picocli.CommandLine.Command;
import picocli.CommandLine.Model.CommandSpec;
import picocli.CommandLine.Option;
import picocli.CommandLine.Parameters;
import picocli.CommandLine.Spec;

@Command(
        name = "astrasync",
        description = "Run bounded AstraSync jobs in the current process.",
        mixinStandardHelpOptions = true,
        version = AstraSyncCli.VERSION)
public final class AstraSyncCli implements Callable<Integer> {
    static final String VERSION = "AstraSync 0.1.0-SNAPSHOT";
    public static final int EXIT_SUCCESS = 0;
    public static final int EXIT_INPUT = 2;
    public static final int EXIT_VALIDATION = 3;
    public static final int EXIT_RUNTIME = 4;
    public static final int EXIT_CANCELLED = 5;

    @Spec
    private CommandSpec commandSpec;

    @Override
    public Integer call() {
        commandSpec.commandLine().usage(commandSpec.commandLine().getErr());
        return EXIT_INPUT;
    }

    public static void main(String[] args) {
        System.exit(execute(args));
    }

    public static int execute(String... args) {
        return newCommandLine(defaultRunner(), new PrintWriter(System.out, true), new PrintWriter(System.err, true))
                .execute(args);
    }

    static CommandLine newCommandLine(Supplier<LocalJobRunner> runner, PrintWriter out, PrintWriter err) {
        Objects.requireNonNull(runner, "runner must not be null");
        CommandLine commandLine = new CommandLine(new AstraSyncCli());
        commandLine.addSubcommand("run", new RunCommand(runner));
        commandLine.setOut(Objects.requireNonNull(out, "out must not be null"));
        commandLine.setErr(Objects.requireNonNull(err, "err must not be null"));
        commandLine.setParameterExceptionHandler(
                (exception, arguments) -> reportParameterFailure(commandLine, arguments));
        return commandLine;
    }

    private static int reportParameterFailure(CommandLine commandLine, String[] arguments) {
        if (requestsJsonReport(arguments)) {
            LinkedHashMap<String, Object> report = new LinkedHashMap<>();
            report.put("status", "FAILED");
            report.put("category", "input");
            report.put("message", "invalid command arguments");
            commandLine.getErr().println(toJson(report));
        } else {
            commandLine.getErr().println("FAILED category=input message=invalid command arguments");
        }
        return EXIT_INPUT;
    }

    private static boolean requestsJsonReport(String[] arguments) {
        String selectedFormat = null;
        for (int index = 0; index < arguments.length; index++) {
            String argument = arguments[index];
            if ("--".equals(argument)) {
                break;
            }
            if ("--metrics".equals(argument)
                    && index + 1 < arguments.length
                    && arguments[index + 1] != null
                    && !arguments[index + 1].startsWith("-")) {
                selectedFormat = arguments[++index];
            } else if (argument != null && argument.startsWith("--metrics=")) {
                selectedFormat = argument.substring("--metrics=".length());
            }
        }
        return "json".equalsIgnoreCase(selectedFormat);
    }

    private static Supplier<LocalJobRunner> defaultRunner() {
        return () -> new LocalJobRunner(ConnectorRegistry.of(new CsvConnectorFactory(), new JdbcConnectorFactory()));
    }

    @Command(
            name = "run",
            description = "Compile and run one UTF-8 JobSpec.",
            mixinStandardHelpOptions = true,
            version = AstraSyncCli.VERSION)
    static final class RunCommand implements Callable<Integer> {
        private final Supplier<LocalJobRunner> runner;

        @Spec
        private CommandSpec commandSpec;

        @Parameters(index = "0", paramLabel = "<job-spec>", description = "Path to a JobSpec YAML or JSON file.")
        private Path jobSpecPath;

        @Option(
                names = "--metrics",
                defaultValue = "text",
                description = "Metrics output format: text or json (default: ${DEFAULT-VALUE}).")
        private String reportFormat;

        private RunCommand(Supplier<LocalJobRunner> runner) {
            this.runner = runner;
        }

        @Override
        public Integer call() {
            if (!isSupportedReportFormat()) {
                reportFailure("input", "invalid metrics format", null, null);
                return EXIT_INPUT;
            }
            String document;
            try {
                document = Files.readString(jobSpecPath, StandardCharsets.UTF_8);
            } catch (IOException | RuntimeException exception) {
                reportFailure("input", "cannot read JobSpec: " + exception.getMessage(), null, null);
                return EXIT_INPUT;
            }

            try {
                JobSpec jobSpec = new JobSpecParser().parse(document);
                LocalRunResult result = runner.get().run(jobSpec);
                reportSuccess(result);
                return EXIT_SUCCESS;
            } catch (SyncJobException exception) {
                String category = exception.stage() == SyncStage.CANCELLED ? "cancelled" : "runtime";
                String text = "stage=" + exception.stage() + " " + exception.getMessage() + " recordsRead="
                        + exception.partialResult().readCount() + " recordsWritten="
                        + exception.partialResult().writtenCount();
                reportFailure(category, text, exception.stage().name(), exception.partialResult());
                return exception.stage() == SyncStage.CANCELLED ? EXIT_CANCELLED : EXIT_RUNTIME;
            } catch (IllegalArgumentException exception) {
                reportFailure("validation", exception.getMessage(), null, null);
                return EXIT_VALIDATION;
            } catch (RuntimeException exception) {
                reportFailure("runtime", "runtime execution failed", "UNKNOWN", SyncResult.empty());
                return EXIT_RUNTIME;
            }
        }

        private void reportSuccess(LocalRunResult result) {
            if (isJsonReport()) {
                LinkedHashMap<String, Object> report = new LinkedHashMap<>();
                report.put("status", "SUCCEEDED");
                report.put("job", result.plan().jobName());
                report.put(
                        "deliveryGuarantee", result.plan().deliveryGuarantee().externalName());
                addMetrics(report, result.metrics());
                commandSpec.commandLine().getOut().println(toJson(report));
                return;
            }
            commandSpec.commandLine().getOut().println(successText(result));
        }

        private String successText(LocalRunResult result) {
            return "SUCCEEDED job=" + result.plan().jobName() + " recordsRead="
                    + result.metrics().readCount()
                    + " recordsWritten=" + result.metrics().writtenCount() + " batches="
                    + result.metrics().batchCount() + " maxBatchRecords="
                    + result.metrics().maxObservedBatchSize() + " elapsedMillis="
                    + result.metrics().elapsedNanos() / 1_000_000;
        }

        private void reportFailure(String category, String textMessage, String stage, SyncResult partialResult) {
            if (isJsonReport()) {
                LinkedHashMap<String, Object> report = new LinkedHashMap<>();
                report.put("status", "FAILED");
                report.put("category", category);
                if (stage != null) {
                    report.put("stage", stage);
                }
                report.put("message", jsonMessage(category, textMessage));
                if (partialResult != null) {
                    addMetrics(report, partialResult);
                }
                commandSpec.commandLine().getErr().println(toJson(report));
                return;
            }
            commandSpec
                    .commandLine()
                    .getErr()
                    .println("FAILED category=" + category + " message=" + singleLine(textMessage));
        }

        private static void addMetrics(LinkedHashMap<String, Object> report, SyncResult metrics) {
            report.put("recordsRead", metrics.readCount());
            report.put("recordsWritten", metrics.writtenCount());
            report.put("batches", metrics.batchCount());
            report.put("maxBatchRecords", metrics.maxObservedBatchSize());
            report.put("elapsedMillis", metrics.elapsedNanos() / 1_000_000);
        }

        private static String jsonMessage(String category, String message) {
            if ("input".equals(category)) {
                return "cannot read JobSpec";
            }
            if ("runtime".equals(category) && message != null && message.startsWith("stage=")) {
                int separator = message.indexOf(' ');
                String stage = separator > 0 ? message.substring("stage=".length(), separator) : "UNKNOWN";
                return "runtime failure at " + stage;
            }
            if ("cancelled".equals(category)) {
                return "job cancelled";
            }
            if ("validation".equals(category)) {
                return "job validation failed";
            }
            return "runtime execution failed";
        }

        private boolean isSupportedReportFormat() {
            return "text".equalsIgnoreCase(reportFormat) || "json".equalsIgnoreCase(reportFormat);
        }

        private boolean isJsonReport() {
            return "json".equalsIgnoreCase(reportFormat);
        }

        private static String singleLine(String message) {
            if (message == null || message.isBlank()) {
                return "unspecified failure";
            }
            return message.replace('\r', ' ').replace('\n', ' ');
        }
    }

    private static String toJson(LinkedHashMap<String, Object> report) {
        try {
            return OBJECT_MAPPER.writeValueAsString(report);
        } catch (JsonProcessingException exception) {
            throw new IllegalStateException("failed to serialize metrics report", exception);
        }
    }

    private static final ObjectMapper OBJECT_MAPPER = new ObjectMapper();
}
