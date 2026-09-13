module io.astrasync/tests/cross-module/chain-tenant-id

go 1.26.0

require (
	github.com/google/uuid v1.6.0
	google.golang.org/grpc v1.83.0
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
	io.astrasync/console v0.0.0
	io.astrasync/control-plane/api-server v0.0.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	io.astrasync/control-plane/auth v0.0.0 // indirect
	io.astrasync/control-plane/observability v0.0.0-00010101000000-000000000000 // indirect
)

replace (
	io.astrasync/console => ../../../console
	io.astrasync/control-plane => ../../../control-plane
	io.astrasync/control-plane/api-server => ../../../control-plane/api-server
	io.astrasync/control-plane/auth => ../../../control-plane/auth
	io.astrasync/control-plane/observability => ../../../control-plane/observability
)
