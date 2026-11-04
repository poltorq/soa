package flight

import (
	flightv1 "flight-booking-hw3/gen/flight/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func flightMessage(item Flight) *flightv1.Flight {
	return &flightv1.Flight{
		Id: item.ID.String(), FlightNumber: item.FlightNumber, Airline: item.Airline,
		Origin: item.Origin, Destination: item.Destination,
		DepartureTime: timestamppb.New(item.DepartureTime), ArrivalTime: timestamppb.New(item.ArrivalTime),
		TotalSeats: item.TotalSeats, AvailableSeats: item.AvailableSeats,
		PriceCents: item.PriceCents, Status: flightStatusValue(item.Status),
	}
}

func reservationMessage(item Reservation) *flightv1.SeatReservation {
	return &flightv1.SeatReservation{
		Id: item.ID.String(), FlightId: item.FlightID.String(), BookingId: item.BookingID.String(),
		SeatCount: item.SeatCount, Status: reservationStatusValue(item.Status),
		CreatedAt: timestamppb.New(item.CreatedAt), UpdatedAt: timestamppb.New(item.UpdatedAt),
	}
}

func flightStatusValue(value string) flightv1.FlightStatus {
	switch value {
	case "SCHEDULED":
		return flightv1.FlightStatus_FLIGHT_STATUS_SCHEDULED
	case "DEPARTED":
		return flightv1.FlightStatus_FLIGHT_STATUS_DEPARTED
	case "CANCELLED":
		return flightv1.FlightStatus_FLIGHT_STATUS_CANCELLED
	case "COMPLETED":
		return flightv1.FlightStatus_FLIGHT_STATUS_COMPLETED
	default:
		return flightv1.FlightStatus_FLIGHT_STATUS_UNSPECIFIED
	}
}

func flightStatusName(value flightv1.FlightStatus) string {
	switch value {
	case flightv1.FlightStatus_FLIGHT_STATUS_SCHEDULED:
		return "SCHEDULED"
	case flightv1.FlightStatus_FLIGHT_STATUS_DEPARTED:
		return "DEPARTED"
	case flightv1.FlightStatus_FLIGHT_STATUS_CANCELLED:
		return "CANCELLED"
	case flightv1.FlightStatus_FLIGHT_STATUS_COMPLETED:
		return "COMPLETED"
	default:
		return ""
	}
}

func reservationStatusValue(value string) flightv1.ReservationStatus {
	switch value {
	case "ACTIVE":
		return flightv1.ReservationStatus_RESERVATION_STATUS_ACTIVE
	case "RELEASED":
		return flightv1.ReservationStatus_RESERVATION_STATUS_RELEASED
	case "EXPIRED":
		return flightv1.ReservationStatus_RESERVATION_STATUS_EXPIRED
	default:
		return flightv1.ReservationStatus_RESERVATION_STATUS_UNSPECIFIED
	}
}
