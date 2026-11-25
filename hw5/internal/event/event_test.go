package event

import (
	"strings"
	"testing"
	"time"
)

func TestEventValidate(t *testing.T) {
	progress := int32(10)
	valid := Event{
		EventID:         "11111111-1111-4111-8111-111111111111",
		UserID:          "user-1",
		MovieID:         "movie-1",
		EventType:       ViewFinished,
		Timestamp:       time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		DeviceType:      Mobile,
		SessionID:       "session-1",
		ProgressSeconds: &progress,
	}
	tests := []struct {
		name    string
		change  func(*Event)
		wantErr string
	}{
		{name: "valid event"},
		{name: "invalid UUID", change: func(item *Event) { item.EventID = "invalid" }, wantErr: "UUID"},
		{name: "timestamp UTC outside storage range", change: func(item *Event) { item.Timestamp = time.Date(1970, 1, 1, 0, 0, 0, 0, time.FixedZone("UTC+3", 3*3600)) }, wantErr: "timestamp"},
		{name: "timestamp outside storage range", change: func(item *Event) { item.Timestamp = time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC) }, wantErr: "timestamp"},
		{name: "whitespace user", change: func(item *Event) { item.UserID = "   " }, wantErr: "user_id"},
		{name: "oversized identifier", change: func(item *Event) { item.UserID = strings.Repeat("x", 1025) }, wantErr: "1024"},
		{name: "missing user", change: func(item *Event) { item.UserID = "" }, wantErr: "user_id"},
		{name: "unknown type", change: func(item *Event) { item.EventType = "SKIPPED" }, wantErr: "event_type"},
		{name: "unknown device", change: func(item *Event) { item.DeviceType = "WATCH" }, wantErr: "device_type"},
		{name: "missing viewing progress", change: func(item *Event) { item.ProgressSeconds = nil }, wantErr: "progress_seconds"},
		{name: "negative progress", change: func(item *Event) { value := int32(-1); item.ProgressSeconds = &value }, wantErr: "negative"},
		{name: "like without progress", change: func(item *Event) { item.EventType = Liked; item.ProgressSeconds = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := valid
			if test.change != nil {
				test.change(&item)
			}
			err := item.Validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.FixedZone("test", 3*60*60))
	item := Event{
		UserID:     " user-1 ",
		MovieID:    " movie-1 ",
		EventType:  "view_started",
		DeviceType: "mobile",
		SessionID:  " session-1 ",
	}
	item.SetDefaults(now)
	if item.EventID == "" {
		t.Fatal("SetDefaults() did not generate event ID")
	}
	if item.Timestamp.Location() != time.UTC || !item.Timestamp.Equal(now) {
		t.Fatalf("SetDefaults() timestamp = %v", item.Timestamp)
	}
	if item.EventType != ViewStarted || item.DeviceType != Mobile {
		t.Fatalf("SetDefaults() type/device = %q/%q", item.EventType, item.DeviceType)
	}
	if item.UserID != "user-1" || item.MovieID != "movie-1" || item.SessionID != "session-1" {
		t.Fatalf("SetDefaults() did not trim identifiers: %+v", item)
	}
}
