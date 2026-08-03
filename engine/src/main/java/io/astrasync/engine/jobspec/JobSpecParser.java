package io.astrasync.engine.jobspec;

import com.fasterxml.jackson.core.JsonParser;
import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.core.JsonToken;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;
import java.io.IOException;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.HashSet;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;

public final class JobSpecParser {
    private static final YAMLFactory YAML_FACTORY = new YAMLFactory();
    private static final ObjectMapper OBJECT_MAPPER = new ObjectMapper(YAML_FACTORY);
    private static final Pattern SIMPLE_PATH_SEGMENT = Pattern.compile("[A-Za-z_][A-Za-z0-9_-]*");
    private static final Pattern JOB_NAME = Pattern.compile("[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?");
    private static final Pattern CONNECTOR_NAME = Pattern.compile("(?=.{1,128}$)[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?");

    private static final Set<String> ROOT_FIELDS = Set.of("apiVersion", "kind", "metadata", "spec");
    private static final Set<String> METADATA_FIELDS = Set.of("name");
    private static final Set<String> SPEC_FIELDS = Set.of("source", "transforms", "sink", "delivery", "runtime");
    private static final Set<String> CONNECTOR_FIELDS = Set.of("connector", "options");
    private static final Set<String> TRANSFORM_FIELDS = Set.of("type", "options");
    private static final Set<String> DELIVERY_FIELDS = Set.of("guarantee");
    private static final Set<String> RUNTIME_FIELDS = Set.of("maxBatchRecords");

    public JobSpec parse(String document) {
        if (document == null) {
            throw failure("$", "document must not be null");
        }
        if (document.isBlank()) {
            throw failure("$", "document must not be blank");
        }

        try {
            rejectDuplicateFields(document);
            try (JsonParser parser = YAML_FACTORY.createParser(document)) {
                JsonNode root = OBJECT_MAPPER.readTree(parser);
                if (root == null) {
                    throw failure("$", "document is empty");
                }
                JsonNode secondDocument = OBJECT_MAPPER.readTree(parser);
                if (secondDocument != null) {
                    throw failure("$", "multiple documents are not supported");
                }
                return parseRoot(root);
            }
        } catch (JobSpecParseException exception) {
            throw exception;
        } catch (JsonProcessingException exception) {
            throw failure("$", "invalid YAML or JSON: " + exception.getOriginalMessage(), exception);
        } catch (IOException exception) {
            throw failure("$", "failed to read JobSpec", exception);
        }
    }

    private static JobSpec parseRoot(JsonNode root) {
        requireObject(root, "$");
        rejectUnknownFields(root, ROOT_FIELDS, "$");

        String apiVersion = requiredText(root, "apiVersion", "$");
        if (!JobSpec.API_VERSION.equals(apiVersion)) {
            throw failure("$.apiVersion", "unsupported apiVersion: " + apiVersion);
        }
        String kind = requiredText(root, "kind", "$");
        if (!JobSpec.KIND.equals(kind)) {
            throw failure("$.kind", "unsupported kind: " + kind);
        }

        JsonNode metadataNode = requiredObject(root, "metadata", "$");
        rejectUnknownFields(metadataNode, METADATA_FIELDS, "$.metadata");
        String name = requiredText(metadataNode, "name", "$.metadata");
        if (!JOB_NAME.matcher(name).matches()) {
            throw failure("$.metadata.name", "must be a lowercase DNS label of at most 63 characters");
        }

        JsonNode specNode = requiredObject(root, "spec", "$");
        rejectUnknownFields(specNode, SPEC_FIELDS, "$.spec");
        ConnectorSpec source = parseConnector(requiredObject(specNode, "source", "$.spec"), "$.spec.source");
        List<TransformSpec> transforms = parseTransforms(specNode.get("transforms"), "$.spec.transforms");
        ConnectorSpec sink = parseConnector(requiredObject(specNode, "sink", "$.spec"), "$.spec.sink");
        DeliverySpec delivery = parseDelivery(requiredObject(specNode, "delivery", "$.spec"));
        RuntimeSpec runtime = parseRuntime(specNode.get("runtime"));

        return new JobSpec(
                apiVersion,
                kind,
                new JobMetadata(name),
                new JobConfiguration(source, transforms, sink, delivery, runtime));
    }

