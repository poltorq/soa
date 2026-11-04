package booking

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type ValidationError struct {
	message string
}

func (e *ValidationError) Error() string {
	return e.message
}

func newValidationError(message string) error {
	return &ValidationError{message: message}
}

var (
	ErrBookingNotFound     = errors.New("booking not found")
	ErrBookingNotConfirmed = errors.New("only a confirmed booking can be cancelled")
)

type Booking struct {
	ID              uuid.UUID `json:"id"`
	UserID          uuid.UUID `json:"user_id"`
	FlightID        uuid.UUID `json:"flight_id"`
	PassengerName   string    `json:"passenger_name"`
	PassengerEmail  string    `json:"passenger_email"`
	SeatCount       int32     `json:"seat_count"`
	TotalPriceCents int64     `json:"total_price_cents"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateInput struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	FlightID        uuid.UUID
	PassengerName   string
	PassengerEmail  string
	SeatCount       int32
	TotalPriceCents int64
}
