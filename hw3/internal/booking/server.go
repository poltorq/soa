package booking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	flightv1 "flight-booking-hw3/gen/flight/v1"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	service *Service
	logger  *slog.Logger
}

type errorResponse struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

type flightResponse struct {
	ID             string    `json:"id"`
	FlightNumber   string    `json:"flight_number"`
	Airline        string    `json:"airline"`
	Origin         string    `json:"origin"`
	Destination    string    `json:"destination"`
	DepartureTime  time.Time `json:"departure_time"`
	ArrivalTime    time.Time `json:"arrival_time"`
	TotalSeats     int32     `json:"total_seats"`
	AvailableSeats int32     `json:"available_seats"`
	PriceCents     int64     `json:"price_cents"`
	Status         string    `json:"status"`
}

func NewServer(service *Service, logger *slog.Logger) *Server {
	return &Server{service: service, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /flights", s.searchFlights)
	mux.HandleFunc("GET /flights/{id}", s.getFlight)
	mux.HandleFunc("POST /bookings", s.createBooking)
	mux.HandleFunc("GET /bookings", s.listBookings)
	mux.HandleFunc("GET /bookings/{id}", s.getBooking)
	mux.HandleFunc("POST /bookings/{id}/cancel", s.cancelBooking)
	return s.requestLog(s.recoverPanic(mux))
}

func (s *Server) health(response http.ResponseWriter, request *http.Request) {
	if err := s.service.Ready(request.Context()); err != nil {
		s.logger.Error("booking database health check", "error", err)
		writeError(response, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "booking database is unavailable")
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok", "service": "booking-service"})
}

func (s *Server) searchFlights(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	items, err := s.service.SearchFlights(
		request.Context(), query.Get("origin"), query.Get("destination"), query.Get("date"),
	)
	if err != nil {
		s.writeServiceError(response, err)
		return
	}
	result := make([]flightResponse, 0, len(items))
	for _, item := range items {
		result = append(result, flightView(item))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": result})
}

func (s *Server) getFlight(response http.ResponseWriter, request *http.Request) {
	item, err := s.service.GetFlight(request.Context(), request.PathValue("id"))
	if err != nil {
		s.writeServiceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, flightView(item))
}

func (s *Server) createBooking(response http.ResponseWriter, request *http.Request) {
	var body CreateBookingInput
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	item, err := s.service.CreateBooking(request.Context(), body)
	if err != nil {
		s.writeServiceError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, item)
}

func (s *Server) getBooking(response http.ResponseWriter, request *http.Request) {
	id, err := uuid.Parse(request.PathValue("id"))
	if err != nil {
		writeError(response, http.StatusBadRequest, "VALIDATION_ERROR", "booking id must be a UUID")
		return
	}
	item, err := s.service.GetBooking(request.Context(), id)
	if err != nil {
		s.writeServiceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (s *Server) listBookings(response http.ResponseWriter, request *http.Request) {
	userID, err := uuid.Parse(request.URL.Query().Get("user_id"))
	if err != nil {
		writeError(response, http.StatusBadRequest, "VALIDATION_ERROR", "user_id query parameter must be a UUID")
		return
	}
	items, err := s.service.ListBookings(request.Context(), userID)
	if err != nil {
		s.writeServiceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) cancelBooking(response http.ResponseWriter, request *http.Request) {
	id, err := uuid.Parse(request.PathValue("id"))
	if err != nil {
		writeError(response, http.StatusBadRequest, "VALIDATION_ERROR", "booking id must be a UUID")
		return
	}
	item, err := s.service.CancelBooking(request.Context(), id)
	if err != nil {
		s.writeServiceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (s *Server) writeServiceError(response http.ResponseWriter, err error) {
	var validationErr *ValidationError
	switch {
	case errors.Is(err, ErrBookingNotFound):
		writeError(response, http.StatusNotFound, "BOOKING_NOT_FOUND", err.Error())
	case errors.Is(err, ErrBookingNotConfirmed):
		writeError(response, http.StatusConflict, "BOOKING_NOT_CONFIRMED", err.Error())
	case status.Code(err) == codes.InvalidArgument:
		writeError(response, http.StatusBadRequest, "VALIDATION_ERROR", status.Convert(err).Message())
	case status.Code(err) == codes.NotFound:
		writeError(response, http.StatusNotFound, "FLIGHT_NOT_FOUND", status.Convert(err).Message())
	case status.Code(err) == codes.ResourceExhausted:
		writeError(response, http.StatusConflict, "INSUFFICIENT_SEATS", status.Convert(err).Message())
	case status.Code(err) == codes.AlreadyExists, status.Code(err) == codes.FailedPrecondition:
		writeError(response, http.StatusConflict, "RESERVATION_CONFLICT", status.Convert(err).Message())
	case status.Code(err) == codes.Unavailable:
		writeError(response, http.StatusServiceUnavailable, "FLIGHT_SERVICE_UNAVAILABLE", status.Convert(err).Message())
	case errors.Is(err, context.DeadlineExceeded), status.Code(err) == codes.DeadlineExceeded:
		writeError(response, http.StatusGatewayTimeout, "FLIGHT_SERVICE_TIMEOUT", "flight service did not respond in time")
	case status.Code(err) == codes.Unauthenticated:
		writeError(response, http.StatusBadGateway, "FLIGHT_SERVICE_AUTH_ERROR", "flight service rejected service credentials")
	case errors.As(err, &validationErr):
		writeError(response, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	default:
		s.logger.Error("handle request", "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(response, request)
		s.logger.Info("HTTP request", "method", request.Method, "path", request.URL.Path, "duration", time.Since(started))
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic in HTTP handler", "panic", recovered)
				writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain exactly one JSON object")
	}
	return nil
}

func writeJSON(response http.ResponseWriter, statusCode int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(body)
}

func writeError(response http.ResponseWriter, statusCode int, code, message string) {
	writeJSON(response, statusCode, errorResponse{ErrorCode: code, Message: message})
}

func flightView(item *flightv1.Flight) flightResponse {
	return flightResponse{
		ID: item.GetId(), FlightNumber: item.GetFlightNumber(), Airline: item.GetAirline(),
		Origin: item.GetOrigin(), Destination: item.GetDestination(),
		DepartureTime: item.GetDepartureTime().AsTime(), ArrivalTime: item.GetArrivalTime().AsTime(),
		TotalSeats: item.GetTotalSeats(), AvailableSeats: item.GetAvailableSeats(),
		PriceCents: item.GetPriceCents(), Status: strings.TrimPrefix(item.GetStatus().String(), "FLIGHT_STATUS_"),
	}
}
