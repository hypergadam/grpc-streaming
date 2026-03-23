package main

import (
	"context"
	"log"
	"net"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	wearablepb "github.com/hypergadam/grpc-streaming/pkg/api/wearable/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Open a TCP socket on port 9879
	lis, err := net.Listen("tcp", ":9879")
	if err != nil {
		log.Fatalf("failed to listen: %v", err.Error())
	}

	// Create a new gRPC server
	grpcServer := grpc.NewServer()

	// Register the WearableService implementation.
	wearablepb.RegisterWearableServiceServer(grpcServer, &wearableServer{})

	// Register reflection - lets grpcurl discover your services.
	reflection.Register(grpcServer)

	// Create the HTTP gateway mux
	ctx := context.Background()
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	// Tell the gateway to forward requests to the gRPC server.
	if err := wearablepb.RegisterWearableServiceHandlerFromEndpoint(
		ctx, mux, "localhost:9879", opts,
	); err != nil {
		log.Fatalf("failed to register gateway: %v", err.Error())
	}

	// Run gRPC server in background goroutine.
	go func() {
		log.Println("Starting gRPC server on port 9879...")
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err.Error())
		}
	}()

	// Run HTTP gateway in foreground.
	go func() {
		log.Println("Starting HTTP gateway on port 8080...")
		if err := http.ListenAndServe(":8080", mux); err != nil {
			log.Fatalf("failed to serve: %v", err.Error())
		}
	}()

}

// NOTE:  ▎ grpc.NewServer() creates the gRPC server but doesn't start it — Serve(lis) starts it. RegisterWearableServiceServer wires your wearableServer struct to handle incoming RPCs.
// reflection.Register lets tools like grpcurl ask the server "what RPCs do you have?" at runtime. runtime.NewServeMux() is the gateway's HTTP router — it knows about your routes from the
// option (google.api.http) annotations in the proto. RegisterWearableServiceHandlerFromEndpoint connects the gateway to the gRPC server — it internally dials localhost:9879 and forwards
// translated requests. The gRPC server runs in a goroutine so main doesn't block on it — HTTP gateway runs in the foreground. insecure.NewCredentials() means no TLS — fine for local
// development.
