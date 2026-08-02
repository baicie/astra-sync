# Makefile for AstraSync

.PHONY: all build build-java build-go build-connectors test test-java test-go clean install format check verify docker-build docker-push proto-generate crd-generate install-hooks

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
	cd control-plane/api-server && go build -o bin/api-server ./cmd/server
	cd control-plane/controller && go build -o bin/controller ./cmd/controller
	cd control-plane/scheduler && go build -o bin/scheduler ./cmd/scheduler

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
	cd control-plane && go test ./...

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
	cd control-plane && go fmt ./...

# Code style check
check:
	@echo "Checking code style..."
	mvn spotless:check
	cd control-plane && go vet ./...

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
	docker build -t astrasync/controller:latest -f deployment/docker/Dockerfile.controller .

# Push Docker images
docker-push:
	@echo "Pushing Docker images..."
	docker push astrasync/worker:latest
	docker push astrasync/api-server:latest
	docker push astrasync/controller:latest

# Generate protobuf
proto-generate:
	@echo "Generating protobuf code..."
	mvn protobuf:compile protobuf:compile-custom

# Generate CRD manifests
crd-generate:
	@echo "Generating CRD manifests..."
	cd deployment/operator && go generate ./...

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
