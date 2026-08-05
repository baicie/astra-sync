# Phase 3 CDC Implementation Plan

- [x] Define immutable CDC events, structured keys, source positions, batches, source/sink SPIs,
      and capability validation.
- [x] Add Debezium Embedded conversion, offset backing store, deterministic offset encoding, and
      snapshot/streaming handoff state.
- [x] Add validated MySQL binlog and PostgreSQL logical-replication connector factories with
      ServiceLoader descriptors.
- [x] Add checkpoint-coupled CDC worker and coordinator with sink-first ordering and Phase 2 epoch
      fencing.
- [x] Add JDBC CDC sink support for insert, update, delete, idempotent commit markers, and digest
      conflict detection.
- [x] Add CDC compiler mode, local runner, CLI command, checkpoint options, and recovery tests.
- [x] Add unit tests for contracts, conversion, offsets, connector options, sink idempotency,
      worker ordering, coordinator recovery, and connector discovery.
- [x] Add Docker-backed MySQL and PostgreSQL integration tests that skip when Docker is unavailable.
- [x] Document the design, usage boundary, operational options, ADR, and verification evidence.
- [x] Run the full Java test, formatting, and diff gates.
