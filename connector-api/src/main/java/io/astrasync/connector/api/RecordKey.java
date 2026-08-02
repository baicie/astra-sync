package io.astrasync.connector.api;

public interface RecordKey {

    byte[] toBytes();

    static RecordKey of(Object... values) {
        throw new UnsupportedOperationException("Implement in subclass");
    }

    static RecordKey empty() {
        throw new UnsupportedOperationException("Implement in subclass");
    }
}
