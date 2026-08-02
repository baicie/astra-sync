package io.astrasync.connector.api.metadata;

public interface ColumnDescriptor {
    int getColumnId();

    String getColumnName();

    LogicalType getDataType();

    boolean isNullable();

    boolean isPrimaryKey();

    String getComment();

    String getDefaultValue();
}
