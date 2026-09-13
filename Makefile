# Makefile for AstraSync

.PHONY: all build build-java build-go build-connectors test test-java test-go test-integration-go test-integration-multi-region vet-go check-security check-runbooks check-docs check clean install format check verify catalog-check catalog-export catalog-info catalog-diff release-dry-run docker-build docker-push proto-generate proto-go-generate proto-lint crd-generate install-hooks

GO_MODULES := control-plane control-plane/api-server control-plane/controller control-plane/scheduler control-plane/catalog control-plane/auth control-plane/observability console
JAVA_PROTO_MODULES := connector-api,protocol/data-protocol,protocol/connector-protocol,protocol/worker-protocol,control-plane/compiler-validation
CONTROLLER_GEN_VERSION := v0.21.0
ADMIN_BIN := control-plane/auth/cmd/admin

# Default target
all: build

# Build all modules
build: build-java build-go build-connectors

# Install local Git hooks
install-hooks:
	@./scripts/install-git-hooks.sh

# Build Java modules (engine, connectors, formats)
build-java:
	@echo "Building Java modules..."
	mvn clean package -DskipTests

# Build Go control plane
build-go:
	@echo "Building Go control plane..."
	@python scripts/run-go-modules.py build

# Build connectors
build-connectors:
	@echo "Building connectors..."
	mvn clean package -pl connectors/connector-jdbc,connectors/connector-debezium,connectors/connector-mysql-cdc,connectors/connector-postgres-cdc,connectors/connector-kafka,connectors/connector-file,connectors/connector-iceberg,connectors/connector-clickhouse -am -DskipTests

# Test
test: test-java test-go

test-java:
	@echo "Running Java tests..."
	mvn test

test-go:
	@echo "Running Go tests..."
	@python scripts/run-go-modules.py test

vet-go:
	@echo "Running Go static analysis..."
	@python scripts/run-go-modules.py vet

# Integration tests
test-integration:
	@echo "Running integration tests..."
	mvn verify -Pintegration-tests

test-integration-go:
	@echo "Running Go integration tests..."
	@python scripts/run-go-modules.py test-integration
	cd tests/integration && go test ./...

test-integration-multi-region:
	@echo "Running multi-region Docker Compose acceptance..."
	@docker info
	@docker compose -f tests/integration/multi-region/docker-compose.yaml config --quiet
	@python scripts/run-multi-region-acceptance.py

# E2E tests
test-e2e:
	@echo "Running E2E tests..."
	cd tests/e2e && ./run-tests.sh

# Code formatting
format:
	@echo "Formatting code..."
	mvn spotless:apply
	@python scripts/run-go-modules.py fmt

# Code style check
check: vet-go check-runbooks check-docs
	@echo "Checking code style..."
	mvn spotless:check

# Phase 7 Slice 24 and Phase 8 multi-region template guard. Verifies that every
# Markdown runbook and Helm multi-region template is safe to commit: it must
# retain a placeholder and contain no known production hostname pattern.
check-runbooks:
	@echo "Checking runbook templates..."
	@python scripts/check-runbook-templates.py
	@python scripts/check-runbook-templates.py --root docs/observability
	@python scripts/check-runbook-templates.py --root deployment/helm/astrasync/templates/multi-region

# Phase 16 Slice 42: cross-cutting documentation gate. Verifies that:
#   1. Every registered template / onboarding doc carries a <placeholder>
#      and contains no production hostname patterns (extends check-runbooks
#      with the Phase 14 ArgoCD README and the Phase 15 catalog authoring
#      guide).
#   2. Every ``docs/phase<N>`` README marked ``**Complete.**`` is
#      referenced in the ``## [Unreleased]`` section of CHANGELOG.md.
check-docs:
	@echo "Checking documentation hygiene..."
	@python scripts/check-runbook-templates.py --all
	@python scripts/check-changelog.py

# Phase 16 Slice 42: dry-run the release process. Verifies the Maven
# project version, the catalog build version, the protobuf file inventory,
# and the CHANGELOG coverage without mutating any file. Used by CI and by
# release prep before tagging a release.
release-dry-run:
	@python scripts/release-dry-run.py $(if $(VERSION),--version $(VERSION),)

