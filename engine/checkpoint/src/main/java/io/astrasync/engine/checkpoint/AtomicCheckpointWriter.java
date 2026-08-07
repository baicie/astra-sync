package io.astrasync.engine.checkpoint;

import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.channels.FileChannel;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.StandardOpenOption;
import java.util.Objects;

/** Writes one JSON manifest through a forced temporary file and atomic replacement. */
final class AtomicCheckpointWriter {
    private final ObjectMapper mapper;

    AtomicCheckpointWriter(ObjectMapper mapper) {
        this.mapper = Objects.requireNonNull(mapper, "mapper must not be null");
    }

    void write(Path manifest, Object value) throws IOException {
        Objects.requireNonNull(manifest, "manifest must not be null");
        Objects.requireNonNull(value, "value must not be null");
        Path parent = Objects.requireNonNull(manifest.toAbsolutePath().getParent(), "manifest parent must not be null");
        Path temporary = Files.createTempFile(parent, manifest.getFileName().toString() + "-", ".tmp");
        try {
            byte[] payload = mapper.writeValueAsBytes(value);
            try (FileChannel channel = FileChannel.open(temporary, StandardOpenOption.WRITE)) {
                ByteBuffer buffer = ByteBuffer.wrap(payload);
                while (buffer.hasRemaining()) {
                    channel.write(buffer);
                }
                channel.force(true);
            }
            try {
                Files.move(temporary, manifest, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING);
            } catch (AtomicMoveNotSupportedException exception) {
                throw new IOException("checkpoint volume does not support atomic replacement", exception);
            }
        } catch (IOException | RuntimeException exception) {
            try {
                Files.deleteIfExists(temporary);
            } catch (IOException cleanupFailure) {
                exception.addSuppressed(cleanupFailure);
            }
            throw exception;
        }
        Files.deleteIfExists(temporary);
    }
}
