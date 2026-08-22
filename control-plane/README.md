# AstraSync Go Modules

This directory contains the Go modules for the AstraSync control plane.

## Modules

- `control-plane/api-server` - REST API server
- `control-plane/controller` - Job controller
- `control-plane/scheduler` - Resource scheduler
- `control-plane/catalog` - Data catalog service
- `control-plane/auth` - Authentication service

## Prerequisites

- Go 1.26+
- protoc 26.0+
- Buf CLI 1.36+

## Building

Each module can be built independently:

```bash
cd control-plane/<module>
go mod download
go build ./...
```

Or build all modules:

```bash
make build-go
```

## Development

Run tests:

```bash
go test ./...
```

Format code:

```bash
go fmt ./...
```

Lint code:

```bash
golangci-lint run
```
