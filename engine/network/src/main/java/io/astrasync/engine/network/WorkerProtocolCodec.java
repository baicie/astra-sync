package io.astrasync.engine.network;

import io.astrasync.protocol.worker.WorkerRequest;
import io.astrasync.protocol.worker.WorkerResponse;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;

final class WorkerProtocolCodec {
    private WorkerProtocolCodec() {}

    static void writeRequest(OutputStream output, WorkerRequest request) throws IOException {
        writeFrame(output, request.toByteArray());
    }

    static void writeResponse(OutputStream output, WorkerResponse response) throws IOException {
        writeFrame(output, response.toByteArray());
    }

    static WorkerRequest readRequest(InputStream input) throws IOException {
        return WorkerRequest.parseFrom(readFrame(input));
    }

    static WorkerResponse readResponse(InputStream input) throws IOException {
        return WorkerResponse.parseFrom(readFrame(input));
    }

    private static void writeFrame(OutputStream output, byte[] payload) throws IOException {
        if (payload.length > WorkerProtocol.MAX_FRAME_BYTES) {
            throw new IOException("worker protocol frame exceeds maximum size");
        }
        DataOutputStream data = new DataOutputStream(output);
        data.writeInt(payload.length);
        data.write(payload);
        data.flush();
    }

    private static byte[] readFrame(InputStream input) throws IOException {
        DataInputStream data = new DataInputStream(input);
        int length;
        try {
            length = data.readInt();
        } catch (EOFException exception) {
            throw new EOFException("worker protocol frame is missing");
        }
        if (length <= 0) {
            throw new IOException("invalid worker protocol frame length: " + length);
        }
        if (length > WorkerProtocol.MAX_FRAME_BYTES) {
            throw new IOException("worker protocol frame exceeds maximum size: " + length);
        }
        byte[] payload = new byte[length];
        data.readFully(payload);
        return payload;
    }
}
