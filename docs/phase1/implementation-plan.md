# Phase 1 Slice 01 Implementation Plan

1. Add a shared runtime module containing the task, Worker, exchange, and result contracts.
2. Add an in-process Worker implementation with bounded Source-to-Sink execution.
3. Add a Coordinator that assigns tasks to Workers with per-Worker serialization and failure
   cancellation.
4. Add focused tests for exchange limits, successful parallel tasks, serialization, resource
   closure, and failure propagation.
5. Run focused Maven verification, full Java verification, formatting, and diff checks.

The implementation must not add a remote service, checkpoint store, retry loop, or stronger
delivery guarantee as an implicit dependency.
