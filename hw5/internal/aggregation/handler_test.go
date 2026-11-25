package aggregation

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestDate(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{name: "valid", raw: "2026-04-01", ok: true},
		{name: "missing", raw: "", ok: false},
		{name: "wrong format", raw: "01.04.2026", ok: false},
		{name: "impossible date", raw: "2026-02-30", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/aggregate?date="+test.raw, nil)
			response := httptest.NewRecorder()
			date, ok := requestDate(response, request)
			if ok != test.ok {
				t.Fatalf("requestDate() ok = %v, want %v", ok, test.ok)
			}
			if ok && date.Format(time.DateOnly) != test.raw {
				t.Fatalf("requestDate() = %v", date)
			}
		})
	}
}

func TestDayUsesUTC(t *testing.T) {
	value := time.Date(2026, 4, 2, 1, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	got := day(value)
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("day() = %v, want %v", got, want)
	}
}
