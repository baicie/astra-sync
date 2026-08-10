package io.astrasync.engine.runtime;

import com.fasterxml.jackson.core.JsonFactory;
import com.fasterxml.jackson.core.StreamReadConstraints;
import com.fasterxml.jackson.core.StreamReadFeature;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.MapperFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.json.JsonMapper;
import io.astrasync.connector.api.ConnectionRequirement;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorOptionDescriptor;
import io.astrasync.connector.api.ConnectorOptionOwner;
import io.astrasync.connector.api.ConnectorOptionPrefixDescriptor;
import io.astrasync.connector.api.ConnectorOptionSensitivity;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.engine.jobspec.ConnectorSpec;
import io.astrasync.engine.jobspec.JobConfiguration;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.plan.ConnectorRegistry;
import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.time.format.DateTimeParseException;
import java.util.Base64;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;
import java.util.UUID;
import java.util.regex.Pattern;

/** Loads Scheduler-owned, epoch-scoped Connection envelopes before connector creation. */
public final class RuntimeCredentialLoader {
    public static final String CREDENTIAL_DIRECTORY_ENVIRONMENT = "ASTRASYNC_RUNTIME_CREDENTIALS";
    public static final String JOB_UID_ENVIRONMENT = "ASTRASYNC_EXECUTION_JOB_UID";
    public static final String EXECUTION_EPOCH_ENVIRONMENT = "ASTRASYNC_EXECUTION_EPOCH";
    public static final String COMPILER_REVISION_ENVIRONMENT = "ASTRASYNC_EXECUTION_COMPILER_REVISION";

