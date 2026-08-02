package io.astrasync.connector.api;

import java.io.*;

public interface SerializableState extends Serializable {

    byte[] toBytes();

    static <T extends SerializableState> T fromBytes(byte[] bytes, Class<T> clazz) {
        try (ByteArrayInputStream bais = new ByteArrayInputStream(bytes);
             ObjectInputStream ois = new ObjectInputStream(bais)) {
            return clazz.cast(ois.readObject());
        } catch (IOException | ClassNotFoundException e) {
            throw new RuntimeException("Failed to deserialize state", e);
        }
    }
}
