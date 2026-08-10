package io.astrasync.connector.api;

import java.util.Collections;
import java.util.EnumSet;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.regex.Pattern;

/** Immutable, side-effect-free schema for one exact connector option key. */
public record ConnectorOptionDescriptor(
        String key,
        Set<ConnectorRole> roles,
        ConnectorOptionOwner owner,
        ConnectorOptionType valueType,
        boolean required,
        String defaultValue,
        List<String> enumValues,
        Long minimum,
        Long maximum,
        Integer minLength,
        Integer maxLength,
        String patternKey,
        ConnectorOptionSensitivity sensitivity,
        boolean advanced,
        String labelKey,
        String helpKey) {
    private static final Pattern KEY_PATTERN = Pattern.compile("[A-Za-z][A-Za-z0-9._-]{0,127}");
    private static final Pattern PRESENTATION_KEY_PATTERN = Pattern.compile("[a-z0-9][a-z0-9._-]{0,255}");

    public ConnectorOptionDescriptor {
        Objects.requireNonNull(key, "key must not be null");
        if (!KEY_PATTERN.matcher(key).matches()) {
            throw new IllegalArgumentException("key must be a canonical connector option key: " + key);
        }
        Objects.requireNonNull(roles, "roles must not be null");
        EnumSet<ConnectorRole> roleCopy = EnumSet.noneOf(ConnectorRole.class);
        for (ConnectorRole role : roles) {
            roleCopy.add(Objects.requireNonNull(role, "roles must not contain null"));
        }
        if (roleCopy.isEmpty()) {
            throw new IllegalArgumentException("roles must not be empty");
        }
        roles = Collections.unmodifiableSet(roleCopy);
        owner = Objects.requireNonNull(owner, "owner must not be null");
        valueType = Objects.requireNonNull(valueType, "valueType must not be null");
        sensitivity = Objects.requireNonNull(sensitivity, "sensitivity must not be null");
        enumValues = List.copyOf(Objects.requireNonNull(enumValues, "enumValues must not be null"));
        if (enumValues.size() > 128) {
            throw new IllegalArgumentException("enumValues must contain at most 128 values");
        }
        validateBounds(minimum, maximum, "numeric");
        validateBounds(minLength, maxLength, "length");
        if ((minimum != null || maximum != null)
                && valueType != ConnectorOptionType.INTEGER
                && valueType != ConnectorOptionType.DURATION) {
            throw new IllegalArgumentException("numeric bounds require INTEGER or DURATION value type");
        }
        if ((minLength != null || maxLength != null)
                && valueType != ConnectorOptionType.STRING
                && valueType != ConnectorOptionType.ENUM) {
            throw new IllegalArgumentException("length bounds require STRING or ENUM value type");
        }
        if (patternKey != null && valueType != ConnectorOptionType.STRING) {
            throw new IllegalArgumentException("patternKey requires the STRING value type");
        }
        if (valueType == ConnectorOptionType.ENUM && enumValues.isEmpty()) {
            throw new IllegalArgumentException("ENUM option must declare enumValues");
        }
        if (valueType != ConnectorOptionType.ENUM && !enumValues.isEmpty()) {
            throw new IllegalArgumentException("enumValues require the ENUM value type");
        }
        if (enumValues.stream().anyMatch(value -> value == null || value.isBlank())) {
            throw new IllegalArgumentException("enumValues must not contain blank values");
        }
        if (enumValues.stream().distinct().count() != enumValues.size()) {
            throw new IllegalArgumentException("enumValues must be unique");
        }
        if (sensitivity != ConnectorOptionSensitivity.PUBLIC) {
            if (owner != ConnectorOptionOwner.CONNECTION) {
                throw new IllegalArgumentException("sensitive option must be owned by CONNECTION");
            }
            if (defaultValue != null) {
                throw new IllegalArgumentException("sensitive option must not declare a default value");
            }
        }
        validateDefault(valueType, defaultValue, enumValues, minimum, maximum, minLength, maxLength);
        patternKey = checkedPresentationKey(patternKey, "patternKey", true);
        labelKey = checkedPresentationKey(labelKey, "labelKey", false);
        helpKey = checkedPresentationKey(helpKey, "helpKey", false);
    }

    public static Builder builder(
            String key, ConnectorOptionType valueType, ConnectorOptionOwner owner, ConnectorRole... roles) {
        return new Builder(key, valueType, owner, roles);
    }

    private static String checkedPresentationKey(String value, String label, boolean nullable) {
        if (value == null && nullable) {
            return null;
        }
        Objects.requireNonNull(value, label + " must not be null");
        if (!PRESENTATION_KEY_PATTERN.matcher(value).matches()) {
            throw new IllegalArgumentException(label + " must be a canonical presentation key");
        }
        return value;
    }

    private static void validateBounds(Number minimum, Number maximum, String label) {
        if (minimum != null && maximum != null && minimum.longValue() > maximum.longValue()) {
            throw new IllegalArgumentException(label + " minimum must not exceed maximum");
        }
        if ((minimum instanceof Integer && minimum.intValue() < 0)
                || (maximum instanceof Integer && maximum.intValue() < 0)) {
            throw new IllegalArgumentException(label + " bounds must not be negative");
        }
    }

    private static void validateDefault(
            ConnectorOptionType type,
            String value,
            List<String> enumValues,
            Long minimum,
            Long maximum,
            Integer minLength,
            Integer maxLength) {
        if (value == null) {
            return;
        }
        if (type == ConnectorOptionType.BOOLEAN && !value.equals("true") && !value.equals("false")) {
            throw new IllegalArgumentException("BOOLEAN defaultValue must be true or false");
        }
        if (type == ConnectorOptionType.ENUM && !enumValues.contains(value)) {
            throw new IllegalArgumentException("ENUM defaultValue must be one of enumValues");
        }
        if (type == ConnectorOptionType.INTEGER || type == ConnectorOptionType.DURATION) {
            long parsed;
            try {
                parsed = Long.parseLong(value);
            } catch (NumberFormatException exception) {
                throw new IllegalArgumentException(type + " defaultValue must be an integer", exception);
            }
            if ((minimum != null && parsed < minimum) || (maximum != null && parsed > maximum)) {
                throw new IllegalArgumentException(type + " defaultValue must be within declared bounds");
            }
        }
        if ((minLength != null && value.length() < minLength) || (maxLength != null && value.length() > maxLength)) {
            throw new IllegalArgumentException("defaultValue must be within declared length bounds");
        }
    }

    public static final class Builder {
        private final String key;
        private final ConnectorOptionType valueType;
        private final ConnectorOptionOwner owner;
        private final Set<ConnectorRole> roles;
        private boolean required;
        private String defaultValue;
        private List<String> enumValues = List.of();
        private Long minimum;
        private Long maximum;
        private Integer minLength;
        private Integer maxLength;
        private String patternKey;
        private ConnectorOptionSensitivity sensitivity = ConnectorOptionSensitivity.PUBLIC;
        private boolean advanced;
        private String labelKey;
        private String helpKey;

        private Builder(String key, ConnectorOptionType valueType, ConnectorOptionOwner owner, ConnectorRole... roles) {
            this.key = key;
            this.valueType = valueType;
            this.owner = owner;
            Objects.requireNonNull(roles, "roles must not be null");
            this.roles = roles.length == 0 ? Set.of() : Set.of(roles);
            String normalized = key == null ? "option" : key.toLowerCase().replace('_', '-');
            this.labelKey = "connector.option." + normalized + ".label";
            this.helpKey = "connector.option." + normalized + ".help";
        }

        public Builder required() {
            required = true;
            return this;
        }

        public Builder defaultValue(String value) {
            defaultValue = value;
            return this;
        }

        public Builder enumValues(String... values) {
            enumValues = List.of(values);
            return this;
        }

        public Builder numericBounds(long lower, long upper) {
            minimum = lower;
            maximum = upper;
            return this;
        }

        public Builder lengthBounds(int lower, int upper) {
            minLength = lower;
            maxLength = upper;
            return this;
        }

        public Builder patternKey(String value) {
            patternKey = value;
            return this;
        }

        public Builder sensitivity(ConnectorOptionSensitivity value) {
            sensitivity = value;
            return this;
        }

        public Builder advanced() {
            advanced = true;
            return this;
        }

        public Builder presentationKeys(String label, String help) {
            labelKey = label;
            helpKey = help;
            return this;
        }

        public ConnectorOptionDescriptor build() {
            return new ConnectorOptionDescriptor(
                    key,
                    roles,
                    owner,
                    valueType,
                    required,
                    defaultValue,
                    enumValues,
                    minimum,
                    maximum,
                    minLength,
                    maxLength,
                    patternKey,
                    sensitivity,
                    advanced,
                    labelKey,
                    helpKey);
        }
    }
}