    private static ConnectorSpec parseConnector(JsonNode node, String path) {
        rejectUnknownFields(node, CONNECTOR_FIELDS, path);
        String connector = requiredText(node, "connector", path);
        if (!CONNECTOR_NAME.matcher(connector).matches()) {
            throw failure(child(path, "connector"), "invalid canonical connector name");
        }
        return new ConnectorSpec(connector, parseOptions(node.get("options"), child(path, "options")));
    }

    private static List<TransformSpec> parseTransforms(JsonNode node, String path) {
        if (node == null) {
            return List.of();
        }
        if (!node.isArray()) {
            throw failure(path, "must be an array");
        }
        List<TransformSpec> transforms = new ArrayList<>();
        for (int index = 0; index < node.size(); index++) {
            String itemPath = path + "[" + index + "]";
            JsonNode transformNode = node.get(index);
            requireObject(transformNode, itemPath);
            rejectUnknownFields(transformNode, TRANSFORM_FIELDS, itemPath);
            String type = requiredText(transformNode, "type", itemPath);
            if (type.isBlank()) {
                throw failure(child(itemPath, "type"), "must not be blank");
            }
            transforms.add(
                    new TransformSpec(type, parseOptions(transformNode.get("options"), child(itemPath, "options"))));
        }
        return List.copyOf(transforms);
    }

    private static DeliverySpec parseDelivery(JsonNode node) {
        String path = "$.spec.delivery";
        rejectUnknownFields(node, DELIVERY_FIELDS, path);
        String value = requiredText(node, "guarantee", path);
        try {
            return new DeliverySpec(DeliveryGuarantee.fromExternalName(value));
        } catch (IllegalArgumentException exception) {
            throw failure(child(path, "guarantee"), exception.getMessage());
        }
    }

    private static RuntimeSpec parseRuntime(JsonNode node) {
        if (node == null) {
            return RuntimeSpec.defaults();
        }
        String path = "$.spec.runtime";
        requireObject(node, path);
        rejectUnknownFields(node, RUNTIME_FIELDS, path);
        JsonNode maxBatchRecordsNode = node.get("maxBatchRecords");
        if (maxBatchRecordsNode == null) {
            return RuntimeSpec.defaults();
        }
        if (!maxBatchRecordsNode.isIntegralNumber() || !maxBatchRecordsNode.canConvertToInt()) {
            throw failure(child(path, "maxBatchRecords"), "must be a 32-bit integer");
        }
        int maxBatchRecords = maxBatchRecordsNode.intValue();
        if (maxBatchRecords <= 0) {
            throw failure(child(path, "maxBatchRecords"), "must be positive");
        }
        return new RuntimeSpec(maxBatchRecords);
    }

