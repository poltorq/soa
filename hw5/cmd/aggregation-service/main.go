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

	"online-cinema-hw5/internal/aggregation"
	"online-cinema-hw5/internal/config"
	"online-cinema-hw5/internal/httpserver"
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
	cfg, err := config.LoadAggregation()
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
	clickhouse, err := aggregation.OpenClickHouse(startupCtx, cfg.ClickHouseDSN)
	if err != nil {
		return fmt.Errorf("connect ClickHouse: %w", err)
	}
	defer clickhouse.Close()
	postgres, err := aggregation.OpenPostgres(startupCtx, cfg.PostgresURL)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()
	exporter, err := aggregation.NewExporter(
		startupCtx,
		cfg.S3Endpoint,
		cfg.S3AccessKey,
		cfg.S3SecretKey,
		cfg.S3Bucket,
	)
	if err != nil {
		return fmt.Errorf("initialize S3 exporter: %w", err)
	}
	service := aggregation.NewService(
		clickhouse,
		postgres,
		exporter,
		cfg.DatabaseAttempts,
		cfg.DatabaseBackoff,
		cfg.AggregationWindow,
		logger,
	)
	workers := []func(context.Context){func(ctx context.Context) { service.RunSchedule(ctx, cfg.ScheduleInterval, cfg.ExportInterval) }}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           aggregation.NewHandler(service, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("aggregation-service started", "address", cfg.HTTPAddr)
	return httpserver.Run(ctx, server, logger, workers...)
}
