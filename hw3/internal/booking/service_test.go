package booking

import (
	"errors"
	"testing"
)

func TestValidateBookingRequest(t *testing.T) {
	input, err := validateBookingRequest(CreateBookingInput{
		UserID:         "20000000-0000-0000-0000-000000000001",
		FlightID:       "10000000-0000-0000-0000-000000000001",
		PassengerName:  " Anna Petrova ",
		PassengerEmail: "anna@example.com",
		SeatCount:      2,
	})
	if err != nil {
		t.Fatalf("validate booking: %v", err)
	}
	if input.PassengerName != "Anna Petrova" || input.SeatCount != 2 {
		t.Fatalf("unexpected normalized input: %+v", input)
	}
}

func TestValidateBookingRequestRejectsInvalidFields(t *testing.T) {
	tests := []CreateBookingInput{
		{UserID: "bad", FlightID: "10000000-0000-0000-0000-000000000001", PassengerName: "Anna", PassengerEmail: "anna@example.com", SeatCount: 1},
		{UserID: "20000000-0000-0000-0000-000000000001", FlightID: "bad", PassengerName: "Anna", PassengerEmail: "anna@example.com", SeatCount: 1},
		{UserID: "20000000-0000-0000-0000-000000000001", FlightID: "10000000-0000-0000-0000-000000000001", PassengerName: "", PassengerEmail: "anna@example.com", SeatCount: 1},
		{UserID: "20000000-0000-0000-0000-000000000001", FlightID: "10000000-0000-0000-0000-000000000001", PassengerName: "Anna", PassengerEmail: "bad", SeatCount: 1},
		{UserID: "20000000-0000-0000-0000-000000000001", FlightID: "10000000-0000-0000-0000-000000000001", PassengerName: "Anna", PassengerEmail: "anna@example.com", SeatCount: 0},
	}
	for index, request := range tests {
		_, err := validateBookingRequest(request)
		var validationErr *ValidationError
		if err == nil || !errors.As(err, &validationErr) {
			t.Fatalf("case %d: expected validation error", index)
		}
	}
}
