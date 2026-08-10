# Makefile for AstraSync

.PHONY: all build build-java build-go build-connectors test test-java test-go vet-go clean install format check verify catalog-check docker-build docker-push proto-generate proto-go-generate proto-lint crd-generate install-hooks

GO_MODULES := control-plane control-plane/api-server control-plane/controller control-plane/scheduler control-plane/catalog control-plane/auth console
CONTROLLER_GEN_VERSION := v0.15.0

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
	@set -e; for module in $(GO_MODULES); do \
		echo "Building $$module..."; \
		(cd "$$module" && go build ./...); \
	done

# Build connectors
build-connectors:
	@echo "Building connectors..."
	mvn clean package -pl connectors/connector-jdbc,connectors/connector-mysql-cdc,connectors/connector-postgres-cdc,connectors/connector-kafka -am -DskipTests

# Test
test: test-java test-go

test-java:
	@echo "Running Java tests..."
	mvn test

test-go:
	@echo "Running Go tests..."
	@set -e; for module in $(GO_MODULES); do \
		echo "Testing $$module..."; \
		(cd "$$module" && go test ./...); \
	done

vet-go:
	@echo "Running Go static analysis..."
	@set -e; for module in $(GO_MODULES); do \
		echo "Vetting $$module..."; \
		(cd "$$module" && go vet ./...); \
	done

# Integration tests
test-integration:
	@echo "Running integration tests..."
	mvn verify -Pintegration-tests

# E2E tests
test-e2e:
	@echo "Running E2E tests..."
	cd tests/e2e && ./run-tests.sh

# Code formatting
format:
	@echo "Formatting code..."
	mvn spotless:apply
	@set -e; for module in $(GO_MODULES); do (cd "$$module" && go fmt ./...); done

# Code style check
check: vet-go
	@echo "Checking code style..."
	mvn spotless:check

catalog-check:
	mvn -pl cli -am package -DskipTests -DskipITs
	java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar catalog-export target/connector-inventory.pb --compiler-build 0.1.0-SNAPSHOT --execution-profile standard
	cmp deployment/catalog/connector-inventory.pb target/connector-inventory.pb

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
	mvn protobuf:compile protobuf:compile-custom
	$(MAKE) proto-go-generate

proto-go-generate:
	buf generate api/protobuf --template buf.gen.yaml

proto-lint:
	buf lint api/protobuf

# Generate CRD manifests
crd-generate:
	@echo "Generating CRD manifests..."
	cd control-plane/controller && GOTOOLCHAIN=go1.22.12 go run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) crd paths=./api/v1 output:crd:artifacts:config=../../deployment/operator/config/crd/bases

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
