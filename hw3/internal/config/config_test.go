package config

import "testing"

func TestLoadBookingRejectsTooManyAttempts(t *testing.T) {
	t.Setenv("GRPC_API_KEY", "test-service-key")
	t.Setenv("GRPC_MAX_ATTEMPTS", "4")
	if _, err := LoadBooking(); err == nil {
		t.Fatal("expected GRPC_MAX_ATTEMPTS validation error")
	}
}

func TestLoadFlightSplitsSentinelAddresses(t *testing.T) {
	t.Setenv("GRPC_API_KEY", "test-service-key")
	t.Setenv("REDIS_SENTINEL_ADDRS", "one:26379, two:26379")
	cfg, err := LoadFlight()
	if err != nil {
		t.Fatalf("load flight config: %v", err)
	}
	if len(cfg.RedisSentinelAddr) != 2 {
		t.Fatalf("expected two sentinel addresses, got %d", len(cfg.RedisSentinelAddr))
	}
}
