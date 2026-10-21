package config

import (
	"strings"
	"testing"
)

func TestTokenTTLRanges(t *testing.T) {
	for _, test := range []struct {
		access, refresh, invalid string
	}{
		{"15m", "168h", ""}, {"30m", "720h", ""}, {"20m", "336h", ""},
		{"14m59s", "336h", "ACCESS_TOKEN_TTL"}, {"30m1s", "336h", "ACCESS_TOKEN_TTL"},
		{"20m", "167h", "REFRESH_TOKEN_TTL"}, {"20m", "721h", "REFRESH_TOKEN_TTL"},
	} {
		t.Run(test.access+"/"+test.refresh, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "configuration-test-secret-long-enough")
			t.Setenv("ACCESS_TOKEN_TTL", test.access)
			t.Setenv("REFRESH_TOKEN_TTL", test.refresh)
			t.Setenv("ORDER_RATE_LIMIT", "1m")
			_, err := Load()
			if test.invalid == "" && err != nil {
				t.Fatal(err)
			}
			if test.invalid != "" && (err == nil || !strings.Contains(err.Error(), test.invalid)) {
				t.Fatalf("expected %s validation error, got %v", test.invalid, err)
			}
		})
	}
}
