package producer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hamba/avro/v2"

	"online-cinema-hw5/internal/event"
)

func TestEventMatchesAvroSchema(t *testing.T) {
	schemaText, err := os.ReadFile("../../schema/movie_event.avsc")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	schema, err := avro.Parse(string(schemaText))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	progress := int32(120)
	item := event.Event{
		EventID:         "11111111-1111-4111-8111-111111111111",
		UserID:          "user-1",
		MovieID:         "movie-1",
		EventType:       event.ViewFinished,
		Timestamp:       time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		DeviceType:      event.TV,
		SessionID:       "session-1",
		ProgressSeconds: &progress,
	}
	payload, err := avro.Marshal(schema, item)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("marshal event returned an empty payload")
	}
}

func TestSchemaRegistrationSetsCompatibilityAndChecksID(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
		status   int
		wantErr  bool
	}{
		{"registered", `{"id":42}`, 200, false},
		{"invalid ID", `{"id":0}`, 200, true},
		{"incompatible", `{"error_code":409}`, 409, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var configured atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut && r.URL.Path == "/config/movie-events-value" {
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					configured.Store(body["compatibility"] == "BACKWARD_TRANSITIVE")
					w.Write([]byte(`{}`))
					return
				}
				if !configured.Load() || r.Method != http.MethodPost || r.URL.Path != "/subjects/movie-events-value/versions" {
					t.Errorf("unexpected registration: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				w.Write([]byte(test.response))
			}))
			defer server.Close()
			codec, err := loadSchema(context.Background(), server.URL, "movie-events", "../../schema/movie_event.avsc")
			if (err != nil) != test.wantErr {
				t.Fatalf("codec=%+v err=%v", codec, err)
			}
			if err == nil && codec.id != 42 {
				t.Fatalf("id=%d", codec.id)
			}
		})
	}
}
