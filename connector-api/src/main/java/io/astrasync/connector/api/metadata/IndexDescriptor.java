package io.astrasync.connector.api.metadata;

import java.util.List;

public interface IndexDescriptor {
    String getIndexName();

    List<String> getColumns();

    boolean isUnique();

    IndexType getIndexType();
}
