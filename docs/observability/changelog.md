# Observability Handbook Changelog

The changelog records the implementation foundation referenced by the
observability handbook. The slice column is the Phase 7 Slice 26 follow-up
identifier; the commit column identifies the source commit on its original
slice branch; the PR column identifies the closeout pull request that lands
the work on `main`. Descriptor registration and endpoint wiring do not imply
that business call sites or Prometheus exemplars are active; F7 is the first
row that explicitly activates business observations.

| Slice | Commit | Title | PR |
|---|---|---|---|
| 26.F1 | `4006e3e` | build(java): manage logback and logstash-logback-encoder at root pom | [#48][closeout-pr] |
| 26.F1 | `8592f08` | feat(engine): add direct slf4j-api and logback-classic to engine | [#48][closeout-pr] |
| 26.F1 | `6cb6239` | feat(engine): ship logback.xml with logstash JSON encoder for coordinator and worker | [#48][closeout-pr] |
| 26.F1 | `fca7fe7` | test(coordinator): assert Logback wiring loads from classpath | [#48][closeout-pr] |
| 26.F2 | `a188011` | build(java): manage logback and logstash-logback-encoder at root pom | [#48][closeout-pr] |
| 26.F2 | `f670ce2` | feat(engine): add direct slf4j-api and logback-classic to engine | [#48][closeout-pr] |
| 26.F2 | `7047479` | feat(engine): ship logback.xml with logstash JSON encoder for coordinator and worker | [#48][closeout-pr] |
| 26.F2 | `a0d296e` | test(coordinator): assert Logback wiring loads from classpath | [#48][closeout-pr] |
| 26.F2 | `b2a9bbe` | feat(coordinator): migrate CoordinatorApplication error path to SLF4J | [#48][closeout-pr] |
| 26.F2 | `070bad2` | feat(coordinator): migrate ExecutionHeartbeat to SLF4J | [#48][closeout-pr] |
| 26.F2 | `7556e0a` | feat(worker): migrate WorkerApplication error path to SLF4J | [#48][closeout-pr] |
| 26.F2 | `148fc8b` | test(coordinator): assert error path emits SLF4J error event | [#48][closeout-pr] |
| 26.F3 | `818ef64` | feat(observability): add module-local control-plane slog JSON logger foundation | [#48][closeout-pr] |
| 26.F3 | `3f7daec` | feat(api-server): install slog JSON logger and emit structured startup events | [#48][closeout-pr] |
| 26.F3 | `9dae1dd` | feat(console): install slog JSON logger and emit structured startup events | [#48][closeout-pr] |
| 26.F3 | `5e66519` | feat(scheduler): tag structured logs with component=scheduler | [#48][closeout-pr] |
| 26.F3 | `bee22df` | feat(scheduler): tag connection-test-executor logs with component | [#48][closeout-pr] |
| 26.F3 | `e550369` | feat(auth): preserve admin CLI stdout while routing runtime errors through slog | [#48][closeout-pr] |
| 26.F3 | `1c66dc5` | test(api-server): assert loadConfig error message does not leak credentials | [#48][closeout-pr] |
| 26.F4 | `bb4cfd8` | feat(observability): add control-plane slog JSON helper | [#48][closeout-pr] |
| 26.F4 | `3f7daec` | feat(api-server): install slog JSON logger and emit structured startup events | [#48][closeout-pr] |
| 26.F4 | `9dae1dd` | feat(console): install slog JSON logger and emit structured startup events | [#48][closeout-pr] |
| 26.F4 | `5e66519` | feat(scheduler): tag structured logs with component=scheduler | [#48][closeout-pr] |
| 26.F4 | `9ae31ce` | feat(scheduler): tag structured logs with component=scheduler | [#48][closeout-pr] |
| 26.F4 | `bee22df` | feat(scheduler): tag connection-test-executor logs with component | [#48][closeout-pr] |
| 26.F4 | `e550369` | feat(auth): preserve admin CLI stdout while routing runtime errors through slog | [#48][closeout-pr] |
| 26.F4 | `1c66dc5` | test(api-server): assert loadConfig error message does not leak credentials | [#48][closeout-pr] |
| 26.F4 | `2d9debe` | feat(observability): register apiserver Prometheus descriptors and bind /metrics | [#48][closeout-pr] |
| 26.F4 | `7e463bb` | feat(observability): register scheduler Prometheus descriptors and bind /metrics | [#48][closeout-pr] |
| 26.F4 | `abf48c9` | feat(observability): register connection-test-executor Prometheus descriptors | [#48][closeout-pr] |
| 26.F4 | `5e216db` | feat(observability): register auth descriptor package (admin CLI stays one-shot) | [#48][closeout-pr] |
| 26.F4 | `4336d4e` | feat(observability): register console Prometheus descriptors and bind /metrics | [#48][closeout-pr] |
| 26.F5 | `3226096` | feat(helm): add monitoring helpers and metrics containerPort | [#48][closeout-pr] |
| 26.F5 | `0fd0a71` | feat(helm): wire METRICS_LISTEN_ADDRESS into api-server deployment | [#48][closeout-pr] |
| 26.F5 | `3de4f86` | feat(helm): wire metrics into scheduler, connection-test-executor, console deployments | [#48][closeout-pr] |
| 26.F5 | `67a10c0` | feat(helm): add ServiceMonitor templates for Prometheus scraping | [#48][closeout-pr] |
| 26.F5 | `63ab279` | ci(helm): add observability toggle render guard | [#48][closeout-pr] |
| 26.F6 | (this slice) | docs(observability): record F1–F5 foundation status and deferred instrumentation | (Phase 7 Slice 26 follow-up F6) |
| 26.F7 | `49debb6` | feat(observability): instrument API Server SLO metrics and bounded exemplars | [#49][f7-pr] |

The PR column is populated when the closeout pull request merges to `main`.
The PR URLs follow the convention
`https://github.com/baicie/astra-sync/pull/<number>`; the `<number>` token is
kept as the template placeholder required by the repository guard.

[closeout-pr]: https://github.com/baicie/astra-sync/pull/48
[f7-pr]: https://github.com/baicie/astra-sync/pull/49
