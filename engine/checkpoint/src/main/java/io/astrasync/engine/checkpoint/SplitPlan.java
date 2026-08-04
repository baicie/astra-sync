package io.astrasync.engine.checkpoint;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import io.astrasync.connector.api.source.SourceSplit;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HexFormat;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;
import java.util.regex.Pattern;

/** Stable identity of a complete enumerated split set. */
public record SplitPlan(String fingerprint, Map<String, String> splitFingerprints) {
    private static final Pattern SHA_256 = Pattern.compile("[0-9a-f]{64}");
    private static final ObjectMapper MAPPER =
            new ObjectMapper().enable(SerializationFeature.ORDER_MAP_ENTRIES_BY_KEYS);

    public SplitPlan {
        fingerprint = requireFingerprint(fingerprint, "fingerprint");
        TreeMap<String, String> ordered = new TreeMap<>();
        Objects.requireNonNull(splitFingerprints, "splitFingerprints must not be null")
                .forEach((splitId, splitFingerprint) -> ordered.put(
                        requireText(splitId, "split id"), requireFingerprint(splitFingerprint, "split fingerprint")));
        splitFingerprints = Collections.unmodifiableMap(ordered);
    }

    public static SplitPlan from(List<? extends SourceSplit> splits) {
        Objects.requireNonNull(splits, "splits must not be null");
        List<CanonicalSplit> canonical = new ArrayList<>(splits.size());
        TreeMap<String, String> fingerprints = new TreeMap<>();
        for (SourceSplit split : splits) {
            SourceSplit checked = Objects.requireNonNull(split, "splits must not contain null");
            CanonicalSplit descriptor = CanonicalSplit.from(checked);
            String previous = fingerprints.putIfAbsent(checked.splitId(), hash(descriptor));
            if (previous != null) {
                throw new IllegalArgumentException("split id is duplicated: " + checked.splitId());
            }
            canonical.add(descriptor);
        }
        canonical.sort(Comparator.comparing(CanonicalSplit::splitId));
        return new SplitPlan(hash(canonical), fingerprints);
    }

    public String requireMatchingSplit(SourceSplit split) {
        SourceSplit checked = Objects.requireNonNull(split, "split must not be null");
        String expected = splitFingerprints.get(checked.splitId());
        if (expected == null) {
            throw new SplitPlanMismatchException("split is not part of the persisted plan: " + checked.splitId());
        }
        String actual = hash(CanonicalSplit.from(checked));
        if (!expected.equals(actual)) {
            throw new SplitPlanMismatchException("split descriptor changed: " + checked.splitId());
        }
        return expected;
    }

    private static String hash(Object value) {
        try {
            byte[] canonicalJson = MAPPER.writeValueAsString(value).getBytes(StandardCharsets.UTF_8);
            return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(canonicalJson));
        } catch (JsonProcessingException | NoSuchAlgorithmException exception) {
            throw new IllegalStateException("failed to fingerprint split plan", exception);
        }
    }

    private static String requireFingerprint(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (!SHA_256.matcher(value).matches()) {
            throw new IllegalArgumentException(name + " must be a lowercase SHA-256 value");
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

    private record CanonicalSplit(
            String splitId, String sourceId, Map<String, String> startOffsets, Map<String, String> endOffsets) {
        private static CanonicalSplit from(SourceSplit split) {
            return new CanonicalSplit(
                    split.splitId(),
                    split.sourceId(),
                    split.start().offsets(),
                    split.end().offsets());
        }
    }
}
