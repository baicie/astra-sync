package io.astrasync.connector.debezium;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

/** Stable, secret-free connector identity used to validate checkpoint ownership. */
public final class DebeziumIdentities {
    private DebeziumIdentities() {}

    public static String forConfiguration(String connectorType, Map<String, String> identityFields) {
        Objects.requireNonNull(identityFields, "identityFields must not be null");
        TreeMap<String, String> ordered = new TreeMap<>();
        identityFields.forEach(
                (key, value) -> ordered.put(requireText(key, "identity field"), requireText(value, "identity value")));
        String canonical = requireText(connectorType, "connectorType") + "|v1|" + ordered;
        try {
            byte[] bytes = MessageDigest.getInstance("SHA-256").digest(canonical.getBytes(StandardCharsets.UTF_8));
            StringBuilder digest = new StringBuilder(bytes.length * 2);
            for (byte item : bytes) {
                digest.append(Character.forDigit((item >>> 4) & 0xf, 16));
                digest.append(Character.forDigit(item & 0xf, 16));
            }
            return connectorType + ":v1:" + digest;
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is not available", exception);
        }
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
