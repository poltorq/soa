package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Movie struct {
	HTTPAddr          string
	KafkaBrokers      []string
	KafkaTopic        string
	SchemaRegistryURL string
	SchemaPath        string
	PublishAttempts   int
	PublishBackoff    time.Duration
	GeneratorEnabled  bool
	GeneratorInterval time.Duration
}

type Aggregation struct {
	HTTPAddr          string
	ClickHouseDSN     string
	PostgresURL       string
	S3Endpoint        string
	S3AccessKey       string
	S3SecretKey       string
	S3Bucket          string
	ScheduleInterval  time.Duration
	ExportInterval    time.Duration
	DatabaseAttempts  int
	DatabaseBackoff   time.Duration
	AggregationWindow int
}

func LoadMovie() (Movie, error) {
	attempts, err := integer("KAFKA_PUBLISH_ATTEMPTS", 3)
	if err != nil {
		return Movie{}, err
	}
	backoff, err := duration("KAFKA_PUBLISH_BACKOFF", time.Second)
	if err != nil {
		return Movie{}, err
	}
	generatorInterval, err := duration("GENERATOR_INTERVAL", 5*time.Second)
	if err != nil {
		return Movie{}, err
	}
	generatorEnabled, err := boolean("GENERATOR_ENABLED", false)
	if err != nil {
		return Movie{}, err
	}

	config := Movie{
		HTTPAddr:          value("HTTP_ADDR", ":8080"),
		KafkaBrokers:      list("KAFKA_BROKERS", "localhost:9092"),
		KafkaTopic:        value("KAFKA_TOPIC", "movie-events"),
		SchemaRegistryURL: strings.TrimRight(value("SCHEMA_REGISTRY_URL", "http://localhost:8082"), "/"),
		SchemaPath:        value("EVENT_SCHEMA_PATH", "schema/movie_event.avsc"),
		PublishAttempts:   attempts,
		PublishBackoff:    backoff,
		GeneratorEnabled:  generatorEnabled,
		GeneratorInterval: generatorInterval,
	}
	if len(config.KafkaBrokers) == 0 {
		return Movie{}, fmt.Errorf("KAFKA_BROKERS must contain at least one broker")
	}
	if attempts < 1 || attempts > 10 {
		return Movie{}, fmt.Errorf("KAFKA_PUBLISH_ATTEMPTS must be between 1 and 10")
	}
	return config, nil
}

func LoadAggregation() (Aggregation, error) {
	interval, err := duration("AGGREGATION_INTERVAL", time.Minute)
	if err != nil {
		return Aggregation{}, err
	}
	exportInterval, err := duration("EXPORT_INTERVAL", 24*time.Hour)
	if err != nil {
		return Aggregation{}, err
	}
	attempts, err := integer("DATABASE_WRITE_ATTEMPTS", 3)
	if err != nil {
		return Aggregation{}, err
	}
	backoff, err := duration("DATABASE_WRITE_BACKOFF", time.Second)
	if err != nil {
		return Aggregation{}, err
	}
	window, err := integer("AGGREGATION_WINDOW_DAYS", 9)
	if err != nil {
		return Aggregation{}, err
	}
	config := Aggregation{
		HTTPAddr:          value("HTTP_ADDR", ":8081"),
		ClickHouseDSN:     value("CLICKHOUSE_DSN", "clickhouse://aggregator:aggregate-local-secret@localhost:9002/movie_analytics"),
		PostgresURL:       value("POSTGRES_URL", "postgres://metrics_writer:metrics-local-secret@localhost:5435/analytics?sslmode=disable"),
		S3Endpoint:        value("S3_ENDPOINT", "localhost:4566"),
		S3AccessKey:       value("S3_ACCESS_KEY", "analytics"),
		S3SecretKey:       value("S3_SECRET_KEY", "analytics-local-secret"),
		S3Bucket:          value("S3_BUCKET", "movie-analytics"),
		ScheduleInterval:  interval,
		ExportInterval:    exportInterval,
		DatabaseAttempts:  attempts,
		DatabaseBackoff:   backoff,
		AggregationWindow: window,
	}
	if attempts < 1 || attempts > 10 || window < 9 || window > 31 {
		return Aggregation{}, fmt.Errorf("attempts must be between 1 and 10 and aggregation window between 9 and 31")
	}
	return config, nil
}

func value(name, fallback string) string {
	if raw := strings.TrimSpace(os.Getenv(name)); raw != "" {
		return raw
	}
	return fallback
}

func list(name, fallback string) []string {
	raw := value(name, fallback)
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func integer(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}

func boolean(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}
