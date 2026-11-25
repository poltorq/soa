package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"online-cinema-hw5/internal/config"
	"online-cinema-hw5/internal/httpserver"
	"online-cinema-hw5/internal/producer"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	healthcheck := flag.Bool("healthcheck", false, "check the local HTTP endpoint")
	flag.Parse()
	cfg, err := config.LoadMovie()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if *healthcheck {
		return httpserver.CheckHealth(cfg.HTTPAddr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	startupCtx, cancelStartup := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStartup()
	publisher, schemaID, err := producer.ConnectPublisher(
		startupCtx,
		cfg.KafkaBrokers,
		cfg.KafkaTopic,
		cfg.SchemaRegistryURL,
		cfg.SchemaPath,
		cfg.PublishAttempts,
		cfg.PublishBackoff,
		logger,
	)
	if err != nil {
		return fmt.Errorf("initialize Kafka publisher: %w", err)
	}
	defer publisher.Close()
	logger.Info("event schema registered", "topic", cfg.KafkaTopic, "schema_id", schemaID)

	var workers []func(context.Context)
	if cfg.GeneratorEnabled {
		generator := producer.NewGenerator(publisher, cfg.GeneratorInterval, logger)
		workers = append(workers, generator.Run)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           producer.NewHandler(publisher, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("movie-service started", "address", cfg.HTTPAddr)
	return httpserver.Run(ctx, server, logger, workers...)
}