# Run the Python script unit tests. The tests live alongside the scripts
# they exercise; the make target is intentionally narrow so the Java and Go
# gates keep their current cadence.
test-scripts:
	@echo "Running script unit tests..."
	@python -m unittest discover -s scripts -p 'test_*.py'

# Security boundary checks: trusted-proxy boundary tests, security response headers,
# the production startup-config negative tests for the API Server and Console,
# and the control-plane mutual TLS gate added by Slice 23 (ADR-045). This gate
# is required by the Repository security checks workflow job.
check-security: vet-go check-mtls
	@echo "Running transport security checks..."
	@set -e; \
	(cd control-plane/auth && go test -count=1 ./transport/...); \
	(cd control-plane/api-server && go test -count=1 -run 'TestLoadConfig|TestAPIHandler|TestLoadTrustedProxy' ./cmd/server/...); \
	(cd console && go test -count=1 -run 'TestLoadConfig|TestConsoleHandler|TestLoadTrustedProxyPrefixesConsole' ./cmd/console/...)

# Phase 7 Slice 23 control-plane mutual TLS verification. Runs the same set of
# tests that check-security runs, restricted to the mTLS suites, so that the
# negotiation path is exercised by an explicit gate in CI.
check-mtls: vet-go
	@echo "Running control-plane mTLS verification..."
	@set -e; \
	(cd control-plane/auth && go test -count=1 -run 'MTLS|ServerTLSConfig|ClientTLSConfig' ./transport/...); \
	(cd control-plane/api-server && go test -count=1 -run 'MTLS|LoadConfig' ./cmd/server/...); \
	(cd console && go test -count=1 -run 'MTLS|LoadConfig' ./cmd/console/...)

# Keep the catalog build id aligned with the compiler image. A per-commit SHA
# would make the committed protobuf change on every commit even when the
# connector descriptors and compiler inputs are unchanged.
CATALOG_BUILD_VERSION ?= 0.8.0
CATALOG_EXECUTION_PROFILE ?= standard
CATALOG_OUTPUT ?= deployment/catalog/connector-inventory.pb

catalog-check:
	mvn -pl cli -am package -DskipTests -DskipITs
	java -jar cli/target/astrasync-cli-0.8.0-all.jar \
		catalog-export target/connector-inventory.pb \
		--compiler-build $(CATALOG_BUILD_VERSION) \
		--execution-profile $(CATALOG_EXECUTION_PROFILE)
	@if ! python scripts/check-files-identical.py deployment/catalog/connector-inventory.pb target/connector-inventory.pb; then \
		echo "::catalog-check::committed catalog differs from freshly-exported catalog; running diff-catalog for diagnostics"; \
		python scripts/diff-catalog.py deployment/catalog/connector-inventory.pb target/connector-inventory.pb || true; \
		exit 1; \
	fi

# Phase 15 Slice 41: regenerate the deployment-authoritative catalog against the
# current commit. Build version is sourced from git, not a hardcoded literal, so
# the catalog embedded build id never drifts when the project bumps its
# version. Override CATALOG_BUILD_VERSION / CATALOG_EXECUTION_PROFILE for
# ad-hoc exports (e.g. release dry-runs).
catalog-export:
	@echo "Exporting connector inventory to $(CATALOG_OUTPUT) (build=$(CATALOG_BUILD_VERSION), profile=$(CATALOG_EXECUTION_PROFILE)) ..."
	mvn -pl cli -am package -DskipTests -DskipITs
	java -jar cli/target/astrasync-cli-0.8.0-all.jar \
		catalog-export $(CATALOG_OUTPUT) \
		--compiler-build $(CATALOG_BUILD_VERSION) \
		--execution-profile $(CATALOG_EXECUTION_PROFILE)

# Phase 15 Slice 41: print a human-readable summary of the committed catalog
# without rebuilding the CLI jar from scratch (uses the same jar that
# catalog-export produces).
catalog-info:
	@python scripts/catalog-info.py

