package booking

import (
	"context"
	"errors"
	"fmt"

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
		return nil, fmt.Errorf("create booking database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping booking database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) Create(ctx context.Context, input CreateInput) (Booking, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO bookings (
			id, user_id, flight_id, passenger_name, passenger_email,
			seat_count, total_price_cents, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'CONFIRMED')
		RETURNING id, user_id, flight_id, passenger_name, passenger_email,
		          seat_count, total_price_cents, status, created_at, updated_at
	`, input.ID, input.UserID, input.FlightID, input.PassengerName, input.PassengerEmail,
		input.SeatCount, input.TotalPriceCents)
	result, err := scanBooking(row)
	if err != nil {
		return Booking{}, fmt.Errorf("create booking: %w", err)
	}
	return result, nil
}

func (s *Store) GetBooking(ctx context.Context, id uuid.UUID) (Booking, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, flight_id, passenger_name, passenger_email,
		       seat_count, total_price_cents, status, created_at, updated_at
		FROM bookings
		WHERE id = $1
	`, id)
	result, err := scanBooking(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Booking{}, ErrBookingNotFound
	}
	if err != nil {
		return Booking{}, fmt.Errorf("get booking: %w", err)
	}
	return result, nil
}

func (s *Store) ListBookingsByUser(ctx context.Context, userID uuid.UUID) ([]Booking, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, flight_id, passenger_name, passenger_email,
		       seat_count, total_price_cents, status, created_at, updated_at
		FROM bookings
		WHERE user_id = $1
		ORDER BY created_at DESC, id
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user bookings: %w", err)
	}
	defer rows.Close()
	result := make([]Booking, 0)
	for rows.Next() {
		item, err := scanBooking(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user booking: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user bookings: %w", err)
	}
	return result, nil
}

func (s *Store) Cancel(ctx context.Context, id uuid.UUID) (Booking, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE bookings
		SET status = 'CANCELLED', updated_at = now()
		WHERE id = $1 AND status = 'CONFIRMED'
		RETURNING id, user_id, flight_id, passenger_name, passenger_email,
		          seat_count, total_price_cents, status, created_at, updated_at
	`, id)
	result, err := scanBooking(row)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := s.GetBooking(ctx, id)
		if getErr != nil {
			return Booking{}, getErr
		}
		if existing.Status != "CONFIRMED" {
			return Booking{}, ErrBookingNotConfirmed
		}
	}
	if err != nil {
		return Booking{}, fmt.Errorf("cancel booking: %w", err)
	}
	return result, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanBooking(row rowScanner) (Booking, error) {
	var result Booking
	err := row.Scan(
		&result.ID, &result.UserID, &result.FlightID, &result.PassengerName,
		&result.PassengerEmail, &result.SeatCount, &result.TotalPriceCents,
		&result.Status, &result.CreatedAt, &result.UpdatedAt,
	)
	return result, err
}
