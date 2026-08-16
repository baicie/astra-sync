# Observability Handbook Changelog

The changelog records every follow-up slice that completes an
implementation referenced by the observability handbook. The slice
column is the Phase 7 Slice 26 follow-up identifier; the commit
column is the source commit on the slice's branch; the PR column is
the GitHub pull request that landed the slice.

| Slice | Commit | Title | PR |
|---|---|---|---|
| 26.F1 | `4006e3e` | build(java): manage logback and logstash-logback-encoder at root pom | (Phase 7 Slice 26 follow-up F1) |
| 26.F1 | `8592f08` | feat(engine): add direct slf4j-api and logback-classic to engine | (Phase 7 Slice 26 follow-up F1) |
| 26.F1 | `6cb6239` | feat(engine): ship logback.xml with logstash JSON encoder for coordinator and worker | (Phase 7 Slice 26 follow-up F1) |
| 26.F1 | `fca7fe7` | test(coordinator): assert Logback wiring loads from classpath | (Phase 7 Slice 26 follow-up F1) |
| 26.F2 | `a188011` | build(java): manage logback and logstash-logback-encoder at root pom | (Phase 7 Slice 26 follow-up F2) |
| 26.F2 | `f670ce2` | feat(engine): add direct slf4j-api and logback-classic to engine | (Phase 7 Slice 26 follow-up F2) |
| 26.F2 | `7047479` | feat(engine): ship logback.xml with logstash JSON encoder for coordinator and worker | (Phase 7 Slice 26 follow-up F2) |
| 26.F2 | `a0d296e` | test(coordinator): assert Logback wiring loads from classpath | (Phase 7 Slice 26 follow-up F2) |
| 26.F2 | `b2a9bbe` | feat(coordinator): migrate CoordinatorApplication error path to SLF4J | (Phase 7 Slice 26 follow-up F2) |
| 26.F2 | `070bad2` | feat(coordinator): migrate ExecutionHeartbeat to SLF4J | (Phase 7 Slice 26 follow-up F2) |
| 26.F2 | `7556e0a` | feat(worker): migrate WorkerApplication error path to SLF4J | (Phase 7 Slice 26 follow-up F2) |
| 26.F2 | `148fc8b` | test(coordinator): assert error path emits SLF4J error event | (Phase 7 Slice 26 follow-up F2) |
| 26.F3 | `818ef64` | feat(observability): add control-plane slog JSON helper | (Phase 7 Slice 26 follow-up F3) |
| 26.F3 | `3f7daec` | feat(api-server): install slog JSON logger and emit structured startup events | (Phase 7 Slice 26 follow-up F3) |
| 26.F3 | `9dae1dd` | feat(console): install slog JSON logger and emit structured startup events | (Phase 7 Slice 26 follow-up F3) |
| 26.F3 | `5e66519` | feat(scheduler): tag structured logs with component=scheduler | (Phase 7 Slice 26 follow-up F3) |
| 26.F3 | `bee22df` | feat(scheduler): tag connection-test-executor logs with component | (Phase 7 Slice 26 follow-up F3) |
| 26.F3 | `e550369` | feat(auth): preserve admin CLI stdout while routing runtime errors through slog | (Phase 7 Slice 26 follow-up F3) |
| 26.F3 | `1c66dc5` | test(api-server): assert loadConfig error message does not leak credentials | (Phase 7 Slice 26 follow-up F3) |
| 26.F4 | `bb4cfd8` | feat(observability): add control-plane slog JSON helper | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `3f7daec` | feat(api-server): install slog JSON logger and emit structured startup events | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `9dae1dd` | feat(console): install slog JSON logger and emit structured startup events | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `5e66519` | feat(scheduler): tag structured logs with component=scheduler | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `9ae31ce` | feat(scheduler): tag structured logs with component=scheduler | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `bee22df` | feat(scheduler): tag connection-test-executor logs with component | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `e550369` | feat(auth): preserve admin CLI stdout while routing runtime errors through slog | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `1c66dc5` | test(api-server): assert loadConfig error message does not leak credentials | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `2d9debe` | feat(observability): register apiserver Prometheus metrics and bind /metrics | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `7e463bb` | feat(observability): register scheduler Prometheus metrics and bind /metrics | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `abf48c9` | feat(observability): register connection-test-executor Prometheus metrics | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `5e216db` | feat(observability): register auth library metrics (admin CLI stays one-shot) | (Phase 7 Slice 26 follow-up F4) |
| 26.F4 | `4336d4e` | feat(observability): register console Prometheus metrics and bind /metrics | (Phase 7 Slice 26 follow-up F4) |
| 26.F5 | `3226096` | feat(helm): add monitoring helpers and metrics containerPort | (Phase 7 Slice 26 follow-up F5) |
| 26.F5 | `0fd0a71` | feat(helm): wire METRICS_LISTEN_ADDRESS into api-server deployment | (Phase 7 Slice 26 follow-up F5) |
| 26.F5 | `3de4f86` | feat(helm): wire metrics into scheduler, connection-test-executor, console deployments | (Phase 7 Slice 26 follow-up F5) |
| 26.F5 | `67a10c0` | feat(helm): add ServiceMonitor templates for Prometheus scraping | (Phase 7 Slice 26 follow-up F5) |
| 26.F5 | `63ab279` | ci(helm): add observability toggle render guard | (Phase 7 Slice 26 follow-up F5) |
| 26.F6 | (this slice) | docs(observability): record F1–F5 implementation status | (Phase 7 Slice 26 follow-up F6) |

The PR column is populated when the slice's pull request merges to
`main`. The PR URLs follow the convention
`https://github.com/baicie/astra-sync/pull/<number>`.
