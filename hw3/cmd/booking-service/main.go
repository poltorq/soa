package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	flightv1 "flight-booking-hw3/gen/flight/v1"
	"flight-booking-hw3/internal/booking"
	"flight-booking-hw3/internal/config"
	"flight-booking-hw3/internal/resilience"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.LoadBooking()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	if len(os.Args) == 2 && os.Args[1] == "-healthcheck" {
		if err := checkHealth(cfg.HTTPAddr); err != nil {
			logger.Error("health check failed", "error", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := booking.OpenStore(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open booking store", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	breaker := resilience.NewBreaker(
		cfg.BreakerFailureThreshold, cfg.BreakerWindowSize, cfg.BreakerOpenTimeout, logger,
	)
	connection, err := grpc.NewClient(
		cfg.FlightGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			booking.APIKeyUnaryClientInterceptor(cfg.APIKey),
			breaker.UnaryClientInterceptor(),
			resilience.RetryUnaryClientInterceptor(cfg.GRPCMaxAttempts, cfg.GRPCRetryBackoff, cfg.GRPCTimeout),
		),
	)
	if err != nil {
		logger.Error("create flight gRPC client", "error", err)
		os.Exit(1)
	}
	defer connection.Close()

	flights := booking.NewFlightClient(flightv1.NewFlightServiceClient(connection))
	service := booking.NewService(store, flights, logger)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           booking.NewServer(service, logger).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown HTTP server", "error", err)
		}
	}()

	logger.Info("booking HTTP service is listening", "address", cfg.HTTPAddr)
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("serve HTTP", "error", err)
		os.Exit(1)
	}
}

func checkHealth(address string) error {
	url := "http://127.0.0.1" + address + "/health"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("perform health request: %w", err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return fmt.Errorf("read health response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected health status: %s", response.Status)
	}
	return nil
}