    private static Map<String, String> parseOptions(JsonNode node, String path) {
        if (node == null) {
            return Map.of();
        }
        requireObject(node, path);
        Map<String, String> options = new LinkedHashMap<>();
        Iterator<Map.Entry<String, JsonNode>> fields = node.fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            String fieldPath = child(path, field.getKey());
            if (field.getKey().isBlank()) {
                throw failure(fieldPath, "option key must not be blank");
            }
            if (!field.getValue().isTextual()) {
                throw failure(fieldPath, "option value must be a string");
            }
            options.put(field.getKey(), field.getValue().textValue());
        }
        return options;
    }

    private static JsonNode requiredObject(JsonNode parent, String field, String parentPath) {
        JsonNode node = parent.get(field);
        String path = child(parentPath, field);
        if (node == null) {
            throw failure(path, "is required");
        }
        return requireObject(node, path);
    }

    private static JsonNode requireObject(JsonNode node, String path) {
        if (node == null || !node.isObject()) {
            throw failure(path, "must be an object");
        }
        return node;
    }

    private static String requiredText(JsonNode parent, String field, String parentPath) {
        String path = child(parentPath, field);
        JsonNode node = parent.get(field);
        if (node == null) {
            throw failure(path, "is required");
        }
        if (!node.isTextual()) {
            throw failure(path, "must be a string");
        }
        return node.textValue();
    }

    private static void rejectUnknownFields(JsonNode node, Set<String> allowedFields, String path) {
        Iterator<String> names = node.fieldNames();
        while (names.hasNext()) {
            String name = names.next();
            if (!allowedFields.contains(name)) {
                throw failure(child(path, name), "unknown field");
            }
        }
    }

    private static void rejectDuplicateFields(String document) throws IOException {
        Deque<ParsingContext> contexts = new ArrayDeque<>();
        try (JsonParser parser = YAML_FACTORY.createParser(document)) {
            JsonToken token;
            while ((token = parser.nextToken()) != null) {
                if (token == JsonToken.FIELD_NAME) {
                    if (contexts.isEmpty() || !(contexts.peek() instanceof ObjectContext objectContext)) {
                        throw failure("$", "field outside an object");
                    }
                    String name = parser.currentName();
                    if (!objectContext.seenFields.add(name)) {
                        throw failure(child(objectContext.path, name), "duplicate field");
                    }
                    objectContext.pendingField = name;
                } else if (token == JsonToken.START_OBJECT) {
                    String path = nextValuePath(contexts);
                    consumeParentValue(contexts);
                    contexts.push(new ObjectContext(path));
                } else if (token == JsonToken.START_ARRAY) {
                    String path = nextValuePath(contexts);
                    consumeParentValue(contexts);
                    contexts.push(new ArrayContext(path));
                } else if (token == JsonToken.END_OBJECT || token == JsonToken.END_ARRAY) {
                    if (!contexts.isEmpty()) {
                        contexts.pop();
                    }
                } else if (token.isScalarValue()) {
                    consumeParentValue(contexts);
                }
            }
        }
    }

    private static String nextValuePath(Deque<ParsingContext> contexts) {
        if (contexts.isEmpty()) {
            return "$";
        }
        ParsingContext context = contexts.peek();
        if (context instanceof ObjectContext objectContext) {
            return child(objectContext.path, objectContext.pendingField);
        }
        ArrayContext arrayContext = (ArrayContext) context;
        return arrayContext.path + "[" + arrayContext.nextIndex + "]";
    }

    private static void consumeParentValue(Deque<ParsingContext> contexts) {
        if (contexts.isEmpty()) {
            return;
        }
        ParsingContext context = contexts.peek();
        if (context instanceof ObjectContext objectContext) {
            objectContext.pendingField = null;
        } else {
            ((ArrayContext) context).nextIndex++;
        }
    }

    private static String child(String parent, String name) {
        if (name == null) {
            return parent;
        }
        if (SIMPLE_PATH_SEGMENT.matcher(name).matches()) {
            return parent + "." + name;
        }
        return parent + "['" + name.replace("'", "\\'") + "']";
    }

    private static JobSpecParseException failure(String path, String message) {
        return new JobSpecParseException(path, message);
    }

    private static JobSpecParseException failure(String path, String message, Throwable cause) {
        return new JobSpecParseException(path, message, cause);
    }

    private abstract static class ParsingContext {
        protected final String path;

        private ParsingContext(String path) {
            this.path = path;
        }
    }

    private static final class ObjectContext extends ParsingContext {
        private final Set<String> seenFields = new HashSet<>();
        private String pendingField;

        private ObjectContext(String path) {
            super(path);
        }
    }

    private static final class ArrayContext extends ParsingContext {
        private int nextIndex;

        private ArrayContext(String path) {
            super(path);
        }
    }
}
