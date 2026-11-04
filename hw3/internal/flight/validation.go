package flight

import (
	"regexp"
	"strings"

	flightv1 "flight-booking-hw3/gen/flight/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	iataPattern         = regexp.MustCompile(`^[A-Z]{3}$`)
	flightNumberPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
)

func searchFilter(request *flightv1.SearchFlightsRequest) (SearchFilter, error) {
	origin := strings.ToUpper(strings.TrimSpace(request.GetOrigin()))
	destination := strings.ToUpper(strings.TrimSpace(request.GetDestination()))
	if !iataPattern.MatchString(origin) || !iataPattern.MatchString(destination) || origin == destination {
		return SearchFilter{}, status.Error(codes.InvalidArgument, "origin and destination must be different three-letter IATA codes")
	}
	filter := SearchFilter{Origin: origin, Destination: destination}
	if request.Date == nil {
		return filter, nil
	}
	if err := request.Date.CheckValid(); err != nil {
		return SearchFilter{}, status.Error(codes.InvalidArgument, "date is invalid")
	}
	date := request.Date.AsTime().UTC()
	filter.Date = &date
	return filter, nil
}

func flightUpdate(request *flightv1.UpdateFlightRequest) (FlightUpdate, error) {
	flightNumber := strings.ToUpper(strings.TrimSpace(request.GetFlightNumber()))
	origin := strings.ToUpper(strings.TrimSpace(request.GetOrigin()))
	destination := strings.ToUpper(strings.TrimSpace(request.GetDestination()))
	airline := strings.TrimSpace(request.GetAirline())
	if !flightNumberPattern.MatchString(flightNumber) {
		return FlightUpdate{}, status.Error(codes.InvalidArgument, "flight_number is invalid")
	}
	if airline == "" {
		return FlightUpdate{}, status.Error(codes.InvalidArgument, "airline is required")
	}
	if !iataPattern.MatchString(origin) || !iataPattern.MatchString(destination) || origin == destination {
		return FlightUpdate{}, status.Error(codes.InvalidArgument, "origin and destination are invalid")
	}
	if request.DepartureTime == nil || request.ArrivalTime == nil {
		return FlightUpdate{}, status.Error(codes.InvalidArgument, "departure_time and arrival_time are required")
	}
	if err := request.DepartureTime.CheckValid(); err != nil {
		return FlightUpdate{}, status.Error(codes.InvalidArgument, "departure_time is invalid")
	}
	if err := request.ArrivalTime.CheckValid(); err != nil {
		return FlightUpdate{}, status.Error(codes.InvalidArgument, "arrival_time is invalid")
	}
	departure := request.DepartureTime.AsTime()
	arrival := request.ArrivalTime.AsTime()
	if !arrival.After(departure) || request.GetTotalSeats() < 1 || request.GetPriceCents() < 1 {
		return FlightUpdate{}, status.Error(codes.InvalidArgument, "times, total_seats or price_cents are invalid")
	}
	statusValue := flightStatusName(request.GetStatus())
	if statusValue == "" {
		return FlightUpdate{}, status.Error(codes.InvalidArgument, "status is invalid")
	}
	return FlightUpdate{
		FlightNumber: flightNumber, Airline: airline, Origin: origin, Destination: destination,
		DepartureTime: departure, ArrivalTime: arrival, TotalSeats: request.GetTotalSeats(),
		PriceCents: request.GetPriceCents(), Status: statusValue,
	}, nil
}