# Phase 15 Slice 41: explain the drift between two catalogs. Used by CI when
# catalog-check fails so the developer sees actionable diagnostics instead of
# a bare "files differ" message. Pass EXPECTED and ACTUAL paths or rely on the
# defaults (committed vs. freshly-exported).
catalog-diff:
	@if [ -z "$(EXPECTED)" ] || [ -z "$(ACTUAL)" ]; then \
		echo "Usage: make catalog-diff EXPECTED=<file> ACTUAL=<file>" >&2; \
		exit 2; \
	fi
	@python scripts/diff-catalog.py "$(EXPECTED)" "$(ACTUAL)"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	mvn clean
	cd control-plane && go clean
	rm -rf */target
	rm -rf */bin

# Install dependencies
install:
	@echo "Installing dependencies..."
	mvn install -DskipTests

# Build Docker images
docker-build:
	@echo "Building Docker images..."
	docker build -t astrasync/worker:latest -f deployment/docker/Dockerfile.worker .
	docker build -t astrasync/api-server:latest -f deployment/docker/Dockerfile.api .
	docker build -t astrasync/compiler-validation:latest -f deployment/docker/Dockerfile.compiler-validation .
	docker build -t astrasync/controller:latest -f deployment/docker/Dockerfile.controller .
	docker build -t astrasync/scheduler:latest -f deployment/docker/Dockerfile.scheduler .
	docker build -t astrasync/connection-test-executor:latest -f deployment/docker/Dockerfile.connection-test-executor .
	docker build -t astrasync/console:latest -f deployment/docker/Dockerfile.console .

# Push Docker images
docker-push:
	@echo "Pushing Docker images..."
	docker push astrasync/worker:latest
	docker push astrasync/api-server:latest
	docker push astrasync/compiler-validation:latest
	docker push astrasync/controller:latest
	docker push astrasync/scheduler:latest
	docker push astrasync/connection-test-executor:latest
	docker push astrasync/console:latest

# Generate protobuf
proto-generate:
	@echo "Generating protobuf code..."
	mvn -B -ntp -pl $(JAVA_PROTO_MODULES) -am compile -DskipTests
	$(MAKE) proto-go-generate

proto-go-generate:
	buf generate api/protobuf --template buf.gen.yaml

proto-lint:
	buf lint api/protobuf

# Generate CRD manifests
crd-generate:
	@echo "Generating CRD manifests..."
	cd control-plane/controller && go run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) crd paths=./api/v1 output:crd:artifacts:config=../../deployment/operator/config/crd/bases

## Build the offline Slice 18 authentication administrator tool (astra-auth-admin).
build-auth-admin:
	cd $(ADMIN_BIN) && go build -o ../../../bin/astra-auth-admin .

## Run the offline Slice 18 authentication administrator tests.
test-auth-admin:
	cd $(ADMIN_BIN) && go test ./...

# Create a new connector
new-connector:
	@echo "Creating new connector scaffold..."
	@read -p "Connector name (e.g., elasticsearch): " name; \
	mkdir -p connectors/connector-$$name/src/main/java/io/astrasync/connectors/$$name; \
	cp connectors/connector-jdbc/pom.xml connectors/connector-$$name/pom.xml; \
	sed -i 's/connector-jdbc/connector-$$name/g' connectors/connector-$$name/pom.xml; \
	echo "Connector $$name created"

# Generate Helm chart values
helm-values:
	@echo "Helm values file:"
	@cat deployment/helm/astrasync/values.yaml

# Deploy to Kubernetes (dev)
k8s-deploy-dev:
	@echo "Deploying to Kubernetes (dev)..."
	helm upgrade --install astrasync deployment/helm/astrasync \
		--namespace astrasync \
		--create-namespace \
		--values deployment/helm/astrasync/values.yaml

# Lint Helm chart
helm-lint:
	helm lint deployment/helm/astrasync

# Run benchmark
benchmark:
	@echo "Running benchmarks..."
	cd tests/benchmark && ./run-benchmark.sh

# Generate documentation
docs:
	@echo "Generating documentation..."

# Help
help:
	@echo "AstraSync Makefile"
	@echo ""
	@echo "Available targets:"
	@sed -n 's/^##//p' Makefile | column -t -s ':' | sed -e 's/^/ /'
