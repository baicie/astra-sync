package io.astrasync.engine.checkpoint;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.engine.runtime.WorkerResult;
import java.io.IOException;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.StandardOpenOption;
import java.util.Objects;
import java.util.Optional;

/** Atomic JSON split-progress store intended for one active Coordinator and a durable volume. */
public final class FileSplitProgressStore implements SplitProgressStore {
    private final Path root;
    private final ObjectMapper mapper = new ObjectMapper()
            .enable(SerializationFeature.INDENT_OUTPUT)
            .enable(SerializationFeature.ORDER_MAP_ENTRIES_BY_KEYS);

    public FileSplitProgressStore(Path root) {
        this.root = Objects.requireNonNull(root, "root must not be null")
                .toAbsolutePath()
                .normalize();
        try {
            Files.createDirectories(this.root);
        } catch (IOException exception) {
            throw new SplitProgressException("failed to create split progress directory", exception);
        }
        if (!Files.isDirectory(this.root)) {
            throw new IllegalArgumentException("split progress root must be a directory");
        }
    }

    @Override
    public synchronized FullLoadProgress open(String jobId, SplitPlan plan) {
        String checkedJobId = FullLoadProgress.requireJobId(jobId);
        SplitPlan checkedPlan = Objects.requireNonNull(plan, "plan must not be null");
        return locked(checkedJobId, () -> {
            Path manifest = manifest(checkedJobId);
            if (!Files.exists(manifest)) {
                FullLoadProgress created = FullLoadProgress.create(checkedJobId, checkedPlan);
                write(manifest, created);
                return created;
            }
            FullLoadProgress existing = read(manifest);
            if (!existing.plan().equals(checkedPlan)) {
                throw new SplitPlanMismatchException("split plan changed for job " + checkedJobId);
            }
            return existing;
        });
    }

    @Override
    public synchronized Optional<FullLoadProgress> load(String jobId) {
        String checkedJobId = FullLoadProgress.requireJobId(jobId);
        return locked(checkedJobId, () -> {
            Path manifest = manifest(checkedJobId);
            return Files.exists(manifest) ? Optional.of(read(manifest)) : Optional.empty();
        });
    }

    @Override
    public synchronized FullLoadProgress recordCompletion(
            String jobId, String planFingerprint, SourceSplit split, WorkerResult result) {
        String checkedJobId = FullLoadProgress.requireJobId(jobId);
        Objects.requireNonNull(planFingerprint, "planFingerprint must not be null");
        return locked(checkedJobId, () -> {
            Path manifest = manifest(checkedJobId);
            if (!Files.exists(manifest)) {
                throw new SplitProgressException("split progress does not exist for job " + checkedJobId, null);
            }
            FullLoadProgress existing = read(manifest);
            if (!existing.plan().fingerprint().equals(planFingerprint)) {
                throw new SplitPlanMismatchException("split plan changed for job " + checkedJobId);
            }
            FullLoadProgress updated = existing.withCompletion(split, result);
            if (updated != existing) {
                write(manifest, updated);
            }
            return updated;
        });
    }

    private <T> T locked(String jobId, IoOperation<T> operation) {
        Path lockPath = root.resolve(jobId + ".lock");
        try (FileChannel channel = FileChannel.open(lockPath, StandardOpenOption.CREATE, StandardOpenOption.WRITE)) {
            try (FileLock lock = channel.lock()) {
                if (!lock.isValid()) {
                    throw new IOException("split progress lock is not valid");
                }
                return operation.run();
            }
        } catch (SplitPlanMismatchException | SplitProgressException exception) {
            throw exception;
        } catch (IOException exception) {
            throw new SplitProgressException("failed to access split progress for job " + jobId, exception);
        }
    }

    private FullLoadProgress read(Path manifest) {
        try {
            return mapper.readValue(manifest.toFile(), FullLoadProgress.class);
        } catch (IOException | RuntimeException exception) {
            throw new SplitProgressException("failed to read split progress " + manifest.getFileName(), exception);
        }
    }

    private void write(Path manifest, FullLoadProgress progress) {
        Path temporary = null;
        try {
            temporary = Files.createTempFile(root, progress.jobId() + "-", ".tmp");
            byte[] payload = mapper.writeValueAsBytes(progress);
            Files.write(temporary, payload, StandardOpenOption.TRUNCATE_EXISTING);
            try (FileChannel channel = FileChannel.open(temporary, StandardOpenOption.WRITE)) {
                channel.force(true);
            }
            try {
                Files.move(temporary, manifest, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING);
            } catch (AtomicMoveNotSupportedException exception) {
                throw new IOException("split progress volume does not support atomic replacement", exception);
            }
            temporary = null;
        } catch (IOException exception) {
            throw new SplitProgressException("failed to write split progress " + manifest.getFileName(), exception);
        } finally {
            if (temporary != null) {
                try {
                    Files.deleteIfExists(temporary);
                } catch (IOException ignored) {
                    // The durable manifest write already failed; retain its primary exception.
                }
            }
        }
    }

    private Path manifest(String jobId) {
        return root.resolve(jobId + ".json");
    }

    @FunctionalInterface
    private interface IoOperation<T> {
        T run() throws IOException;
    }
}
