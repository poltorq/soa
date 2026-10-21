package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/auth"

	"github.com/google/uuid"
)

func TestMaskedBody(t *testing.T) {
	got := maskedBody([]byte(`{"email":"user@example.com","password":"secret","nested":{"refresh_token":"token"}}`))
	want := map[string]any{
		"email":    "user@example.com",
		"password": "***",
		"nested":   map[string]any{"refresh_token": "***"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("maskedBody() = %#v, want %#v", got, want)
	}
}

func TestRequestLogsContainContractFields(t *testing.T) {
	tokens := auth.NewTokenManager("logging-test-secret-long-enough-123", 20*time.Minute, 14*24*time.Hour)
	userID := uuid.New()
	pair, err := tokens.Issue(auth.Identity{UserID: userID, Role: "USER"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path, body, authorization string
		status                          int
		panic                           bool
	}{
		{"authorized", "/orders", `{"password":"secret","nested":{"refresh_token":"private-token"}}`, "bEaReR " + pair.AccessToken, 201, false},
		{"anonymous", "/auth/login", `{"password":"secret"}`, "", 201, false},
		{"panic", "/orders", `{}`, "Bearer " + pair.AccessToken, 500, true},
		{"too large", "/orders", strings.Repeat("x", 129), "Bearer " + pair.AccessToken, 400, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if test.panic {
					panic("deliberate test panic")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != test.body {
					t.Errorf("body was not preserved: %q, %v", body, err)
				}
				w.WriteHeader(201)
				w.WriteHeader(409) // A second header must not corrupt the logged status.
			})
			handler := maxBodyBytes(128)(requestLogMiddlewareWithLogger(logger)(authMiddleware(tokens)(next)))
			r := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
			r.Header.Set("Authorization", test.authorization)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if test.name == "too large" && called {
				t.Fatal("oversized body reached handler")
			}
			if test.status >= 400 {
				var body api.ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if test.panic && body.ErrorCode != api.INTERNALERROR {
					t.Fatalf("unexpected panic response: %+v", body)
				}
			}
			var entry map[string]any
			count := 0
			decoder := json.NewDecoder(&output)
			for {
				var line map[string]any
				if err := decoder.Decode(&line); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if line["msg"] == "api_request" {
					entry = line
					count++
				}
			}
			if count != 1 {
				t.Fatalf("expected one request log, got %d", count)
			}
			for _, field := range []string{"request_id", "method", "endpoint", "status_code", "duration_ms", "user_id", "timestamp", "request_body"} {
				if _, ok := entry[field]; !ok {
					t.Errorf("missing log field %s", field)
				}
			}
			if entry["request_id"] != w.Header().Get("X-Request-Id") {
				t.Fatal("request ID differs from response")
			}
			if _, err := uuid.Parse(entry["request_id"].(string)); err != nil {
				t.Fatal(err)
			}
			if _, err := time.Parse(time.RFC3339Nano, entry["timestamp"].(string)); err != nil {
				t.Fatal(err)
			}
			if entry["status_code"] != float64(test.status) || entry["method"] != "POST" || entry["endpoint"] != test.path {
				t.Fatalf("wrong request metadata: %+v", entry)
			}
			if test.name == "authorized" && entry["user_id"] != userID.String() {
				t.Fatal("authorized user ID missing")
			}
			if test.name == "anonymous" && entry["user_id"] != nil {
				t.Fatal("anonymous user ID must be null")
			}
			loggedBody, _ := json.Marshal(entry["request_body"])
			if strings.Contains(string(loggedBody), "secret") || strings.Contains(string(loggedBody), "private-token") {
				t.Fatal("sensitive data leaked into log")
			}
		})
	}
}

func TestMaskedBodyEmpty(t *testing.T) {
	if got := maskedBody(nil); got != nil {
		t.Fatalf("maskedBody(nil) = %#v, want nil", got)
	}
}
