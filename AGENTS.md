# AstraSync — Agent Guidelines

本文件是 **所有 AI 编程代理(Cursor / Claude / Codex / 其他)** 在 `astrasync` 仓库内协作的统一规则。
更细粒度、面向特定目录的规则放在 `.cursor/rules/` 下;代理在进入对应路径前应同时加载那些规则。

---

## 0. 语言与回复

- 默认使用 **中文** 回复用户提问、解释代码、提交说明和 PR 描述(除非用户明确指定其他语言)。
- 代码、标识符、文件名、CLI 命令、提交 type/scope、ADR 标题、proto 字段名一律保持 **英文**。
- 中英混合时,代码片段前后保留一行英文上下文即可,不要把整段代码翻译成中文注释。
- 不要在解释中使用 emoji,除非用户主动要求。

---

## 1. 项目一览

AstraSync 是一个 **分布式数据同步引擎**,目标是把数据库 / 消息队列 / 文件系统 / 数据湖 / 数据仓库的同步
统一到同一套运行时上,支持离线全量、实时增量、全量+增量无缝衔接。

| 维度 | 内容 |
| --- | --- |
| 控制平面 | Go (`control-plane/*`, `console`)。 |
| 数据平面 | Java 21 (`engine/*`, `connectors/*`, `formats/*`, `transforms/*`)。 |
| 协议 | Protobuf (`api/protobuf`, `protocol/*`)。 |
| CLI / 工具 | `cli` (Java, picocli)。 |
| 部署 | `deployment/{docker,helm,operator}`。 |
| 测试 | `tests/{e2e,compatibility,chaos,benchmark}`。 |
| 架构基线 | `docs/architecture.md`,ADR-001 ~ ADR-042 在 `docs/adr/`。 |

**核心不变量**(架构基线,不可破坏):

1. 控制面和数据面是独立的拥有权和故障域。
2. Direct Pipeline 是默认拓扑;Durable Relay、Batch Materialization 必须是显式选择。
3. Batch 和 Stream 任务共享同一套 Job / Connector / State 概念。
4. 投递保证(Exactly/At-least/At-most)基于 **真实能力协商**,不支持时必须 **前置拒绝**,不允许静默降级。
5. 每个执行路径必须有 **有界 batch、有界 queue、显式背压**。
6. 协调元数据 / 大状态 / 批量数据 各自使用专用的存储(PostgreSQL / etcd / 对象存储)。
7. 同一时刻一个 Job 只能有一个 active execution epoch,过期 writer 必须被 epoch fence。

详细 ADR 见 `docs/adr/README.md`。**任何违反以上不变点的修改必须先新增或修改 ADR,再写代码。**

---

## 2. 仓库结构与导航

```
astrasync/
├── api/                         # API 定义
│   ├── openapi/                 # OpenAPI 规范
│   └── protobuf/                # Protocol Buffer 源文件
├── protocol/                    # 编译后的 Java protobuf 模块
├── connector-api/               # Source / Sink / Catalog SPI
├── engine/                      # 数据面运行时
│   ├── runtime/                 # 批/Cdc 任务、batch exchange、spill、adaptive policy
│   ├── checkpoint/              # Checkpoint 协调与持久化
│   ├── network/                 # Netty 网络层
│   ├── coordinator/             # JobCoordinator
│   ├── worker/                  # Worker 运行时
│   ├── state/                   # 状态后端(RocksDB)
│   └── scheduler-spi/           # 调度 SPI
├── formats/                     # Arrow / Row / JSON / Parquet
├── transforms/                  # sql / mask / schema 转换
├── connectors/                  # JDBC / MySQL CDC / PG CDC / Kafka / File / Iceberg / ClickHouse / Debezium
├── control-plane/               # Go 多模块
│   ├── api-server/              # REST + gRPC API,RBAC
│   ├── controller/              # Job 生命周期
│   ├── scheduler/               # 资源调度
│   ├── catalog/                 # 元数据目录
│   ├── auth/                    # 认证 + OIDC + 审计
│   ├── connection/              # 连接校验
│   ├── job/                     # Job 编译 / 验证
│   └── compiler-validation/     # 编译器服务
├── console/                     # 控制台 Web(Go)
├── cli/                         # CLI (Java)
├── deployment/                  # Docker / Helm / Operator
├── tests/                       # e2e / 兼容性 / chaos / benchmark
├── docs/                        # 架构、ADR、阶段文档
└── scripts/                     # 工具脚本
```

