package io.astrasync.control.compiler;

import io.astrasync.compiler.v1.CompilerValidationIssueCode;
import io.astrasync.compiler.v1.EffectiveConnectorConfig;
import io.astrasync.connector.api.ConnectionRequirement;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorOptionDescriptor;
import io.astrasync.connector.api.ConnectorOptionOwner;
import io.astrasync.connector.api.ConnectorOptionPrefixDescriptor;
import io.astrasync.connector.api.ConnectorOptionSensitivity;
import io.astrasync.connector.api.ConnectorRole;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.format.DateTimeParseException;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;

final class DescriptorOptionValidator {
    static final int MAX_OPTIONS = 256;
    static final int MAX_OPTION_VALUE_BYTES = 65_536;
    private static final String CONFIGURED_SECRET_MARKER = "configured";
    private static final Pattern OPTION_KEY = Pattern.compile("[A-Za-z][A-Za-z0-9._-]{0,127}");
    private static final Pattern JDBC_URL = Pattern.compile("^jdbc:[A-Za-z0-9][A-Za-z0-9+.-]*:.+$");
    private static final Pattern SQL_IDENTIFIER = Pattern.compile("[A-Za-z_][A-Za-z0-9_$]*");
    private static final Pattern SQL_IDENTIFIER_LIST =
            Pattern.compile("[A-Za-z_][A-Za-z0-9_$]*(?:,[A-Za-z_][A-Za-z0-9_$]*)*");
    private static final Pattern TOPIC_PREFIX = Pattern.compile("[A-Za-z0-9._-]{1,249}");
    private static final CompilerValidationIssueCode ISSUE_CONNECTION_REQUIRED =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_CONNECTION_REF_REQUIRED;
    private static final CompilerValidationIssueCode ISSUE_CONNECTION_SCHEMA =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_CONNECTION_SCHEMA_INCOMPATIBLE;
    private static final CompilerValidationIssueCode ISSUE_OPTION_INVALID =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_OPTION_INVALID;
    private static final CompilerValidationIssueCode ISSUE_OPTION_OWNERSHIP =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_OPTION_OWNERSHIP_INVALID;
    private static final CompilerValidationIssueCode ISSUE_OPTION_REQUIRED =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_OPTION_REQUIRED;
    private static final CompilerValidationIssueCode ISSUE_OPTION_UNKNOWN =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_OPTION_UNKNOWN;
    private static final CompilerValidationIssueCode ISSUE_ROLE_UNSUPPORTED =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_ROLE_UNSUPPORTED;
    private static final CompilerValidationIssueCode ISSUE_SECRET_FIELD =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_SECRET_FIELD_INVALID;
    private static final CompilerValidationIssueCode ISSUE_STRUCTURE =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_STRUCTURE_INVALID;
    private static final CompilerValidationIssueCode ISSUE_REVISION_CHANGED =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_VALIDATION_REVISION_CHANGED;

    private DescriptorOptionValidator() {}

    static Map<String, String> validate(
            EffectiveConnectorConfig config,
            ConnectorDescriptor descriptor,
            ConnectorRole role,
            String path,
            ValidationIssues issues) {
        if (!descriptor.supportsRole(role)) {
            issues.add(ISSUE_ROLE_UNSUPPORTED, path + ".connector", "connector does not support this role");
            return Map.of();
        }
        if (!config.getDescriptorRevision().isEmpty()
                && !config.getDescriptorRevision().equals(descriptor.descriptorRevision())) {
            issues.add(
                    ISSUE_REVISION_CHANGED,
                    path + ".connector",
                    "connector descriptor revision changed; refresh and validate again");
        }
        if (config.getConnectionConfigured()) {
            if (config.getConnectionSchemaRevision().isEmpty()
                    || !descriptor.acceptsConnectionSchema(config.getConnectionSchemaRevision())) {
                issues.add(
                        ISSUE_CONNECTION_SCHEMA,
                        path + ".connection_ref",
                        "Connection schema is incompatible with the active connector");
            }
        } else if (descriptor.connectionRequirement(role) == ConnectionRequirement.REQUIRED) {
            issues.add(ISSUE_CONNECTION_REQUIRED, path + ".connection_ref", "an active Connection is required");
        }
        if (descriptor.connectionRequirement(role) == ConnectionRequirement.NONE && config.getConnectionConfigured()) {
            issues.add(
                    ISSUE_OPTION_OWNERSHIP,
                    path + ".connection_ref",
                    "this connector role does not accept a Connection");
        }

        int optionCount = config.getJobOptionsCount()
                + config.getConnectionSettingsCount()
                + config.getConfiguredSecretFieldsCount();
        if (optionCount > MAX_OPTIONS) {
            issues.add(ISSUE_STRUCTURE, path + ".options", "connector option count exceeds the supported limit");
            return Map.of();
        }

        Map<String, ConnectorOptionDescriptor> exact = new HashMap<>();
        descriptor.options().forEach(option -> exact.put(option.key(), option));
        Set<String> seen = new HashSet<>();
        Map<String, String> effective = new LinkedHashMap<>();

        config.getJobOptionsMap().entrySet().stream()
                .sorted(Map.Entry.comparingByKey())
                .forEach(entry -> validateValue(
                        entry.getKey(),
                        entry.getValue(),
                        ConnectorOptionOwner.JOB,
                        false,
                        role,
                        path + ".options",
                        descriptor,
                        exact,
                        seen,
                        effective,
                        issues));
        config.getConnectionSettingsMap().entrySet().stream()
                .sorted(Map.Entry.comparingByKey())
                .forEach(entry -> validateValue(
                        entry.getKey(),
                        entry.getValue(),
                        ConnectorOptionOwner.CONNECTION,
                        false,
                        role,
                        path + ".connection_settings",
                        descriptor,
                        exact,
                        seen,
                        effective,
                        issues));
        config.getConfiguredSecretFieldsList().stream()
                .sorted()
                .forEach(key -> validateSecret(key, role, path, exact, seen, effective, issues));

        for (ConnectorOptionDescriptor option : descriptor.options()) {
            if (!option.roles().contains(role)) {
                continue;
            }
            if (option.owner() == ConnectorOptionOwner.CONNECTION && !config.getConnectionConfigured()) {
                continue;
            }
            if (!seen.contains(option.key()) && option.defaultValue() != null) {
                effective.put(option.key(), option.defaultValue());
            }
            if (option.required() && option.defaultValue() == null && !seen.contains(option.key())) {
                String field = option.owner() == ConnectorOptionOwner.JOB ? "options" : "connection_ref";
                issues.add(
                        ISSUE_OPTION_REQUIRED,
                        path + "." + field,
                        "a required connector option is not configured",
                        option.helpKey());
            }
        }
        return Map.copyOf(effective);
    }

