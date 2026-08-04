package io.astrasync.engine.network;

/** Constants shared by the framed Coordinator-to-Worker protocol. */
public final class WorkerProtocol {
    public static final int CURRENT_VERSION = 1;
    public static final int CHECKPOINT_VERSION = 2;
    public static final int MAX_FRAME_BYTES = 8 * 1024 * 1024;

    private WorkerProtocol() {}
}
