module io.astrasync/console

go 1.22

require (
	google.golang.org/grpc v1.64.0
	google.golang.org/protobuf v1.34.2
	io.astrasync/control-plane/api-server v0.0.0
)

require (
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240513163218-0867130af1f8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240513163218-0867130af1f8 // indirect
)

replace io.astrasync/control-plane/api-server => ../control-plane/api-server
