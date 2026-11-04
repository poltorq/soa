package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Flight struct {
	GRPCAddr          string
	DatabaseURL       string
	APIKey            string
	RedisMasterName   string
	RedisSentinelAddr []string
	RedisPassword     string
	CacheTTL          time.Duration
}

type Booking struct {
	HTTPAddr                string
	DatabaseURL             string
	FlightGRPCAddr          string
	APIKey                  string
	GRPCTimeout             time.Duration
	GRPCMaxAttempts         int
	GRPCRetryBackoff        time.Duration
	BreakerFailureThreshold int
	BreakerWindowSize       int
	BreakerOpenTimeout      time.Duration
}

func LoadFlight() (Flight, error) {
	cacheTTL, err := duration("CACHE_TTL", 5*time.Minute)
	if err != nil {
		return Flight{}, err
	}
	cfg := Flight{
		GRPCAddr:          value("GRPC_ADDR", ":9090"),
		DatabaseURL:       value("DATABASE_URL", "postgres://flight:flight@localhost:5434/flight?sslmode=disable"),
		APIKey:            os.Getenv("GRPC_API_KEY"),
		RedisMasterName:   value("REDIS_MASTER_NAME", "flight-cache"),
		RedisSentinelAddr: split(value("REDIS_SENTINEL_ADDRS", "localhost:26379")),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		CacheTTL:          cacheTTL,
	}
	if err := validateSecret("GRPC_API_KEY", cfg.APIKey); err != nil {
		return Flight{}, err
	}
	if len(cfg.RedisSentinelAddr) == 0 {
		return Flight{}, fmt.Errorf("REDIS_SENTINEL_ADDRS must not be empty")
	}
	return cfg, nil
}

func LoadBooking() (Booking, error) {
	timeout, err := duration("GRPC_TIMEOUT", 2*time.Second)
	if err != nil {
		return Booking{}, err
	}
	backoff, err := duration("GRPC_RETRY_BACKOFF", 100*time.Millisecond)
	if err != nil {
		return Booking{}, err
	}
	openTimeout, err := duration("BREAKER_OPEN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Booking{}, err
	}
	attempts, err := integer("GRPC_MAX_ATTEMPTS", 3)
	if err != nil {
		return Booking{}, err
	}
	threshold, err := integer("BREAKER_FAILURE_THRESHOLD", 5)
	if err != nil {
		return Booking{}, err
	}
	windowSize, err := integer("BREAKER_WINDOW_SIZE", 10)
	if err != nil {
		return Booking{}, err
	}
	if attempts < 1 || attempts > 3 {
		return Booking{}, fmt.Errorf("GRPC_MAX_ATTEMPTS must be between 1 and 3")
	}
	if threshold < 1 {
		return Booking{}, fmt.Errorf("BREAKER_FAILURE_THRESHOLD must be positive")
	}
	if windowSize < threshold {
		return Booking{}, fmt.Errorf("BREAKER_WINDOW_SIZE must be at least BREAKER_FAILURE_THRESHOLD")
	}
	cfg := Booking{
		HTTPAddr:                value("HTTP_ADDR", ":8080"),
		DatabaseURL:             value("DATABASE_URL", "postgres://booking:booking@localhost:5433/booking?sslmode=disable"),
		FlightGRPCAddr:          value("FLIGHT_GRPC_ADDR", "localhost:9090"),
		APIKey:                  os.Getenv("GRPC_API_KEY"),
		GRPCTimeout:             timeout,
		GRPCMaxAttempts:         attempts,
		GRPCRetryBackoff:        backoff,
		BreakerFailureThreshold: threshold,
		BreakerWindowSize:       windowSize,
		BreakerOpenTimeout:      openTimeout,
	}
	if err := validateSecret("GRPC_API_KEY", cfg.APIKey); err != nil {
		return Booking{}, err
	}
	return cfg, nil
}

func value(name, fallback string) string {
	if result := os.Getenv(name); result != "" {
		return result
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	result, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if result <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return result, nil
}

func integer(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	result, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return result, nil
}

func split(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func validateSecret(name, secret string) error {
	if len(secret) < 16 {
		return fmt.Errorf("%s must contain at least 16 characters", name)
	}
	return nil
}
