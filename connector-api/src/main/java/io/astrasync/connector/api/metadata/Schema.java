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

public interface Field {

    int getId();

    String getName();

    LogicalType getLogicalType();

    boolean isNullable();

    String getComment();

    Object getDefaultValue();
}

public enum LogicalType {
    BOOLEAN,
    TINYINT,
    SMALLINT,
    INTEGER,
    BIGINT,
    FLOAT,
    DOUBLE,
    DECIMAL(int scale, int precision),
    STRING,
    VARCHAR(int length),
    CHAR(int length),
    BINARY,
    VARBINARY(int length),
    DATE,
    TIME,
    TIME_WITH_TZ,
    TIMESTAMP,
    TIMESTAMP_WITH_TZ,
    ARRAY(LogicalType elementType),
    MAP(LogicalType keyType, LogicalType valueType),
    ROW(List<Field> fields),
    JSON,
    XML,
    UUID,
    STRUCT,
    UNION,
    RAW
}
