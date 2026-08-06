# Adaptive Batch and Parallelism Control

Phase 5 Slice 15 adds bounded, signal-driven tuning to the existing row runtime. The default
fixed configuration remains unchanged; a JobSpec opts into adaptive batch sizing through an
explicit runtime policy. Non-checkpoint split scheduling can also opt into adaptive parallelism.

## Data Flow

```text
source read -> bounded exchange -> sink write
      |              |                |
      +-- batch timing and queue pressure --+
                         |
                  AdaptiveBatchController
                         |
                  next read limit

completed task latency + remaining splits -> AdaptiveParallelismController -> active workers
```

Batch control uses an EWMA of processing and queue-wait signals. High latency or a full exchange
reduces the next read limit; low latency and an empty exchange increases it. Changes are clamped
to the configured bounds and separated by a cooldown window.

Parallelism control uses completed-task latency and remaining work. It changes the number of
workers receiving new tasks, while already running tasks are allowed to finish. Checkpointed
execution keeps its existing ordered barrier semantics and does not dynamically overlap splits.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-033: Adaptive Batch and Parallelism Control](../../adr/adr-033-adaptive-batch-and-parallelism-control.md)
