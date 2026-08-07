# Phase 5 Slice 16 Implementation Plan

## 1. Contract and design

- [x] Add the Slice 16 design, verification record, and ADR-034.
- [x] Add strict optional spill JobSpec fields with disabled defaults and local Worker directory resolution.
- [x] Define the versioned spill frame, bounded resource ownership, and checkpoint writer/cache contracts.

## 2. Runtime and persistence

- [x] Implement the spill frame codec and optional bounded exchange storage.
- [x] Integrate spill policy into BatchTask, local Worker execution, remote task validation, and Worker environment.
- [x] Centralize checkpoint atomic writes and add bounded per-store state caching without changing JSON recovery semantics.

## 3. Verification and delivery

- [x] Test order, bounds, malformed frames, cleanup, failure propagation, cache reload, and stale checkpoint rejection.
- [x] Add spill/memory JMH smoke coverage and CI wiring.
- [x] Run Maven verification, Spotless, benchmark smoke, and diff checks.
- [ ] Record PR and post-merge evidence, then require all applicable checks before squash merge.
