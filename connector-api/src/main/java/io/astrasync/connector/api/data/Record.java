package io.astrasync.connector.api.data;

public interface Record {

    Object getValue(String columnName);

    <T> T getValue(int columnIndex);

    Record setValue(String columnName, Object value);

    Record setValue(int columnIndex, Object value);

    int getColumnCount();

    String getString(String columnName);

    Integer getInt(String columnName);

    Long getLong(String columnName);

    Double getDouble(String columnName);

    Boolean getBoolean(String columnName);

    byte[] getBytes(String columnName);

    boolean isNull(String columnName);
}
