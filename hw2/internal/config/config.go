package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Port           string
	DatabaseURL    string
	JWTSecret      string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	OrderRateLimit time.Duration
}

func Load() (Config, error) {
	accessTTL, err := duration("ACCESS_TOKEN_TTL", 20*time.Minute)
	if err != nil {
		return Config{}, err
	}
	refreshTTL, err := duration("REFRESH_TOKEN_TTL", 14*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if accessTTL < 15*time.Minute || accessTTL > 30*time.Minute {
		return Config{}, fmt.Errorf("ACCESS_TOKEN_TTL must be between 15m and 30m")
	}
	if refreshTTL < 7*24*time.Hour || refreshTTL > 30*24*time.Hour {
		return Config{}, fmt.Errorf("REFRESH_TOKEN_TTL must be between 168h and 720h")
	}
	rateLimit, err := duration("ORDER_RATE_LIMIT", time.Minute)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Port:           value("PORT", "8080"),
		DatabaseURL:    value("DATABASE_URL", "postgres://marketplace:marketplace@localhost:5432/marketplace?sslmode=disable"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		AccessTTL:      accessTTL,
		RefreshTTL:     refreshTTL,
		OrderRateLimit: rateLimit,
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must contain at least 32 characters")
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
