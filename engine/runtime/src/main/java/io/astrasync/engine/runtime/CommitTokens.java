package io.astrasync.engine.runtime;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** Stable logical commit identity shared by retries across execution epochs. */
public final class CommitTokens {
    private CommitTokens() {}

    public static String forBatch(String jobId, String splitId, long sequence, String batchDigest) {
        if (jobId == null
                || jobId.isBlank()
                || splitId == null
                || splitId.isBlank()
                || sequence <= 0
                || batchDigest == null
                || batchDigest.isBlank()) {
            throw new IllegalArgumentException("commit token identity is incomplete");
        }
        String canonical = "astrasync-v1|" + jobId + "|" + splitId + "|" + sequence + "|" + batchDigest;
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] bytes = digest.digest(canonical.getBytes(StandardCharsets.UTF_8));
            StringBuilder value = new StringBuilder(bytes.length * 2);
            for (byte item : bytes) {
                value.append(Character.forDigit((item >>> 4) & 0xf, 16));
                value.append(Character.forDigit(item & 0xf, 16));
            }
            return value.toString();
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is not available", exception);
        }
    }
}
