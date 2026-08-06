# AstraSync

**AstraSync** is a distributed big data synchronization engine that unifies database, message queue, file system, data lake, and data warehouse synchronization into a single runtime. It supports offline full-load, real-time incremental, and unified full+incremental synchronization.

## Design Philosophy

AstraSync is built on proven open-source patterns rather than reinventing fundamental concepts:

| Reference | Architecture Pattern |
|-----------|---------------------|
| **Apache Flink** | DAG execution, JobCoordinator/Worker, Checkpoint, Fault tolerance |
| **Apache SeaTunnel** | Source Split/Reader/Writer/Committer, Connector decoupling |
| **Debezium** | Native log-based CDC for different databases |
| **Kafka Connect** | Connector tasking, Offset management |
| **Airbyte** | Control plane and connector isolation |
| **Apache Arrow** | Columnar batch data representation |
| **Apache Iceberg** | Stable field IDs, Schema evolution |

## Key Features

### Core Capabilities

- **Unified Data Sources**: MySQL, PostgreSQL, Oracle, SQL Server, MongoDB, Kafka, Pulsar, S3, HDFS, Iceberg, ClickHouse
- **Delivery Guarantees**: Exactly-once, At-least-once, At-most-once (capability-negotiated)
- **Three Pipeline Modes**:
  - **Direct Pipeline**: Low-latency direct transfer (default)
  - **Durable Relay**: Kafka/Pulsar-based persistence and fan-out
  - **Batch Materialization**: File-based staging for TB/PB-scale migration

### Technical Highlights

- **Control/Data Plane Separation**: Control plane (Go) and data plane (Java) operate independently
- **Adaptive Parallelism**: Dynamic split and load balancing
- **Full + Incremental Seamless Handoff**: Snapshot-to-CDC transition without long locks
- **Native CDC**: Debezium-backed MySQL binlog and PostgreSQL logical replication sources
- **Epoch Fencing**: Prevents split-brain during coordinator failover
- **Multi-Format Support**: Row Binary for CDC, Arrow RecordBatch for bulk processing

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Control Plane (Go)                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│  │API Server│ │Controller│ │Scheduler │ │ Catalog  │           │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘           │
│       │             │             │             │                  │
│       └─────────────┴──────┬──────┴─────────────┘                  │
│                            │                                      │
│                    ┌───────┴───────┐                              │
│                    │  etcd 3/5     │                              │
│                    └───────┬───────┘                              │
│                            │                                      │
│                    ┌───────┴───────┐                              │
│                    │  PostgreSQL   │                              │
│                    └───────────────┘                              │
└─────────────────────────────────────────────────────────────────┘
                             │
                             │ gRPC
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Data Plane (Java)                         │
│  ┌─────────────┐                                                 │
│  │Job Coordinator│                                                │
│  └──────┬──────┘                                                 │
│         │                                                         │
│  ┌──────┴──────┐                                                 │
│  │   Workers   │                                                 │
│  └──┬──────┬──┘                                                 │
│     │      │                                                      │
│  ┌──┴──┐ ┌┴────┐ ┌────────┐                                    │
│  │Source│ │Shuffle│ │  Sink  │                                    │
│  └──┬──┘ └──┬───┘ └───┬────┘                                    │
│     │        │         │                                          │
│     └────────┴────┬────┘                                         │
│                    │                                              │
│             ┌──────┴──────┐                                      │
│             │Local RocksDB │                                      │
│             └─────────────┘                                       │
└─────────────────────────────────────────────────────────────────┘
```

## Repository Structure

```
astrasync/
├── api/                      # API definitions
│   ├── openapi/              # OpenAPI specs
│   └── protobuf/            # Protocol Buffer definitions
├── control-plane/            # Go-based control plane
│   ├── api-server/           # REST/gRPC API server
│   ├── controller/           # Job lifecycle controller
│   ├── scheduler/            # Resource scheduler
│   ├── catalog/              # Metadata catalog
│   └── auth/                 # Authentication service
├── engine/                   # Java-based data plane runtime
│   ├── coordinator/          # Job coordinator
│   ├── worker/               # Worker runtime
│   ├── runtime/              # Core DAG runtime
│   ├── network/              # Netty network layer
│   ├── state/                # State management
│   ├── checkpoint/            # Checkpoint coordination
│   └── scheduler-spi/        # Scheduler interface
├── connector-api/            # Unified connector SPI
│   ├── source-api/           # Source connector interface
│   ├── sink-api/             # Sink connector interface
│   └── catalog-api/          # Table catalog interface
├── connectors/                # Connector implementations
│   ├── connector-jdbc/        # Generic JDBC
│   ├── connector-mysql-cdc/   # MySQL CDC (Debezium)
│   ├── connector-postgres-cdc/ # PostgreSQL CDC (Debezium)
│   ├── connector-kafka/       # Kafka source/sink
│   ├── connector-file/        # File source/sink
│   ├── connector-iceberg/    # Apache Iceberg sink
│   └── connector-clickhouse/  # ClickHouse sink
├── formats/                   # Data format handlers
│   ├── arrow-format/          # Arrow RecordBatch
│   ├── row-format/            # Row binary
│   ├── json-format/           # JSON
│   └── parquet-format/        # Parquet
├── transforms/               # Data transformations
│   ├── sql-transform/        # SQL-based transform
│   ├── mask-transform/        # Column masking
│   └── schema-transform/      # Schema evolution
├── protocol/                 # Wire protocols
│   ├── data-protocol/         # Data exchange protocol
│   └── connector-protocol/     # Connector control protocol
├── deployment/               # Deployment configurations
│   ├── docker/                # Dockerfiles
│   ├── helm/                  # Helm charts
│   └── operator/              # Kubernetes Operator
├── tests/                    # Test suites
│   ├── e2e/                   # End-to-end tests
│   ├── compatibility/         # Connector compatibility
│   ├── chaos/                 # Chaos engineering
│   └── benchmark/             # Performance benchmarks
└── docs/                    # Documentation
    └── adr/                   # Architecture Decision Records
