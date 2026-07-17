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

	"connectrpc.com/connect"
	"github.com/wundergraph/cosmo/demo/pkg/subgraphs/projects/src/service"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const (
	address = ":4011"
)

func recoveryInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, request connect.AnyRequest) (response connect.AnyResponse, err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("Recovered from panic: %v", recovered)
					err = connect.NewError(connect.CodeInternal, errors.New("internal server error"))
				}
			}()
			return next(ctx, request)
		}
	}
}

func loggingInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			response, err := next(ctx, request)
			log.Printf("Method: %s, Duration: %s, Error: %v", request.Spec().Procedure, time.Since(start), err)
			return response, err
		}
	}
}

func main() {
	handler, err := service.NewConnectHandler(
		&service.ProjectsService{},
		connect.WithInterceptors(recoveryInterceptor(), loggingInterceptor()),
	)
	if err != nil {
		log.Fatalf("failed to create projects handler: %v", err)
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
	log.Println("Server stopped")
}
