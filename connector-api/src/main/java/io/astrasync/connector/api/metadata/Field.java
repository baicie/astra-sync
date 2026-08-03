package io.astrasync.connector.api.metadata;

public interface Field {
    int getId();

    String getName();

    LogicalType getLogicalType();

    boolean isNullable();

    String getComment();

    Object getDefaultValue();
}