    private static void validateValue(
            String key,
            String value,
            ConnectorOptionOwner expectedOwner,
            boolean secret,
            ConnectorRole role,
            String path,
            ConnectorDescriptor descriptor,
            Map<String, ConnectorOptionDescriptor> exact,
            Set<String> seen,
            Map<String, String> effective,
            ValidationIssues issues) {
        if (!validKey(key) || value == null || value.getBytes(StandardCharsets.UTF_8).length > MAX_OPTION_VALUE_BYTES) {
            issues.add(ISSUE_OPTION_INVALID, path, "connector option is malformed or exceeds the supported limit");
            return;
        }
        if (!seen.add(key)) {
            issues.add(ISSUE_OPTION_INVALID, path, "connector option is configured more than once");
            return;
        }
        ConnectorOptionDescriptor option = exact.get(key);
        ConnectorOptionPrefixDescriptor prefix = null;
        if (option == null) {
            prefix = matchingPrefix(descriptor, key);
            if (prefix == null) {
                issues.add(ISSUE_OPTION_UNKNOWN, path, "connector option is not declared by the active descriptor");
                return;
            }
        }
        ConnectorOptionOwner owner = option == null ? prefix.owner() : option.owner();
        ConnectorOptionSensitivity sensitivity = option == null ? prefix.sensitivity() : option.sensitivity();
        Set<ConnectorRole> roles = option == null ? prefix.roles() : option.roles();
        if (owner != expectedOwner
                || !roles.contains(role)
                || secret
                || sensitivity == ConnectorOptionSensitivity.SECRET) {
            issues.add(ISSUE_OPTION_OWNERSHIP, path, "connector option is not allowed on this resource or role");
            return;
        }
        if (option == null) {
            if (value.length() > prefix.maxValueLength()) {
                issues.add(ISSUE_OPTION_INVALID, path, "connector option does not satisfy descriptor bounds");
                return;
            }
        } else if (!validValue(option, value)) {
            issues.add(
                    ISSUE_OPTION_INVALID,
                    path,
                    "connector option does not satisfy descriptor type or bounds",
                    option.helpKey());
            return;
        }
        effective.put(key, value);
    }

    private static void validateSecret(
            String key,
            ConnectorRole role,
            String path,
            Map<String, ConnectorOptionDescriptor> exact,
            Set<String> seen,
            Map<String, String> effective,
            ValidationIssues issues) {
        if (!validKey(key) || !seen.add(key)) {
            issues.add(ISSUE_SECRET_FIELD, path + ".connection_ref", "configured secret field metadata is invalid");
            return;
        }
        ConnectorOptionDescriptor option = exact.get(key);
        if (option == null
                || option.owner() != ConnectorOptionOwner.CONNECTION
                || option.sensitivity() != ConnectorOptionSensitivity.SECRET
                || !option.roles().contains(role)) {
            issues.add(
                    ISSUE_SECRET_FIELD,
                    path + ".connection_ref",
                    "configured secret field is not declared for this connector role");
            return;
        }
        effective.put(key, CONFIGURED_SECRET_MARKER);
    }

    private static ConnectorOptionPrefixDescriptor matchingPrefix(ConnectorDescriptor descriptor, String key) {
        for (ConnectorOptionPrefixDescriptor prefix : descriptor.optionPrefixes()) {
            if (key.startsWith(prefix.prefix())
                    && key.substring(prefix.prefix().length()).matches(prefix.keyPattern())) {
                return prefix;
            }
        }
        return null;
    }

    private static boolean validKey(String key) {
        return key != null && OPTION_KEY.matcher(key).matches();
    }

    private static boolean validValue(ConnectorOptionDescriptor option, String value) {
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
            long millis;
            if (value.matches("-?[0-9]+")) {
                millis = Long.parseLong(value);
            } else {
                millis = Duration.parse(value).toMillis();
            }
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
}
