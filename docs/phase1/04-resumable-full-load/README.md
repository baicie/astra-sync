# Phase 1 Slice 04: Resumable Full-load Execution

Slice 04 makes static full-load work resumable at split boundaries and turns the network Worker into
an executable, health-checked deployment unit.

## Delivered

- Deterministic split-plan and split fingerprints with plan-drift rejection.
- Versioned JSON progress manifests with file locking, forced temporary writes, and atomic replacement.
- First-success-wins completion records containing the successful Worker and cumulative split metrics.
- `ResumableBatchCoordinator`, which skips durable completions and materializes only pending splits.
- Completion-order result handling and a failure latch that prevents queued work from starting after a
  task failure is observed.
- Executable Worker service, environment validation, provider plugin boundary, and TCP health probe.
- Shaded Worker JAR, multi-stage Worker image, guarded Helm deployment, and headless Worker Service.

## Resume Contract

The caller supplies a stable job ID and enumerates the entire split set on every invocation. The first
invocation creates the plan manifest. Subsequent invocations must enumerate the exact same split IDs,
source identities, and boundaries. Completed splits are not passed to the task factory; unfinished
splits run again from their original boundaries.

The manifest directory must be a durable, single-writer volume supporting file locks and atomic rename.
There is no automatic cleanup or plan migration. Use a new job ID for a deliberately changed plan.

## Worker Contract

The Worker requires `ASTRASYNC_WORKER_ID` and `ASTRASYNC_TASK_FACTORY_PROVIDER`. The provider class
must implement `WorkerTaskFactoryProvider`, have a public no-argument constructor, and return a
non-null `BatchTaskFactory`. Provider JARs and their dependencies belong on `/app/plugins/*` in the
container image. Helm keeps Workers disabled until this contract is configured.

## Delivery Boundary

The durable record is written after a Worker reports sink success. A crash between sink success and
manifest replacement can replay the split, so this slice makes no exactly-once claim. It also adds no
intra-split checkpoints, automatic retries, Coordinator epoch fencing, leader election, Worker
registration, TLS, or service discovery controller.