---

## 3. 必备工具链

| 工具 | 最低版本 | 用途 |
| --- | --- | --- |
| Java | 21 | 数据面编译运行 |
| Maven | 3.9+ | Java 模块构建 |
| Go | 1.22+ | 控制面编译运行 |
| Docker / Compose | — | 本地部署、e2e |
| Helm | — | K8s 部署 |
| protoc / Buf | 26.0+ / 1.36+ | Protobuf 生成 |

`Makefile` 暴露的常用目标:

```bash
make install-hooks      # 安装本地 Git 钩子
make build              # = build-java + build-go + build-connectors
make test               # = test-java + test-go
make vet-go             # 全部 Go 模块静态检查
make check              # = vet-go + spotless:check
make format             # = mvn spotless:apply + go fmt ./...
make catalog-check      # 验证 Connector 目录与编译产物一致
make proto-generate     # 生成 protobuf(Java + Go)
make crd-generate       # 生成 Operator CRD
```

**禁止**直接绕过 Makefile 编译后提交 — 这会与 CI 不一致。

---

## 4. 通用质量约束(适用于所有语言 / 模块)

### 4.1 项目质量总则

1. **不变量优先于特性**:任何破坏架构基线 / ADR / checkpoint 边界的改动必须先更新或新建 ADR。
2. **精确错误处理**:
   - Go:使用 `google.golang.org/grpc/status` 表达 gRPC 错误码,绝不在响应里泄露数据库/驱动内部细节(参考 `AccessService` 中的 `Internal` + 脱敏)。
   - Java:不要把 catch 的异常原样序列化到响应;封装为领域异常或在 sink/source 边界重新分类。
3. **事务边界明确**:
   - 控制面任何 RBAC 变更必须 **同时** 写 membership / role 行 **和** security audit 行,且失败时一并回滚(见 ADR-037)。
   - 数据面任何 sink 写必须满足 *transactional* 或 *idempotent* 之一(见 ADR-027)。
4. **幂等性**:所有 mutate 类 RPC(`Grant*`、`Revoke*`、`Create*`、`Update*`、`Delete*`)必须支持 **幂等键**,长度合理(项目惯例 ≥ 16 字符)。
6. **可观测性**:`stderr/stdout` 日志与 metrics 使用项目已有的 SLF4J / zap / Prometheus 约定,新增模块必须挂上同款 logger name。
7. **可测试性**:所有公共 service 必须有 `*_test.go`(Go)或 `*Test.java`(Java)覆盖 happy path + 拒绝路径 + 边界条件。
8. **构建可重现**:不要提交 `bin/`、`target/`、`.flattened-pom.xml` 等生成产物;`.gitignore` 已覆盖,新增生成物也要同步补进 `.gitignore`。
9. **依赖**:新增 Maven 依赖必须落到根 `pom.xml` 的 `<properties>` 统一版本;新增 Go 依赖必须 `go mod tidy` 并解释用途。
10. **不要提交敏感信息**:密码、token、`.env`、`kubeconfig` 等一律不允许进入仓库;测试用 fixture 写到 `tests/fixtures/`(如不存在则创建),并使用明显占位符(如 `change-me`)。

### 4.2 代码风格

- Go:`gofmt` 强制;遵循 Effective Go 与项目现有命名(`AccessService`、`NewAccessService`、
  `WithAccessClock` 等 `NewXxx` / `WithXxx` 选项模式)。Go 的 **ctx** 作为函数第一个参数,名为 `ctx`。
- Java:由 Spotless + palantir-java-format 统一格式化;`./mvnw spotless:apply` 在提交前跑一次。
  - 必须使用 `@Override`、`Objects.requireNonNull(...)` 做参数校验(record canonical constructor 内)。
  - 不允许出现裸 `catch (Exception e)` 后吞掉;要么重新抛出,要么记日志并翻译成领域错误。
