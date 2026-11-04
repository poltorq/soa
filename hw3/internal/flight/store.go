package flight

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func OpenStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create flight database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping flight database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) GetFlight(ctx context.Context, id uuid.UUID) (Flight, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, flight_number, airline, origin, destination, departure_time,
		       arrival_time, total_seats, available_seats, price_cents, status
		FROM flights
		WHERE id = $1
	`, id)
	result, err := scanFlight(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Flight{}, ErrFlightNotFound
	}
	if err != nil {
		return Flight{}, fmt.Errorf("get flight: %w", err)
	}
	return result, nil
}

func (s *Store) SearchFlights(ctx context.Context, filter SearchFilter) ([]Flight, error) {
	query := `
		SELECT id, flight_number, airline, origin, destination, departure_time,
		       arrival_time, total_seats, available_seats, price_cents, status
		FROM flights
		WHERE origin = $1 AND destination = $2 AND status = 'SCHEDULED'
	`
	args := []any{filter.Origin, filter.Destination}
	if filter.Date != nil {
		query += " AND (departure_time AT TIME ZONE 'UTC')::date = $3"
		args = append(args, filter.Date.UTC().Format(time.DateOnly))
	}
	query += " ORDER BY departure_time, id"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search flights: %w", err)
	}
	defer rows.Close()

	result := make([]Flight, 0)
	for rows.Next() {
		item, err := scanFlight(rows)
		if err != nil {
			return nil, fmt.Errorf("scan searched flight: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate searched flights: %w", err)
	}
	return result, nil
}

func (s *Store) ReserveSeats(
	ctx context.Context,
	flightID uuid.UUID,
	seatCount int32,
	bookingID uuid.UUID,
) (Reservation, int32, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Reservation{}, 0, fmt.Errorf("begin reserve seats: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))", bookingID); err != nil {
		return Reservation{}, 0, fmt.Errorf("lock booking reservation: %w", err)
	}
	existing, found, err := reservationByBooking(ctx, tx, bookingID)
	if err != nil {
		return Reservation{}, 0, err
	}
	if found {
		if existing.FlightID != flightID || existing.SeatCount != seatCount {
			return Reservation{}, 0, ErrReservationConflict
		}
		if existing.Status != "ACTIVE" {
			return Reservation{}, 0, ErrReservationReleased
		}
		available, err := availableSeats(ctx, tx, flightID)
		if err != nil {
			return Reservation{}, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Reservation{}, 0, fmt.Errorf("commit idempotent reservation: %w", err)
		}
		return existing, available, nil
	}

	var available int32
	var flightStatus string
	err = tx.QueryRow(ctx, `
		SELECT available_seats, status
		FROM flights
		WHERE id = $1
		FOR UPDATE
	`, flightID).Scan(&available, &flightStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, 0, ErrFlightNotFound
	}
	if err != nil {
		return Reservation{}, 0, fmt.Errorf("lock flight: %w", err)
	}
	if flightStatus != "SCHEDULED" {
		return Reservation{}, 0, ErrFlightNotScheduled
	}
	if available < seatCount {
		return Reservation{}, 0, ErrInsufficientSeats
	}

	available -= seatCount
	if _, err := tx.Exec(ctx, `
		UPDATE flights SET available_seats = $2, updated_at = now() WHERE id = $1
	`, flightID, available); err != nil {
		return Reservation{}, 0, fmt.Errorf("decrease available seats: %w", err)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO seat_reservations (flight_id, booking_id, seat_count)
		VALUES ($1, $2, $3)
		RETURNING id, flight_id, booking_id, seat_count, status, created_at, updated_at
	`, flightID, bookingID, seatCount)
	reservation, err := scanReservation(row)
	if err != nil {
		return Reservation{}, 0, fmt.Errorf("create seat reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, 0, fmt.Errorf("commit reserve seats: %w", err)
	}
	return reservation, available, nil
}

