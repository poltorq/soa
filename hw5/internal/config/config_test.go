package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadMovie(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "kafka-1:9092, kafka-2:9092")
	t.Setenv("GENERATOR_ENABLED", "true")
	t.Setenv("GENERATOR_INTERVAL", "2s")
	config, err := LoadMovie()
	if err != nil {
		t.Fatalf("LoadMovie() error = %v", err)
	}
	if len(config.KafkaBrokers) != 2 || config.KafkaBrokers[1] != "kafka-2:9092" {
		t.Fatalf("KafkaBrokers = %#v", config.KafkaBrokers)
	}
	if !config.GeneratorEnabled || config.GeneratorInterval != 2*time.Second {
		t.Fatalf("generator config = %v/%v", config.GeneratorEnabled, config.GeneratorInterval)
	}
}

func TestLoadAggregationRejectsInvalidValues(t *testing.T) {
	t.Setenv("AGGREGATION_INTERVAL", "never")
	_, err := LoadAggregation()
	if err == nil || !strings.Contains(err.Error(), "AGGREGATION_INTERVAL") {
		t.Fatalf("LoadAggregation() error = %v", err)
	}
}

func TestAggregationWindowIncludesD7(t *testing.T) {
	t.Setenv("AGGREGATION_WINDOW_DAYS", "8")
	if _, err := LoadAggregation(); err == nil {
		t.Fatal("accepted a window that misses D7")
	}
}
func TestMovieRejectsEmptyBrokerList(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", ", ,")
	if _, err := LoadMovie(); err == nil {
		t.Fatal("accepted empty broker list")
	}
}