```

## Quick Start

### Prerequisites

- Java 21+
- Maven 3.9+
- Go 1.22+
- Docker & Docker Compose (for local development)

### Build

```bash
# Clone the repository
git clone https://github.com/astrasync/astra-sync.git
cd astrasync

# Build all modules
mvn clean package -DskipTests

# Build with code formatting
mvn clean package
```

### Distributed JDBC Demo

```bash
# Start PostgreSQL and two stable TCP Workers.
docker compose -f deployment/docker/docker-compose.dev.yml up --build --wait postgres worker-0 worker-1

# Run the one-shot resumable Coordinator.
docker compose -f deployment/docker/docker-compose.dev.yml run --rm --build coordinator
```

### Create a Sync Job

```yaml
apiVersion: sync.astrasync.io/v1
kind: SyncJob
metadata:
  name: mysql-to-jdbc-cdc
spec:
  source:
    connector: mysql-cdc
    options:
      hostname: source.example
      port: "3306"
      username: sync_reader
      password: change-me
      database: shop
      tables: shop.orders
      topicPrefix: shop-source
      serverId: "5401"
      schemaHistoryFile: ./state/mysql-schema-history.dat
  sink:
    connector: jdbc
    options:
      url: jdbc:postgresql://target.example/shop
      user: sync_writer
      password: change-me
      table: orders
      keyColumns: id
  delivery:
    guarantee: exactly-once
  runtime:
    maxBatchRecords: 2048
```

The current CDC runtime is a local checkpointed runner. A CDC JDBC sink must provide `table` and
`keyColumns`; it applies inserts, updates, and deletes and stores idempotent commit markers in the
target database. See the Phase 3 documentation for the PostgreSQL source options and operational
constraints.

## Documentation

- [Architecture Overview](./docs/architecture.md)
- [ADR Index](./docs/adr/README.md)
- [Phase 3 CDC](./docs/phase3/README.md)
- [Phase 4 Control Plane](./docs/phase4/README.md)
- [Phase 5 Performance](./docs/phase5/README.md)
- [CDC Usage and Delivery Boundary](./docs/phase3/01-native-cdc/README.md)
- [Connector Development Guide](./docs/connector-dev.md)
- [Deployment Guide](./docs/deployment.md)

## Roadmap

| Phase | Focus | Status |
|-------|-------|--------|
| Phase 0 | Protocol & Single-node Kernel | Complete |
| Phase 1 | Distributed Batch Sync | Complete |
| Phase 2 | Checkpoint & Exactly-Once | Complete |
| Phase 3 | CDC (MySQL, PostgreSQL) | Complete |
| Phase 4 | Control Plane HA | Complete |
| Phase 5 | Performance Optimization | In Progress |
| Phase 6 | Platform (Web Console, RBAC) | Planning |

## Contributing

Contributions are welcome. Please read our [Contributing Guide](./CONTRIBUTING.md) before submitting PRs. Install the local commit hook with `./scripts/install-git-hooks.sh` or `./scripts/install-git-hooks.ps1`; commits use the Conventional Commits format.

## License

AstraSync is licensed under the Apache License 2.0. See [LICENSE](./LICENSE) for details.

## References

1. [Apache Flink Architecture](https://nightlies.apache.org/flink/flink-docs-release-2.2/docs/concepts/flink-architecture/)
2. [Apache SeaTunnel](https://seatunnel.apache.org/)
3. [Debezium](https://debezium.io/)
4. [Apache Kafka](https://kafka.apache.org/)
5. [Airbyte](https://airbyte.com/)
6. [Apache Arrow](https://arrow.apache.org/)
7. [Apache Iceberg](https://iceberg.apache.org/)
