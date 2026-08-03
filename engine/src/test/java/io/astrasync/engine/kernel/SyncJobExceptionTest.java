package io.astrasync.engine.kernel;

import static org.assertj.core.api.Assertions.assertThat;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.ObjectInputStream;
import java.io.ObjectOutputStream;
import org.junit.jupiter.api.Test;

class SyncJobExceptionTest {
    @Test
    void preservesStageAndPartialResultAcrossSerialization() throws IOException, ClassNotFoundException {
        SyncResult partialResult = new SyncResult(7, 5, 3, 4, 11);
        SyncJobException original = new SyncJobException(
                SyncStage.CANCELLATION_CHECK,
                "failed to check cancellation",
                new IllegalStateException("token boom"),
                partialResult);

        SyncJobException restored = roundTrip(original);

        assertThat(restored.stage()).isEqualTo(SyncStage.CANCELLATION_CHECK);
        assertThat(restored.partialResult()).isEqualTo(partialResult);
        assertThat(restored.getCause()).hasMessage("token boom");
    }

    private static SyncJobException roundTrip(SyncJobException exception) throws IOException, ClassNotFoundException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        try (ObjectOutputStream output = new ObjectOutputStream(bytes)) {
            output.writeObject(exception);
        }
        try (ObjectInputStream input = new ObjectInputStream(new ByteArrayInputStream(bytes.toByteArray()))) {
            return (SyncJobException) input.readObject();
        }
    }
}
