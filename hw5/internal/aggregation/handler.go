package aggregation

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) http.Handler {
	handler := &Handler{service: service, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("POST /aggregate", handler.aggregate)
	mux.HandleFunc("POST /export", handler.export)
	return mux
}

func (h *Handler) health(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), time.Second)
	defer cancel()
	if err := h.service.Ready(ctx); err != nil {
		h.logger.Error("readiness check", "error", err)
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok", "service": "aggregation-service"})
}

func (h *Handler) aggregate(response http.ResponseWriter, request *http.Request) {
	date, ok := requestDate(response, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 25*time.Second)
	defer cancel()
	if err := h.service.Recalculate(ctx, date); err != nil {
		h.logger.Error("manual aggregation", "date", date, "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "aggregation failed"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "completed", "date": date.Format(time.DateOnly)})
}

func (h *Handler) export(response http.ResponseWriter, request *http.Request) {
	date, ok := requestDate(response, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 25*time.Second)
	defer cancel()
	location, err := h.service.Export(ctx, date)
	if err != nil {
		h.logger.Error("manual export", "date", date, "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "export failed"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "completed", "location": location})
}

func requestDate(response http.ResponseWriter, request *http.Request) (time.Time, bool) {
	raw := request.URL.Query().Get("date")
	date, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "date must use YYYY-MM-DD format"})
		return time.Time{}, false
	}
	if date.Year() < 1970 || date.Year() >= 2100 || date.After(day(time.Now())) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "date must be between 1970-01-01 and today (UTC)"})
		return time.Time{}, false
	}
	return date.UTC(), true
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
