package flight

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrFlightNotFound      = errors.New("flight not found")
	ErrReservationNotFound = errors.New("reservation not found")
	ErrInsufficientSeats   = errors.New("not enough available seats")
	ErrFlightNotScheduled  = errors.New("flight is not scheduled")
	ErrReservationConflict = errors.New("booking already has another reservation")
	ErrReservationReleased = errors.New("reservation has already been released")
	ErrTotalSeatsTooSmall  = errors.New("total seats cannot be lower than reserved seats")
)

type Flight struct {
	ID             uuid.UUID `json:"id"`
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

type Reservation struct {
	ID        uuid.UUID `json:"id"`
	FlightID  uuid.UUID `json:"flight_id"`
	BookingID uuid.UUID `json:"booking_id"`
	SeatCount int32     `json:"seat_count"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SearchFilter struct {
	Origin      string
	Destination string
	Date        *time.Time
}

type FlightUpdate struct {
	FlightNumber  string
	Airline       string
	Origin        string
	Destination   string
	DepartureTime time.Time
	ArrivalTime   time.Time
	TotalSeats    int32
	PriceCents    int64
	Status        string
}
