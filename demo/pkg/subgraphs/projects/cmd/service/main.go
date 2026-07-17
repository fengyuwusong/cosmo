// This file spawns the projects service as a standalone subgraph. The H2C
// endpoint accepts Connect, gRPC, and gRPC-Web on the same port.

package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/vanguard/vanguardgrpc"
	projects "github.com/wundergraph/cosmo/demo/pkg/subgraphs/projects/generated"
	"github.com/wundergraph/cosmo/demo/pkg/subgraphs/projects/src/service"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	address = ":4011"
)

func recoveryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response interface{}, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("Recovered from panic: %v", recovered)
			response = nil
			err = status.Error(codes.Internal, "internal server error")
		}
	}()

	return handler(ctx, req)
}

func loggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	response, err := handler(ctx, req)
	log.Printf("Method: %s, Duration: %s, Error: %v", info.FullMethod, time.Since(start), err)
	return response, err
}

func errorInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	response, err := handler(ctx, req)
	if err == nil {
		return response, nil
	}
	if _, ok := status.FromError(err); ok {
		return response, err
	}
	return response, status.Errorf(codes.Internal, "internal server error: %v", err)
}

func main() {
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			recoveryInterceptor,
			loggingInterceptor,
			errorInterceptor,
		),
	)
	projects.RegisterProjectsServiceServer(grpcServer, &service.ProjectsService{})

	handler, err := vanguardgrpc.NewTranscoder(grpcServer)
	if err != nil {
		log.Fatalf("failed to create projects transcoder: %v", err)
	}

	server := &http.Server{
		Addr:    address,
		Handler: h2c.NewHandler(handler, &http2.Server{}),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Starting projects subgraph on %s (Connect, gRPC, gRPC-Web over H2C)", address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()

	log.Println("Shutting down projects subgraph...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	grpcServer.GracefulStop()
	log.Println("Server stopped")
}
