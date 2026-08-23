module github.com/baicie/astrasync/tests/integration

go 1.26.0

require (
	go.uber.org/zap v1.27.0
	google.golang.org/grpc v1.83.0
	io.astrasync/control-plane/api-server v0.0.0
)

require (
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
)

replace io.astrasync/control-plane/api-server => ../../control-plane/api-server
