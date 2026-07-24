package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	address := flag.String("addr", "127.0.0.1:3000", "gRPC server address")
	service := flag.String("service", "readiness", "gRPC health service name")
	timeout := flag.Duration("timeout", 3*time.Second, "health check timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	conn, err := grpc.NewClient(*address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fail(err)
	}
	defer conn.Close()

	response, err := healthv1.NewHealthClient(conn).Check(ctx, &healthv1.HealthCheckRequest{Service: *service})
	if err != nil {
		fail(err)
	}
	if response.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		fail(fmt.Errorf("service status is %s", response.GetStatus()))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
