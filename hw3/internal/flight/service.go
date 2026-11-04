package flight

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	flightv1 "flight-booking-hw3/gen/flight/v1"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	flightv1.UnimplementedFlightServiceServer
	store   *Store
	cache   *Cache
	logger  *slog.Logger
	cacheMu sync.RWMutex
}

func NewService(store *Store, cache *Cache, logger *slog.Logger) *Service {
	return &Service{store: store, cache: cache, logger: logger}
}

func (s *Service) SearchFlights(ctx context.Context, request *flightv1.SearchFlightsRequest) (*flightv1.SearchFlightsResponse, error) {
	filter, err := searchFilter(request)
	if err != nil {
		return nil, err
	}
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	items, found := s.cache.GetSearch(ctx, filter)
	if !found {
		items, err = s.store.SearchFlights(ctx, filter)
		if err != nil {
			return nil, s.internalError("search flights", err)
		}
		s.cache.SetSearch(ctx, filter, items)
	}
	response := &flightv1.SearchFlightsResponse{Flights: make([]*flightv1.Flight, 0, len(items))}
	for _, item := range items {
		response.Flights = append(response.Flights, flightMessage(item))
	}
	return response, nil
}

func (s *Service) GetFlight(ctx context.Context, request *flightv1.GetFlightRequest) (*flightv1.GetFlightResponse, error) {
	id, err := uuid.Parse(request.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "flight id must be a UUID")
	}
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	item, found := s.cache.GetFlight(ctx, id)
	if !found {
		item, err = s.store.GetFlight(ctx, id)
		if errors.Is(err, ErrFlightNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err != nil {
			return nil, s.internalError("get flight", err)
		}
		s.cache.SetFlight(ctx, item)
	}
	return &flightv1.GetFlightResponse{Flight: flightMessage(item)}, nil
}

func (s *Service) ReserveSeats(ctx context.Context, request *flightv1.ReserveSeatsRequest) (*flightv1.ReserveSeatsResponse, error) {
	flightID, err := uuid.Parse(request.GetFlightId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "flight_id must be a UUID")
	}
	bookingID, err := uuid.Parse(request.GetBookingId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "booking_id must be a UUID")
	}
	if request.GetSeatCount() < 1 || request.GetSeatCount() > 9 {
		return nil, status.Error(codes.InvalidArgument, "seat_count must be between 1 and 9")
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	reservation, available, err := s.store.ReserveSeats(ctx, flightID, request.GetSeatCount(), bookingID)
	if err != nil {
		return nil, s.businessError("reserve seats", err)
	}
	s.cache.InvalidateFlightData(ctx, flightID)
	return &flightv1.ReserveSeatsResponse{
		Reservation:    reservationMessage(reservation),
		AvailableSeats: available,
	}, nil
}

func (s *Service) ReleaseReservation(ctx context.Context, request *flightv1.ReleaseReservationRequest) (*flightv1.ReleaseReservationResponse, error) {
	bookingID, err := uuid.Parse(request.GetBookingId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "booking_id must be a UUID")
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	reservation, available, err := s.store.ReleaseReservation(ctx, bookingID)
	if err != nil {
		return nil, s.businessError("release reservation", err)
	}
	s.cache.InvalidateFlightData(ctx, reservation.FlightID)
	return &flightv1.ReleaseReservationResponse{
		Reservation:    reservationMessage(reservation),
		AvailableSeats: available,
	}, nil
}

func (s *Service) UpdateFlight(ctx context.Context, request *flightv1.UpdateFlightRequest) (*flightv1.UpdateFlightResponse, error) {
	id, err := uuid.Parse(request.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "flight id must be a UUID")
	}
	input, err := flightUpdate(request)
	if err != nil {
		return nil, err
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	item, err := s.store.UpdateFlight(ctx, id, input)
	if err != nil {
		return nil, s.businessError("update flight", err)
	}
	s.cache.InvalidateFlightData(ctx, id)
	return &flightv1.UpdateFlightResponse{Flight: flightMessage(item)}, nil
}

func (s *Service) businessError(operation string, err error) error {
	switch {
	case errors.Is(err, ErrFlightNotFound), errors.Is(err, ErrReservationNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrInsufficientSeats):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, ErrReservationConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, ErrFlightNotScheduled), errors.Is(err, ErrReservationReleased), errors.Is(err, ErrTotalSeatsTooSmall):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return s.internalError(operation, err)
	}
}

func (s *Service) internalError(operation string, err error) error {
	s.logger.Error(operation, "error", err)
	return status.Error(codes.Internal, "internal flight service error")
}
