module io.astrasync/console

go 1.22

require (
	github.com/google/uuid v1.6.0
	google.golang.org/grpc v1.64.0
	google.golang.org/protobuf v1.34.2
	io.astrasync/control-plane/api-server v0.0.0
	io.astrasync/control-plane/auth v0.0.0
	io.astrasync/control-plane/observability v0.0.0-00010101000000-000000000000
)

require (
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.7.2 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240513163218-0867130af1f8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240513163218-0867130af1f8 // indirect
)

replace io.astrasync/control-plane/api-server => ../control-plane/api-server

replace io.astrasync/control-plane/auth => ../control-plane/auth

replace io.astrasync/control-plane/observability => ../control-plane/observability
