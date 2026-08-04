package io.astrasync.engine.checkpoint;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import java.io.IOException;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.StandardOpenOption;
import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;
import java.util.TreeMap;
import java.util.regex.Pattern;

/** Atomic JSON checkpoint store with one durable epoch counter per job. */
public final class FileCheckpointStore implements CheckpointStore {
    private static final int CURRENT_FORMAT_VERSION = 1;
    private static final Pattern JOB_ID = Pattern.compile("[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?");

    private final Path root;
    private final ObjectMapper mapper = new ObjectMapper()
            .enable(SerializationFeature.INDENT_OUTPUT)
            .enable(SerializationFeature.ORDER_MAP_ENTRIES_BY_KEYS);

    public FileCheckpointStore(Path root) {
        this.root = Objects.requireNonNull(root, "root must not be null")
                .toAbsolutePath()
                .normalize();
        try {
            Files.createDirectories(this.root);
        } catch (IOException exception) {
            throw new CheckpointStoreException("failed to create checkpoint directory", exception);
        }
        if (!Files.isDirectory(this.root)) {
            throw new IllegalArgumentException("checkpoint root must be a directory");
        }
    }

    @Override
    public synchronized long acquireEpoch(String jobId, SplitPlan plan) {
        String checkedJobId = requireJobId(jobId);
        SplitPlan checkedPlan = Objects.requireNonNull(plan, "plan must not be null");
        return locked(checkedJobId, () -> {
            Path manifest = manifest(checkedJobId);
            CheckpointState state =
                    Files.exists(manifest) ? read(manifest) : CheckpointState.empty(checkedJobId, checkedPlan);
            if (!state.plan().equals(checkedPlan)) {
                throw new SplitPlanMismatchException("split plan changed for job " + checkedJobId);
            }
            long epoch = state.nextExecutionEpoch();
            write(manifest, state.withNextExecutionEpoch(Math.addExact(epoch, 1)));
            return epoch;
        });
    }

    @Override
    public synchronized Optional<CheckpointRecord> load(String jobId, String splitId) {
        String checkedJobId = requireJobId(jobId);
        String checkedSplitId = requireText(splitId, "splitId");
        return locked(checkedJobId, () -> {
            Path manifest = manifest(checkedJobId);
            if (!Files.exists(manifest)) {
                return Optional.empty();
            }
            return Optional.ofNullable(read(manifest).records().get(checkedSplitId));
        });
    }

    @Override
    public synchronized Optional<CheckpointCompletion> loadCompletion(String jobId, String splitId) {
        String checkedJobId = requireJobId(jobId);
        String checkedSplitId = requireText(splitId, "splitId");
        return locked(checkedJobId, () -> {
            Path manifest = manifest(checkedJobId);
            if (!Files.exists(manifest)) {
                return Optional.empty();
            }
            return Optional.ofNullable(read(manifest).completedSplits().get(checkedSplitId));
        });
    }

    @Override
    public synchronized CheckpointRecord record(CheckpointRecord checkpoint) {
        CheckpointRecord checked = Objects.requireNonNull(checkpoint, "checkpoint must not be null");
        String checkedJobId = requireJobId(checked.jobId());
        return locked(checkedJobId, () -> {
            Path manifest = manifest(checkedJobId);
            if (!Files.exists(manifest)) {
                throw new CheckpointStoreException("checkpoint epoch was not acquired for job " + checkedJobId);
            }
            CheckpointState state = read(manifest);
            long activeEpoch = state.nextExecutionEpoch() - 1;
            if (checked.executionEpoch() != activeEpoch) {
                throw new StaleCheckpointException(
                        "checkpoint epoch " + checked.executionEpoch() + " is stale; active epoch is " + activeEpoch);
            }
            requirePlanIdentity(state.plan(), checked.splitId(), checked.splitFingerprint());
            if (state.completedSplits().containsKey(checked.splitId())) {
                throw new StaleCheckpointException("split is already complete: " + checked.splitId());
            }
            CheckpointRecord previous = state.records().get(checked.splitId());
            if (previous != null) {
                if (!previous.splitFingerprint().equals(checked.splitFingerprint())) {
                    throw new CheckpointStoreException("split fingerprint changed: " + checked.splitId());
                }
                if (checked.executionEpoch() < previous.executionEpoch()) {
                    throw new StaleCheckpointException("checkpoint epoch " + checked.executionEpoch()
                            + " is stale for split " + checked.splitId());
                }
                if (checked.executionEpoch() == previous.executionEpoch()
                        && checked.checkpointSequence() == previous.checkpointSequence()
                        && previous.equals(checked)) {
                    return previous;
                }
                long expected = Math.addExact(previous.checkpointSequence(), 1);
                if (checked.checkpointSequence() != expected) {
                    throw new StaleCheckpointException("checkpoint sequence must advance from "
                            + previous.checkpointSequence() + " to " + expected);
                }
            } else if (checked.checkpointSequence() != 1) {
                throw new StaleCheckpointException("first checkpoint sequence must be 1");
            }
            TreeMap<String, CheckpointRecord> updated = new TreeMap<>(state.records());
            updated.put(checked.splitId(), checked);
            write(manifest, state.withRecords(updated));
            return checked;
        });
    }

