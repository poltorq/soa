package booking

import (
	"context"
	"log/slog"
	"math"
	"net/mail"
	"strings"
	"time"

	flightv1 "flight-booking-hw3/gen/flight/v1"

	"github.com/google/uuid"
)

type Service struct {
	store   *Store
	flights *FlightClient
	logger  *slog.Logger
}

type CreateBookingInput struct {
	UserID         string `json:"user_id"`
	FlightID       string `json:"flight_id"`
	PassengerName  string `json:"passenger_name"`
	PassengerEmail string `json:"passenger_email"`
	SeatCount      int32  `json:"seat_count"`
}

func NewService(store *Store, flights *FlightClient, logger *slog.Logger) *Service {
	return &Service{store: store, flights: flights, logger: logger}
}

func (s *Service) SearchFlights(ctx context.Context, origin, destination, dateValue string) ([]*flightv1.Flight, error) {
	var date *time.Time
	if dateValue != "" {
		parsed, err := time.Parse(time.DateOnly, dateValue)
		if err != nil {
			return nil, newValidationError("date must use YYYY-MM-DD format")
		}
		date = &parsed
	}
	return s.flights.Search(ctx, origin, destination, date)
}

func (s *Service) GetFlight(ctx context.Context, id string) (*flightv1.Flight, error) {
	return s.flights.Get(ctx, id)
}

func (s *Service) CreateBooking(ctx context.Context, request CreateBookingInput) (Booking, error) {
	input, err := validateBookingRequest(request)
	if err != nil {
		return Booking{}, err
	}
	flightInfo, err := s.flights.Get(ctx, input.FlightID.String())
	if err != nil {
		return Booking{}, err
	}
	if flightInfo.GetPriceCents() > math.MaxInt64/int64(input.SeatCount) {
		return Booking{}, newValidationError("total price is too large")
	}
	input.ID = uuid.New()
	input.TotalPriceCents = flightInfo.GetPriceCents() * int64(input.SeatCount)
	if err := s.flights.Reserve(ctx, input.FlightID.String(), input.SeatCount, input.ID.String()); err != nil {
		return Booking{}, err
	}
	result, err := s.store.Create(ctx, input)
	if err == nil {
		return result, nil
	}
	compensationCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if releaseErr := s.flights.Release(compensationCtx, input.ID.String()); releaseErr != nil {
		s.logger.Error("compensating reservation release failed", "booking_id", input.ID, "error", releaseErr)
	}
	return Booking{}, err
}

func (s *Service) GetBooking(ctx context.Context, id uuid.UUID) (Booking, error) {
	return s.store.GetBooking(ctx, id)
}

func (s *Service) ListBookings(ctx context.Context, userID uuid.UUID) ([]Booking, error) {
	return s.store.ListBookingsByUser(ctx, userID)
}

func (s *Service) Ready(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) CancelBooking(ctx context.Context, id uuid.UUID) (Booking, error) {
	item, err := s.store.GetBooking(ctx, id)
	if err != nil {
		return Booking{}, err
	}
	if item.Status != "CONFIRMED" {
		return Booking{}, ErrBookingNotConfirmed
	}
	if err := s.flights.Release(ctx, id.String()); err != nil {
		return Booking{}, err
	}
	return s.store.Cancel(ctx, id)
}

func validateBookingRequest(request CreateBookingInput) (CreateInput, error) {
	userID, err := uuid.Parse(request.UserID)
	if err != nil {
		return CreateInput{}, newValidationError("user_id must be a UUID")
	}
	flightID, err := uuid.Parse(request.FlightID)
	if err != nil {
		return CreateInput{}, newValidationError("flight_id must be a UUID")
	}
	name := strings.TrimSpace(request.PassengerName)
	if len(name) < 1 || len(name) > 200 {
		return CreateInput{}, newValidationError("passenger_name must contain between 1 and 200 characters")
	}
	email := strings.TrimSpace(request.PassengerEmail)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 320 {
		return CreateInput{}, newValidationError("passenger_email is invalid")
	}
	if request.SeatCount < 1 || request.SeatCount > 9 {
		return CreateInput{}, newValidationError("seat_count must be between 1 and 9")
	}
	return CreateInput{
		UserID: userID, FlightID: flightID, PassengerName: name,
		PassengerEmail: email, SeatCount: request.SeatCount,
	}, nil
}
