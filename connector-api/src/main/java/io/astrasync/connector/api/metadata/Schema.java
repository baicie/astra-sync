package io.astrasync.connector.api.metadata;

import java.util.List;

public interface Schema {

    String getSchemaId();

    int getVersion();

    List<Field> getFields();

    Field getField(int fieldId);

    Field getField(String name);

    boolean isCompatible(Schema other);

    Schema evolve(List<Field> addedFields, List<Integer> removedFieldIds);

    int getFieldCount();
}