    @Override
    public synchronized CheckpointCompletion recordCompletion(CheckpointCompletion completion) {
        CheckpointCompletion checked = Objects.requireNonNull(completion, "completion must not be null");
        String checkedJobId = requireJobId(checked.jobId());
        return locked(checkedJobId, () -> {
            Path manifest = manifest(checkedJobId);
            if (!Files.exists(manifest)) {
                throw new CheckpointStoreException("checkpoint epoch was not acquired for job " + checkedJobId);
            }
            CheckpointState state = read(manifest);
            long activeEpoch = state.nextExecutionEpoch() - 1;
            if (checked.executionEpoch() != activeEpoch) {
                throw new StaleCheckpointException(
                        "completion epoch " + checked.executionEpoch() + " is stale; active epoch is " + activeEpoch);
            }
            requirePlanIdentity(state.plan(), checked.splitId(), checked.splitFingerprint());
            CheckpointCompletion previousCompletion = state.completedSplits().get(checked.splitId());
            if (previousCompletion != null) {
                if (previousCompletion.equals(checked)) {
                    return previousCompletion;
                }
                throw new StaleCheckpointException("split is already complete: " + checked.splitId());
            }
            CheckpointRecord checkpoint = state.records().get(checked.splitId());
            long expectedSequence = checkpoint == null ? 0 : checkpoint.checkpointSequence();
            if (checked.checkpointSequence() != expectedSequence) {
                throw new StaleCheckpointException("completion sequence " + checked.checkpointSequence()
                        + " does not match durable checkpoint sequence " + expectedSequence);
            }
            TreeMap<String, CheckpointCompletion> updated = new TreeMap<>(state.completedSplits());
            updated.put(checked.splitId(), checked);
            write(manifest, state.withCompletedSplits(updated));
            return checked;
        });
    }

    private <T> T locked(String jobId, IoOperation<T> operation) {
        Path lockPath = root.resolve(jobId + ".lock");
        try (FileChannel channel = FileChannel.open(lockPath, StandardOpenOption.CREATE, StandardOpenOption.WRITE)) {
            try (FileLock lock = channel.lock()) {
                if (!lock.isValid()) {
                    throw new IOException("checkpoint lock is not valid");
                }
                return operation.run();
            }
        } catch (CheckpointStoreException exception) {
            throw exception;
        } catch (IOException | ArithmeticException exception) {
            throw new CheckpointStoreException("failed to access checkpoint for job " + jobId, exception);
        }
    }

    private CheckpointState read(Path manifest) {
        try {
            return mapper.readValue(manifest.toFile(), CheckpointState.class);
        } catch (IOException | RuntimeException exception) {
            throw new CheckpointStoreException("failed to read checkpoint " + manifest.getFileName(), exception);
        }
    }

