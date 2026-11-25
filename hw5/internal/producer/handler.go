package producer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"online-cinema-hw5/internal/event"
)

const maxRequestBytes = 1 << 20

type eventPublisher interface {
	Publish(context.Context, event.Event) error
}

type handlerPublisher interface {
	eventPublisher
	Ready(context.Context) error
}

type Handler struct {
	publisher handlerPublisher
	logger    *slog.Logger
	now       func() time.Time
}

func NewHandler(publisher handlerPublisher, logger *slog.Logger) http.Handler {
	handler := &Handler{publisher: publisher, logger: logger, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("POST /events", handler.createEvent)
	return mux
}

func (h *Handler) health(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), time.Second)
	defer cancel()
	if err := h.publisher.Ready(ctx); err != nil {
		h.logger.Error("readiness check", "error", err)
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok", "service": "movie-service"})
}

func (h *Handler) createEvent(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 12*time.Second)
	defer cancel()
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var item event.Event
	if err := decoder.Decode(&item); err != nil {
		writeError(response, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}
	item.SetDefaults(h.now())
	if err := item.Validate(); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.publisher.Publish(ctx, item); err != nil {
		h.logger.Error("publish event", "event_id", item.EventID, "error", err)
		writeError(response, http.StatusServiceUnavailable, "event broker is unavailable")
		return
	}
	writeJSON(response, http.StatusCreated, map[string]string{"event_id": item.EventID})
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
