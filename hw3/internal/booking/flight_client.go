package booking

import (
	"context"
	"time"

	flightv1 "flight-booking-hw3/gen/flight/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type FlightClient struct {
	client flightv1.FlightServiceClient
}

func NewFlightClient(client flightv1.FlightServiceClient) *FlightClient {
	return &FlightClient{client: client}
}

func APIKeyUnaryClientInterceptor(apiKey string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", apiKey)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func (c *FlightClient) Search(ctx context.Context, origin, destination string, date *time.Time) ([]*flightv1.Flight, error) {
	request := &flightv1.SearchFlightsRequest{Origin: origin, Destination: destination}
	if date != nil {
		request.Date = timestamppb.New(*date)
	}
	response, err := c.client.SearchFlights(ctx, request)
	if err != nil {
		return nil, err
	}
	return response.GetFlights(), nil
}

func (c *FlightClient) Get(ctx context.Context, id string) (*flightv1.Flight, error) {
	response, err := c.client.GetFlight(ctx, &flightv1.GetFlightRequest{Id: id})
	if err != nil {
		return nil, err
	}
	return response.GetFlight(), nil
}

func (c *FlightClient) Reserve(ctx context.Context, flightID string, seatCount int32, bookingID string) error {
	_, err := c.client.ReserveSeats(ctx, &flightv1.ReserveSeatsRequest{
		FlightId: flightID, SeatCount: seatCount, BookingId: bookingID,
	})
	return err
}

func (c *FlightClient) Release(ctx context.Context, bookingID string) error {
	_, err := c.client.ReleaseReservation(ctx, &flightv1.ReleaseReservationRequest{BookingId: bookingID})
	return err
}