- 不写 *“明显”* 注释(// import / // increment);只为表达 **意图 / 取舍 / 不变量** 写注释。
- 提交前必须:

  ```bash
  make check
  make test-java
  make test-go
  ```

  CI 失败等同于本地失败,不要试图"先合再说"。

### 4.3 提交与分支

- 提交格式:Conventional Commits,允许 type:`build chore ci docs feat fix perf refactor revert style test`。
  Subject 用小写、祈使句、不加句号、不超过 100 字符。
  Breaking change 必须加 `!` 或 footer:`BREAKING CHANGE: <说明>`。
- 仓库根 `.gitmessage` 已是模板,推荐 `git config commit.template .gitmessage`。
- 分支策略见 `CONTRIBUTING.md`:`main` / `develop` / `feature/*` / `fix/*` / `release/*`。
- PR 提交前请勾选 `CONTRIBUTING.md` 中 PR Checklist(代码风格、测试、文档、commit message、无冲突)。

---

## 5. 按语言 / 模块的专属规则

请在进入下列目录前同时加载对应规则:

| 路径 | 规则 |
| --- | --- |
| `control-plane/**/*.go`、`control-plane/auth/**` | `.cursor/rules/go-control-plane.mdc` |
| `engine/**`、`connectors/*`、`formats/*`、`transforms/*`、`cli/**` | `.cursor/rules/java-data-plane.mdc` |
| `api/protobuf/**`、`protocol/**` | `.cursor/rules/protobuf-contracts.mdc` |
| `connector-api/**`、`connectors/**`、`formats/**`、`transforms/**` | `.cursor/rules/connector-spi.mdc` |
| `**/*_test.go`、`**/src/test/**` | `.cursor/rules/testing.mdc` |
| `.githooks/**`、`.github/**`、`docs/adr/**`、`docs/**`、`CHANGELOG*` | `.cursor/rules/docs-and-process.mdc` |

---

## 6. Agent 工作流(推荐流程)

1. **读上下文**:进入新目录先读 `README.md`、附近 `*_test.go` 或 `*Test.java`,理解当前不变量和测试风格。
2. **查 ADR**:任何涉及 checkpoint、fence、exactly-once、auth/RBAC、catalog、credential、CDC 边界
   的修改,**先**到 `docs/adr/` 找到对应 ADR 并读一遍。
3. **写测试先行**:遵循 `testing.mdc` 的 red-green-refactor 流程。
4. **改代码**:遵守 `.cursor/rules/` 下对应规则。
5. **跑本地检查**:`make check && make test`(Java + Go)。
6. **更新 ADR / 文档**:若改动触发了架构不变量调整,新建或更新 ADR;用户面文档同步更新。
7. **写提交信息**:Conventional Commits,中文描述放在 body(若需),subject 保持英文小写。

---

## 7. 反模式(请主动避免)

- 在 Go 测试中用 `t.Skip(...)` 跳过关键不变量断言。
- 在 Java 中抛出后又 `e.printStackTrace()` 而不是用 SLF4J。
- 在 Sink 端同时开 *和* 关闭 transaction(违反 ADR-027)。
- 在 API handler 中拼 SQL 或拼 Protobuf 文本;通过生成的 `gen/go/v1` 或 `gen-java` 类型操作。
- 用 `panic` 处理预期错误;只在真正不可恢复时使用。
- 在生产路径里 `time.Sleep(...)`、`runtime.Gosched()`、自旋等待 — 必须用 channel / context / condition。
- 让一个 Job 跨多个 active execution epoch 同时写(违反 ADR-006 / ADR-029)。
- 把 secret / 密码硬编码进仓库或日志中。
- 直接修改 `target/`、`bin/`、`*.pb.go`、`*.flattened-pom.xml` 等生成产物。
- 在未跑 `make catalog-check` 的情况下改 connector descriptor。

---

## 8. 需要人审决定的场景

遇到以下任一情况,**停下来**通过 `AskQuestion` 与用户确认,不要擅自决定:

- 改动是否破坏架构不变量或 ADR。
- 是否要引入新的 Maven / Go 依赖。
- 是否要新增 / 修改 protobuf 字段(意味着版本兼容问题)。
- 是否要扩展 RBAC 角色或新增 audit event type。
- 是否要在数据面引入新的存储后端、新的序列化格式、新的 checkpoint 策略。
- 是否要修改 `Dockerfile`、`Helm chart`、`CRD`(运维影响)。

---

## 9. 参考资料

- `README.md`、`CONTRIBUTING.md`
- `docs/architecture.md` — 架构基线
- `docs/adr/README.md` — 所有 ADR 索引
- `docs/phase*/README.md` — 各阶段交付
- `docs/connector-dev.md` — connector 开发
- `docs/deployment.md` — 部署