    private static final int ENVELOPE_SCHEMA_VERSION = 1;
    private static final int MAX_OPTIONS = 256;
    private static final int MAX_VALUE_BYTES = 65_536;
    private static final int MAX_ENVELOPE_BYTES = 512 * 1024;
    private static final int MAX_BASE64_LENGTH = 90_000;
    private static final Pattern OPTION_KEY = Pattern.compile("[A-Za-z][A-Za-z0-9._-]{0,127}");
    private static final Pattern REVISION = Pattern.compile("sha256:[0-9a-f]{64}");
    private static final Pattern JDBC_URL = Pattern.compile("^jdbc:[A-Za-z0-9][A-Za-z0-9+.-]*:.+$");
    private static final Pattern SQL_IDENTIFIER = Pattern.compile("[A-Za-z_][A-Za-z0-9_$]*");
    private static final Pattern SQL_IDENTIFIER_LIST =
            Pattern.compile("[A-Za-z_][A-Za-z0-9_$]*(?:,[A-Za-z_][A-Za-z0-9_$]*)*");
    private static final Pattern TOPIC_PREFIX = Pattern.compile("[A-Za-z0-9._-]{1,249}");
    private static final ObjectMapper MAPPER = JsonMapper.builder(JsonFactory.builder()
                    .streamReadConstraints(StreamReadConstraints.builder()
                            .maxNestingDepth(8)
                            .maxNumberLength(32)
                            .maxStringLength(MAX_BASE64_LENGTH)
                            .build())
                    .enable(StreamReadFeature.STRICT_DUPLICATE_DETECTION)
                    .build())
            .enable(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES)
            .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
            .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS)
            .build();

    private RuntimeCredentialLoader() {}

    public static JobSpec load(JobSpec jobSpec, ConnectorRegistry registry, Map<String, String> environment) {
        JobSpec checkedJob = Objects.requireNonNull(jobSpec, "jobSpec must not be null");
        ConnectorRegistry checkedRegistry = Objects.requireNonNull(registry, "registry must not be null");
        Map<String, String> checkedEnvironment = Objects.requireNonNull(environment, "environment must not be null");

        String directoryValue = optional(checkedEnvironment, CREDENTIAL_DIRECTORY_ENVIRONMENT);
        String jobUid = optional(checkedEnvironment, JOB_UID_ENVIRONMENT);
        String epochValue = optional(checkedEnvironment, EXECUTION_EPOCH_ENVIRONMENT);
        String compilerRevision = optional(checkedEnvironment, COMPILER_REVISION_ENVIRONMENT);
        boolean managed = directoryValue != null || jobUid != null || epochValue != null || compilerRevision != null;
        if (!managed) {
            return checkedJob;
        }
        if (jobUid == null || epochValue == null) {
            throw invalid("trusted execution identity is incomplete");
        }
        requireUuid(jobUid, "trusted Job UID is invalid");
        long epoch = positiveLong(epochValue, "trusted execution epoch is invalid");
        if ((directoryValue == null) != (compilerRevision == null)) {
            throw invalid("credential mount and compiler revision must be configured together");
        }
        if (compilerRevision != null && !REVISION.matcher(compilerRevision).matches()) {
            throw invalid("trusted compiler revision is invalid");
        }

        Path directory = directoryValue == null
                ? null
                : Path.of(directoryValue).toAbsolutePath().normalize();
        ConnectorSpec source = mergeRole(
                checkedJob.spec().source(),
                ConnectorRole.SOURCE,
                checkedRegistry,
                directory,
                jobUid,
                epoch,
                compilerRevision);
        ConnectorSpec sink = mergeRole(
                checkedJob.spec().sink(),
                ConnectorRole.SINK,
                checkedRegistry,
                directory,
                jobUid,
                epoch,
                compilerRevision);
        JobConfiguration configuration = checkedJob.spec();
        return new JobSpec(
                checkedJob.apiVersion(),
                checkedJob.kind(),
                checkedJob.metadata(),
                new JobConfiguration(
                        source, configuration.transforms(), sink, configuration.delivery(), configuration.runtime()));
    }

    private static ConnectorSpec mergeRole(
            ConnectorSpec connector,
            ConnectorRole role,
            ConnectorRegistry registry,
            Path directory,
            String jobUid,
            long epoch,
            String compilerRevision) {
        ConnectorDescriptor descriptor = registry.findDescriptor(connector.connector())
                .orElseThrow(() -> invalid("runtime connector is not registered"));
        if (!descriptor.supportsRole(role)) {
            throw invalid("runtime connector role is unsupported");
        }
        Path envelopePath =
                directory == null ? null : directory.resolve(role.name().toLowerCase() + ".json");
        boolean envelopePresent = envelopePath != null && Files.isRegularFile(envelopePath);
        ConnectionRequirement requirement = descriptor.connectionRequirement(role);
        if (requirement == ConnectionRequirement.REQUIRED && !envelopePresent) {
            throw invalid("required runtime credential envelope is missing");
        }
        if (requirement == ConnectionRequirement.NONE && envelopePresent) {
            throw invalid("unexpected runtime credential envelope");
        }

        Map<String, String> effective = new TreeMap<>();
        for (ConnectorOptionDescriptor option : descriptor.options()) {
            if (option.roles().contains(role)
                    && option.defaultValue() != null
                    && (option.owner() == ConnectorOptionOwner.JOB || envelopePresent)) {
                effective.put(option.key(), option.defaultValue());
            }
        }
        Map<String, Integer> prefixCounts = new HashMap<>();
        int configuredOptions = connector.options().size();
        if (envelopePresent) {
            RuntimeEnvelope envelope = readEnvelope(envelopePath);
            validateEnvelopeIdentity(envelope, role, jobUid, epoch, compilerRevision, descriptor);
            configuredOptions += envelope.options().size();
            String previous = null;
            for (EnvelopeOption option : envelope.options()) {
                if (option == null
                        || option.key() == null
                        || option.value() == null
                        || option.source() == null
                        || !OPTION_KEY.matcher(option.key()).matches()
                        || previous != null && option.key().compareTo(previous) <= 0) {
                    throw invalid("runtime credential options are malformed or duplicated");
                }
                previous = option.key();
                OptionPolicy policy = policy(descriptor, option.key(), role);
                if (policy.owner() != ConnectorOptionOwner.CONNECTION) {
                    throw invalid("runtime credential option ownership is invalid");
                }
                OptionSource source = parseSource(option.source());
                OptionSource expectedSource = policy.sensitivity() == ConnectorOptionSensitivity.SECRET
                        ? OptionSource.PROVIDER
                        : OptionSource.CONNECTION_SETTING;
                if (source != expectedSource) {
                    throw invalid("runtime credential option source is invalid");
                }
                String value = decodeValue(option.value());
                if (!validValue(policy, value)) {
                    throw invalid("runtime credential option violates descriptor bounds");
                }
                countPrefix(policy, prefixCounts);
                if (effective.put(option.key(), value) != null && policy.defaultValue() == null) {
                    throw invalid("runtime credential option ownership overlaps");
                }
            }
            validateProviderSources(envelope);
        }
        if (configuredOptions > MAX_OPTIONS) {
            throw invalid("runtime connector option count exceeds the supported limit");
        }
        for (Map.Entry<String, String> option : connector.options().entrySet()) {
            if (option.getKey() == null
                    || option.getValue() == null
                    || !OPTION_KEY.matcher(option.getKey()).matches()
                    || option.getValue().getBytes(StandardCharsets.UTF_8).length > MAX_VALUE_BYTES) {
                throw invalid("runtime Job option is malformed");
            }
            OptionPolicy policy = policy(descriptor, option.getKey(), role);
            if (policy.owner() != ConnectorOptionOwner.JOB || !validValue(policy, option.getValue())) {
                throw invalid("runtime Job option violates descriptor ownership or bounds");
            }
            countPrefix(policy, prefixCounts);
            if (effective.put(option.getKey(), option.getValue()) != null && policy.defaultValue() == null) {
                throw invalid("runtime option ownership overlaps");
            }
        }
        for (ConnectorOptionDescriptor option : descriptor.options()) {
            if (option.roles().contains(role) && option.required() && !effective.containsKey(option.key())) {
                throw invalid("required runtime connector option is missing");
            }
        }
        return new ConnectorSpec(connector.connector(), effective);
    }

    private static RuntimeEnvelope readEnvelope(Path path) {
        byte[] document = null;
        try (InputStream input = Files.newInputStream(path)) {
            document = input.readNBytes(MAX_ENVELOPE_BYTES + 1);
            if (document.length > MAX_ENVELOPE_BYTES) {
                throw invalid("runtime credential envelope exceeds the supported limit");
            }
            RuntimeEnvelope result = MAPPER.readValue(document, RuntimeEnvelope.class);
            if (result == null || result.options() == null || result.options().size() > MAX_OPTIONS) {
                throw invalid("runtime credential envelope is malformed");
            }
            return result;
        } catch (RuntimeCredentialException exception) {
            throw exception;
        } catch (IOException | RuntimeException exception) {
            throw invalid("runtime credential envelope cannot be read or decoded");
        } finally {
            if (document != null) {
                java.util.Arrays.fill(document, (byte) 0);
            }
        }
    }

    private static void validateEnvelopeIdentity(
            RuntimeEnvelope envelope,
            ConnectorRole role,
            String jobUid,
            long epoch,
            String compilerRevision,
            ConnectorDescriptor descriptor) {
        if (envelope.schemaVersion() != ENVELOPE_SCHEMA_VERSION
                || !jobUid.equals(envelope.jobUid())
                || epoch != envelope.epoch()
                || !role.name().equals(envelope.role())
                || !validUuid(envelope.connectionUid())
                || envelope.generation() <= 0
                || !descriptor.descriptorRevision().equals(envelope.descriptorRevision())
                || !compilerRevision.equals(envelope.compilerRevision())
                || !descriptor.acceptsConnectionSchema(envelope.connectionSchemaRevision())) {
            throw invalid("runtime credential envelope identity or revision does not match this execution");
        }
        if (envelope.providerKind() == null
                || envelope.providerObjectUid() == null
                || envelope.providerObjectUid().isBlank()
                || envelope.providerObjectUid().length() > 256
                || envelope.providerVersionToken() == null
                || envelope.providerVersionToken().isBlank()
                || envelope.providerVersionToken().length() > 256) {
            throw invalid("runtime credential provider receipt is invalid");
        }
        if ("NONE".equals(envelope.providerKind())) {
            if (!envelope.connectionUid().equals(envelope.providerObjectUid())
                    || !("generation:" + envelope.generation()).equals(envelope.providerVersionToken())) {
                throw invalid("runtime credential provider receipt is inconsistent");
            }
        } else if (!"KUBERNETES_SECRET_V1".equals(envelope.providerKind())) {
            throw invalid("runtime credential provider kind is unsupported");
        }
    }

    private static void validateProviderSources(RuntimeEnvelope envelope) {
        boolean hasProviderValue = envelope.options().stream()
                .filter(Objects::nonNull)
                .anyMatch(option -> "PROVIDER".equals(option.source()));
        if (hasProviderValue != "KUBERNETES_SECRET_V1".equals(envelope.providerKind())) {
            throw invalid("runtime credential provider receipt does not match option sources");
        }
    }

    private static OptionPolicy policy(ConnectorDescriptor descriptor, String key, ConnectorRole role) {
        for (ConnectorOptionDescriptor option : descriptor.options()) {
            if (option.key().equals(key)) {
                if (!option.roles().contains(role)) {
                    throw invalid("runtime connector option role is invalid");
                }
                return OptionPolicy.exact(option);
            }
        }
        for (ConnectorOptionPrefixDescriptor prefix : descriptor.optionPrefixes()) {
            if (key.startsWith(prefix.prefix())
                    && key.substring(prefix.prefix().length()).matches(prefix.keyPattern())) {
                if (!prefix.roles().contains(role)) {
                    throw invalid("runtime connector option prefix role is invalid");
                }
                return OptionPolicy.prefix(prefix);
            }
        }
        throw invalid("runtime connector option is not declared by the active descriptor");
    }

    private static boolean validValue(OptionPolicy policy, String value) {
        if (value.getBytes(StandardCharsets.UTF_8).length > MAX_VALUE_BYTES) {
            return false;
        }
        if (policy.prefix() != null) {
            return value.length() <= policy.prefix().maxValueLength();
        }
        ConnectorOptionDescriptor option = policy.option();
        int length = value.codePointCount(0, value.length());
        if (option.minLength() != null && length < option.minLength()
                || option.maxLength() != null && length > option.maxLength()) {
            return false;
        }
        return switch (option.valueType()) {
            case STRING -> validPattern(option.patternKey(), value);
            case INTEGER -> validInteger(option, value);
            case BOOLEAN -> value.equals("true") || value.equals("false");
            case DURATION -> validDuration(option, value);
            case ENUM -> option.enumValues().contains(value);
        };
    }

    private static boolean validInteger(ConnectorOptionDescriptor option, String value) {
        try {
            long parsed = Long.parseLong(value);
            return (option.minimum() == null || parsed >= option.minimum())
                    && (option.maximum() == null || parsed <= option.maximum());
        } catch (NumberFormatException exception) {
            return false;
        }
    }

    private static boolean validDuration(ConnectorOptionDescriptor option, String value) {
        try {
            long millis = value.matches("-?[0-9]+")
                    ? Long.parseLong(value)
                    : Duration.parse(value).toMillis();
            return (option.minimum() == null || millis >= option.minimum())
                    && (option.maximum() == null || millis <= option.maximum());
        } catch (ArithmeticException | DateTimeParseException | NumberFormatException exception) {
            return false;
        }
    }

    private static boolean validPattern(String patternKey, String value) {
        if (patternKey == null || patternKey.isEmpty()) {
            return true;
        }
        return switch (patternKey) {
            case "jdbc.url" -> JDBC_URL.matcher(value).matches();
            case "sql.column", "sql.table", "postgres.identifier" -> SQL_IDENTIFIER
                    .matcher(value)
                    .matches();
            case "sql.column-list" -> SQL_IDENTIFIER_LIST.matcher(value).matches();
            case "debezium.topic-prefix" -> TOPIC_PREFIX.matcher(value).matches();
            case "local.path" -> !value.contains("\u0000") && !value.contains("\r") && !value.contains("\n");
            default -> true;
        };
    }

    private static String decodeValue(String encoded) {
        if (encoded.length() > MAX_BASE64_LENGTH) {
            throw invalid("runtime credential option value exceeds the supported limit");
        }
        byte[] decoded;
        try {
            decoded = Base64.getDecoder().decode(encoded);
        } catch (IllegalArgumentException exception) {
            throw invalid("runtime credential option value is not canonical Base64");
        }
        try {
            if (decoded.length > MAX_VALUE_BYTES) {
                throw invalid("runtime credential option value exceeds the supported limit");
            }
            return StandardCharsets.UTF_8
                    .newDecoder()
                    .onMalformedInput(CodingErrorAction.REPORT)
                    .onUnmappableCharacter(CodingErrorAction.REPORT)
                    .decode(ByteBuffer.wrap(decoded))
                    .toString();
        } catch (CharacterCodingException exception) {
            throw invalid("runtime credential option value is not valid UTF-8");
        } finally {
            java.util.Arrays.fill(decoded, (byte) 0);
        }
    }

    private static void countPrefix(OptionPolicy policy, Map<String, Integer> counts) {
        if (policy.prefix() == null) {
            return;
        }
        int count = counts.merge(policy.prefix().prefix(), 1, Integer::sum);
        if (count > policy.prefix().maxEntries()) {
            throw invalid("runtime connector option prefix exceeds its entry limit");
        }
    }

    private static OptionSource parseSource(String value) {
        try {
            return OptionSource.valueOf(value);
        } catch (IllegalArgumentException exception) {
            throw invalid("runtime credential option source is unsupported");
        }
    }

    private static String optional(Map<String, String> environment, String name) {
        String value = environment.get(name);
        return value == null || value.isBlank() ? null : value;
    }

    private static long positiveLong(String value, String message) {
        try {
            long parsed = Long.parseLong(value);
            if (parsed <= 0) {
                throw invalid(message);
            }
            return parsed;
        } catch (NumberFormatException exception) {
            throw invalid(message);
        }
    }

    private static void requireUuid(String value, String message) {
        if (!validUuid(value)) {
            throw invalid(message);
        }
    }

    private static boolean validUuid(String value) {
        if (value == null) {
            return false;
        }
        try {
            return UUID.fromString(value).toString().equals(value.toLowerCase());
        } catch (IllegalArgumentException exception) {
            return false;
        }
    }

    private static RuntimeCredentialException invalid(String reason) {
        return new RuntimeCredentialException(reason);
    }

    private enum OptionSource {
        CONNECTION_SETTING,
        PROVIDER
    }

    private record RuntimeEnvelope(
            int schemaVersion,
            String jobUid,
            long epoch,
            String role,
            String connectionUid,
            long generation,
            String descriptorRevision,
            String compilerRevision,
            String connectionSchemaRevision,
            String providerKind,
            String providerObjectUid,
            String providerVersionToken,
            List<EnvelopeOption> options) {}

    private record EnvelopeOption(String key, String source, String value) {}

    private record OptionPolicy(
            ConnectorOptionOwner owner,
            ConnectorOptionSensitivity sensitivity,
            String defaultValue,
            ConnectorOptionDescriptor option,
            ConnectorOptionPrefixDescriptor prefix) {
        private static OptionPolicy exact(ConnectorOptionDescriptor option) {
            return new OptionPolicy(option.owner(), option.sensitivity(), option.defaultValue(), option, null);
        }

        private static OptionPolicy prefix(ConnectorOptionPrefixDescriptor prefix) {
            return new OptionPolicy(prefix.owner(), prefix.sensitivity(), null, null, prefix);
        }
    }

    public static final class RuntimeCredentialException extends IllegalArgumentException {
        private static final long serialVersionUID = 1L;

        private RuntimeCredentialException(String reason) {
            super("runtime credential validation failed: " + reason);
        }
    }
}