func (s *Store) ReleaseReservation(ctx context.Context, bookingID uuid.UUID) (Reservation, int32, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Reservation{}, 0, fmt.Errorf("begin release reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))", bookingID); err != nil {
		return Reservation{}, 0, fmt.Errorf("lock booking reservation: %w", err)
	}
	reservation, found, err := reservationByBooking(ctx, tx, bookingID)
	if err != nil {
		return Reservation{}, 0, err
	}
	if !found {
		return Reservation{}, 0, ErrReservationNotFound
	}
	if reservation.Status != "ACTIVE" {
		available, err := availableSeats(ctx, tx, reservation.FlightID)
		if err != nil {
			return Reservation{}, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Reservation{}, 0, fmt.Errorf("commit idempotent release: %w", err)
		}
		return reservation, available, nil
	}

	var available, total int32
	err = tx.QueryRow(ctx, `
		SELECT available_seats, total_seats FROM flights WHERE id = $1 FOR UPDATE
	`, reservation.FlightID).Scan(&available, &total)
	if err != nil {
		return Reservation{}, 0, fmt.Errorf("lock reserved flight: %w", err)
	}
	available += reservation.SeatCount
	if available > total {
		return Reservation{}, 0, fmt.Errorf("released seats exceed total seats")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE flights SET available_seats = $2, updated_at = now() WHERE id = $1
	`, reservation.FlightID, available); err != nil {
		return Reservation{}, 0, fmt.Errorf("return available seats: %w", err)
	}
	row := tx.QueryRow(ctx, `
		UPDATE seat_reservations
		SET status = 'RELEASED', updated_at = now()
		WHERE id = $1
		RETURNING id, flight_id, booking_id, seat_count, status, created_at, updated_at
	`, reservation.ID)
	reservation, err = scanReservation(row)
	if err != nil {
		return Reservation{}, 0, fmt.Errorf("mark reservation released: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, 0, fmt.Errorf("commit release reservation: %w", err)
	}
	return reservation, available, nil
}

func (s *Store) UpdateFlight(ctx context.Context, id uuid.UUID, input FlightUpdate) (Flight, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Flight{}, fmt.Errorf("begin update flight: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentTotal, currentAvailable int32
	err = tx.QueryRow(ctx, `
		SELECT total_seats, available_seats FROM flights WHERE id = $1 FOR UPDATE
	`, id).Scan(&currentTotal, &currentAvailable)
	if errors.Is(err, pgx.ErrNoRows) {
		return Flight{}, ErrFlightNotFound
	}
	if err != nil {
		return Flight{}, fmt.Errorf("lock flight for update: %w", err)
	}
	reserved := currentTotal - currentAvailable
	if input.TotalSeats < reserved {
		return Flight{}, ErrTotalSeatsTooSmall
	}
	newAvailable := input.TotalSeats - reserved
	row := tx.QueryRow(ctx, `
		UPDATE flights
		SET flight_number = $2, airline = $3, origin = $4, destination = $5,
		    departure_time = $6, arrival_time = $7,
		    total_seats = $8, available_seats = $9, price_cents = $10,
		    status = $11, updated_at = now()
		WHERE id = $1
		RETURNING id, flight_number, airline, origin, destination, departure_time,
		          arrival_time, total_seats, available_seats, price_cents, status
	`, id, input.FlightNumber, input.Airline, input.Origin, input.Destination,
		input.DepartureTime, input.ArrivalTime,
		input.TotalSeats, newAvailable, input.PriceCents, input.Status)
	result, err := scanFlight(row)
	if err != nil {
		return Flight{}, fmt.Errorf("update flight: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Flight{}, fmt.Errorf("commit update flight: %w", err)
	}
	return result, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFlight(row rowScanner) (Flight, error) {
	var result Flight
	err := row.Scan(
		&result.ID, &result.FlightNumber, &result.Airline, &result.Origin, &result.Destination,
		&result.DepartureTime, &result.ArrivalTime, &result.TotalSeats, &result.AvailableSeats,
		&result.PriceCents, &result.Status,
	)
	return result, err
}

func scanReservation(row rowScanner) (Reservation, error) {
	var result Reservation
	err := row.Scan(
		&result.ID, &result.FlightID, &result.BookingID, &result.SeatCount,
		&result.Status, &result.CreatedAt, &result.UpdatedAt,
	)
	return result, err
}

func reservationByBooking(ctx context.Context, tx pgx.Tx, bookingID uuid.UUID) (Reservation, bool, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, flight_id, booking_id, seat_count, status, created_at, updated_at
		FROM seat_reservations
		WHERE booking_id = $1
		FOR UPDATE
	`, bookingID)
	result, err := scanReservation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, false, nil
	}
	if err != nil {
		return Reservation{}, false, fmt.Errorf("get booking reservation: %w", err)
	}
	return result, true, nil
}

func availableSeats(ctx context.Context, tx pgx.Tx, flightID uuid.UUID) (int32, error) {
	var available int32
	if err := tx.QueryRow(ctx, "SELECT available_seats FROM flights WHERE id = $1", flightID).Scan(&available); err != nil {
		return 0, fmt.Errorf("get available seats: %w", err)
	}
	return available, nil
}
