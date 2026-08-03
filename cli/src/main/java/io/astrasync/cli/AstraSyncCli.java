package io.astrasync.cli;

import io.astrasync.connector.file.CsvConnectorFactory;
import io.astrasync.connector.jdbc.JdbcConnectorFactory;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.jobspec.JobSpecParser;
import io.astrasync.engine.kernel.SyncJobException;
import io.astrasync.engine.local.LocalJobRunner;
import io.astrasync.engine.local.LocalRunResult;
import io.astrasync.engine.plan.ConnectorRegistry;
import java.io.IOException;
import java.io.PrintWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Objects;
import java.util.concurrent.Callable;
import java.util.function.Supplier;
import picocli.CommandLine;
import picocli.CommandLine.Command;
import picocli.CommandLine.Model.CommandSpec;
import picocli.CommandLine.Parameters;
import picocli.CommandLine.Spec;

@Command(
        name = "astrasync",
        description = "Run bounded AstraSync jobs in the current process.",
        mixinStandardHelpOptions = true,
        version = "AstraSync 0.1.0-SNAPSHOT")
public final class AstraSyncCli implements Callable<Integer> {
    public static final int EXIT_SUCCESS = 0;
    public static final int EXIT_INPUT = 2;
    public static final int EXIT_VALIDATION = 3;
    public static final int EXIT_RUNTIME = 4;

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
        return commandLine;
    }

    private static Supplier<LocalJobRunner> defaultRunner() {
        return () -> new LocalJobRunner(ConnectorRegistry.of(new CsvConnectorFactory(), new JdbcConnectorFactory()));
    }

    @Command(name = "run", description = "Compile and run one UTF-8 JobSpec.", mixinStandardHelpOptions = true)
    static final class RunCommand implements Callable<Integer> {
        private final Supplier<LocalJobRunner> runner;

        @Spec
        private CommandSpec commandSpec;

        @Parameters(index = "0", paramLabel = "<job-spec>", description = "Path to a JobSpec YAML or JSON file.")
        private Path jobSpecPath;

        private RunCommand(Supplier<LocalJobRunner> runner) {
            this.runner = runner;
        }

        @Override
        public Integer call() {
            String document;
            try {
                document = Files.readString(jobSpecPath, StandardCharsets.UTF_8);
            } catch (IOException | RuntimeException exception) {
                failure("input", "cannot read JobSpec: " + exception.getMessage());
                return EXIT_INPUT;
            }

            try {
                JobSpec jobSpec = new JobSpecParser().parse(document);
                LocalRunResult result = runner.get().run(jobSpec);
                commandSpec.commandLine().getOut().println(success(result));
                return EXIT_SUCCESS;
            } catch (SyncJobException exception) {
                failure(
                        "runtime",
                        "stage=" + exception.stage() + " " + exception.getMessage() + " recordsRead="
                                + exception.partialResult().readCount() + " recordsWritten="
                                + exception.partialResult().writtenCount());
                return EXIT_RUNTIME;
            } catch (IllegalArgumentException exception) {
                failure("validation", exception.getMessage());
                return EXIT_VALIDATION;
            } catch (RuntimeException exception) {
                failure("runtime", exception.getMessage());
                return EXIT_RUNTIME;
            }
        }

        private String success(LocalRunResult result) {
            return "SUCCEEDED job=" + result.plan().jobName() + " recordsRead="
                    + result.metrics().readCount()
                    + " recordsWritten=" + result.metrics().writtenCount() + " batches="
                    + result.metrics().batchCount() + " maxBatchRecords="
                    + result.metrics().maxObservedBatchSize() + " elapsedMillis="
                    + result.metrics().elapsedNanos() / 1_000_000;
        }

        private void failure(String category, String message) {
            commandSpec
                    .commandLine()
                    .getErr()
                    .println("FAILED category=" + category + " message=" + singleLine(message));
        }

        private static String singleLine(String message) {
            if (message == null || message.isBlank()) {
                return "unspecified failure";
            }
            return message.replace('\r', ' ').replace('\n', ' ');
        }
    }
}
