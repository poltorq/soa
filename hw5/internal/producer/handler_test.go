package producer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"online-cinema-hw5/internal/event"
)

type publisherStub struct {
	published []event.Event
	err       error
	readyErr  error
}

func (p *publisherStub) Publish(_ context.Context, item event.Event) error {
	p.published = append(p.published, item)
	return p.err
}

func (p *publisherStub) Ready(_ context.Context) error {
	return p.readyErr
}

func TestHealthReflectsKafkaReadiness(t *testing.T) {
	tests := []struct {
		name       string
		readyErr   error
		wantStatus int
	}{
		{name: "ready", wantStatus: http.StatusOK},
		{name: "unavailable", readyErr: errors.New("Kafka unavailable"), wantStatus: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher := &publisherStub{readyErr: test.readyErr}
			handler := NewHandler(publisher, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestCreateEvent(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		publishErr error
		wantStatus int
		wantCount  int
	}{
		{
			name:       "valid event",
			body:       `{"user_id":"user-1","movie_id":"movie-1","event_type":"VIEW_STARTED","device_type":"MOBILE","session_id":"session-1","progress_seconds":0}`,
			wantStatus: http.StatusCreated,
			wantCount:  1,
		},
		{
			name:       "invalid JSON",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown field",
			body:       `{"unknown":true}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing progress",
			body:       `{"user_id":"user-1","movie_id":"movie-1","event_type":"VIEW_STARTED","device_type":"MOBILE","session_id":"session-1"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "broker failure",
			body:       `{"user_id":"user-1","movie_id":"movie-1","event_type":"LIKED","device_type":"TV","session_id":"session-1"}`,
			publishErr: errors.New("unavailable"),
			wantStatus: http.StatusServiceUnavailable,
			wantCount:  1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher := &publisherStub{err: test.publishErr}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := NewHandler(publisher, logger)
			request := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if len(publisher.published) != test.wantCount {
				t.Fatalf("published = %d, want %d", len(publisher.published), test.wantCount)
			}
			if test.wantCount == 1 && publisher.published[0].Timestamp.Location() != time.UTC {
				t.Fatalf("timestamp location = %v, want UTC", publisher.published[0].Timestamp.Location())
			}
		})
	}
}

func TestCreateEventRejectsMultipleObjects(t *testing.T) {
	publisher := &publisherStub{}
	handler := NewHandler(publisher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(`{} {}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
