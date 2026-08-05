package io.astrasync.engine.runtime;

import io.astrasync.connector.api.data.CdcBatch;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** Stable digest of the ordered source identities in one CDC batch. */
public final class CdcBatchDigests {
    private CdcBatchDigests() {}

    public static String sha256(CdcBatch batch) {
        if (batch == null) {
            throw new IllegalArgumentException("batch must not be null");
        }
        StringBuilder canonical = new StringBuilder("astrasync-cdc-v1|");
        batch.events().forEach(event -> canonical
                .append(event.getEventId())
                .append('|')
                .append(event.getOperation())
                .append('|')
                .append(event.getSourcePosition().getPositionId())
                .append(';'));
        try {
            byte[] bytes = MessageDigest.getInstance("SHA-256")
                    .digest(canonical.toString().getBytes(StandardCharsets.UTF_8));
            StringBuilder digest = new StringBuilder(bytes.length * 2);
            for (byte item : bytes) {
                digest.append(Character.forDigit((item >>> 4) & 0xf, 16));
                digest.append(Character.forDigit(item & 0xf, 16));
            }
            return digest.toString();
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is not available", exception);
        }
    }
}
