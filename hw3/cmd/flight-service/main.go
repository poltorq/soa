package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	flightv1 "flight-booking-hw3/gen/flight/v1"
	"flight-booking-hw3/internal/config"
	"flight-booking-hw3/internal/flight"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.LoadFlight()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	if len(os.Args) == 2 && os.Args[1] == "-healthcheck" {
		if err := checkHealth(cfg.GRPCAddr, cfg.APIKey); err != nil {
			logger.Error("health check failed", "error", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := flight.OpenStore(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open flight store", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	cache := flight.OpenCache(
		cfg.RedisMasterName, cfg.RedisSentinelAddr, cfg.RedisPassword, cfg.CacheTTL, logger,
	)
	defer func() { _ = cache.Close() }()
	if err := cache.Ping(ctx); err != nil {
		logger.Error("open flight cache", "error", err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen gRPC", "error", err)
		os.Exit(1)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(flight.APIKeyUnaryServerInterceptor(cfg.APIKey)))
	flightv1.RegisterFlightServiceServer(server, flight.NewService(store, cache, logger))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("flight.v1.FlightService", healthv1.HealthCheckResponse_SERVING)
	healthv1.RegisterHealthServer(server, healthServer)

	go func() {
		<-ctx.Done()
		done := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			server.Stop()
		}
	}()

	logger.Info("flight gRPC service is listening", "address", cfg.GRPCAddr)
	if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		logger.Error("serve gRPC", "error", err)
		os.Exit(1)
	}
}

func checkHealth(address, apiKey string) error {
	target, err := localTarget(address)
	if err != nil {
		return err
	}
	connection, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("create health client: %w", err)
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", apiKey)
	response, err := healthv1.NewHealthClient(connection).Check(ctx, &healthv1.HealthCheckRequest{Service: "flight.v1.FlightService"})
	if err != nil {
		return fmt.Errorf("check gRPC health: %w", err)
	}
	if response.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		return fmt.Errorf("flight service is not serving")
	}
	return nil
}

func localTarget(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parse gRPC address: %w", err)
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}
