package main

import (
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "io.astrasync/control-plane/api-server/gen/go/v1"
)

func main() {
	fmt.Println("AstraSync API Server")
	fmt.Println("Version: 0.1.0-SNAPSHOT")

	// Start gRPC server
	go func() {
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}

		s := grpc.NewServer()
		pb.RegisterJobServiceServer(s, &jobServer{})
		pb.RegisterConnectionServiceServer(s, &connectionServer{})

		reflection.Register(s)
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// Start REST gateway
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}
	err := pb.RegisterJobServiceHandlerFromEndpoint(nil, mux, ":50051", opts)
	if err != nil {
		log.Fatalf("failed to register gateway: %v", err)
	}

	fmt.Println("REST Gateway listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

type jobServer struct {
	pb.UnimplementedJobServiceServer
}

type connectionServer struct {
	pb.UnimplementedConnectionServiceServer
}