    private void write(Path manifest, CheckpointState state) {
        Path temporary = null;
        try {
            temporary = Files.createTempFile(root, state.jobId() + "-checkpoint-", ".tmp");
            Files.write(temporary, mapper.writeValueAsBytes(state), StandardOpenOption.TRUNCATE_EXISTING);
            try (FileChannel channel = FileChannel.open(temporary, StandardOpenOption.WRITE)) {
                channel.force(true);
            }
            try {
                Files.move(temporary, manifest, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING);
            } catch (AtomicMoveNotSupportedException exception) {
                throw new IOException("checkpoint volume does not support atomic replacement", exception);
            }
            temporary = null;
        } catch (IOException exception) {
            throw new CheckpointStoreException("failed to write checkpoint " + manifest.getFileName(), exception);
        } finally {
            if (temporary != null) {
                try {
                    Files.deleteIfExists(temporary);
                } catch (IOException ignored) {
                    // Preserve the primary durable-write failure.
                }
            }
        }
    }

    private Path manifest(String jobId) {
        return root.resolve(jobId + ".json");
    }

    private static void requirePlanIdentity(SplitPlan plan, String splitId, String splitFingerprint) {
        String expected = plan.splitFingerprints().get(splitId);
        if (expected == null) {
            throw new SplitPlanMismatchException("split is not part of the persisted plan: " + splitId);
        }
        if (!expected.equals(splitFingerprint)) {
            throw new SplitPlanMismatchException("split descriptor changed: " + splitId);
        }
    }

    private static String requireJobId(String value) {
        if (value == null || !JOB_ID.matcher(value).matches()) {
            throw new IllegalArgumentException("jobId must be a lowercase DNS label of at most 63 characters");
        }
        return value;
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }

    @FunctionalInterface
    private interface IoOperation<T> {
        T run() throws IOException;
    }

    private record CheckpointState(
            int formatVersion,
            String jobId,
            SplitPlan plan,
            long nextExecutionEpoch,
            Map<String, CheckpointRecord> records,
            Map<String, CheckpointCompletion> completedSplits) {
        private CheckpointState {
            if (formatVersion != CURRENT_FORMAT_VERSION) {
                throw new IllegalArgumentException("unsupported checkpoint store format: " + formatVersion);
            }
            requireJobId(jobId);
            SplitPlan checkedPlan = Objects.requireNonNull(plan, "plan must not be null");
            plan = checkedPlan;
            if (nextExecutionEpoch <= 0) {
                throw new IllegalArgumentException("nextExecutionEpoch must be positive");
            }
            TreeMap<String, CheckpointRecord> ordered = new TreeMap<>();
            Objects.requireNonNull(records, "records must not be null").forEach((splitId, record) -> {
                CheckpointRecord checked = Objects.requireNonNull(record, "checkpoint record must not be null");
                if (!splitId.equals(checked.splitId()) || !jobId.equals(checked.jobId())) {
                    throw new IllegalArgumentException("checkpoint record identity does not match its store");
                }
                requirePlanIdentity(checkedPlan, splitId, checked.splitFingerprint());
                ordered.put(splitId, checked);
            });
            records = Collections.unmodifiableMap(ordered);

            TreeMap<String, CheckpointCompletion> orderedCompletions = new TreeMap<>();
            Objects.requireNonNull(completedSplits, "completedSplits must not be null")
                    .forEach((splitId, completion) -> {
                        CheckpointCompletion checked =
                                Objects.requireNonNull(completion, "checkpoint completion must not be null");
                        if (!splitId.equals(checked.splitId()) || !jobId.equals(checked.jobId())) {
                            throw new IllegalArgumentException(
                                    "checkpoint completion identity does not match its store");
                        }
                        requirePlanIdentity(checkedPlan, splitId, checked.splitFingerprint());
                        orderedCompletions.put(splitId, checked);
                    });
            completedSplits = Collections.unmodifiableMap(orderedCompletions);
        }

        private static CheckpointState empty(String jobId, SplitPlan plan) {
            return new CheckpointState(CURRENT_FORMAT_VERSION, jobId, plan, 1, Map.of(), Map.of());
        }

        private CheckpointState withNextExecutionEpoch(long nextEpoch) {
            return new CheckpointState(formatVersion, jobId, plan, nextEpoch, records, completedSplits);
        }

        private CheckpointState withRecords(Map<String, CheckpointRecord> updated) {
            return new CheckpointState(formatVersion, jobId, plan, nextExecutionEpoch, updated, completedSplits);
        }

        private CheckpointState withCompletedSplits(Map<String, CheckpointCompletion> updated) {
            return new CheckpointState(formatVersion, jobId, plan, nextExecutionEpoch, records, updated);
        }
    }
}
