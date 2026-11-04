package flight

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIntegrationConcurrentReservationAndIdempotency(t *testing.T) {
	databaseURL := os.Getenv("TEST_FLIGHT_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_FLIGHT_DATABASE_URL is not set")
	}
	ctx := context.Background()
	store, err := OpenStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(store.Close)
	cleanupIntegrationData(t, store)

	flightID := uuid.New()
	flightNumber := "IT" + strings.ToUpper(strings.ReplaceAll(flightID.String()[:6], "-", ""))
	departure := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	_, err = store.pool.Exec(ctx, `
		INSERT INTO flights (
			id, flight_number, airline, origin, destination, departure_time,
			arrival_time, total_seats, available_seats, price_cents, status
		)
		VALUES ($1, $2, 'Integration Air', 'SVO', 'LED', $3, $4, 5, 5, 10000, 'SCHEDULED')
	`, flightID, flightNumber, departure, departure.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("insert test flight: %v", err)
	}
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(), "DELETE FROM seat_reservations WHERE flight_id = $1", flightID); err != nil {
			t.Errorf("delete test reservations: %v", err)
		}
		if _, err := store.pool.Exec(context.Background(), "DELETE FROM flights WHERE id = $1", flightID); err != nil {
			t.Errorf("delete test flight: %v", err)
		}
	})

	bookingID := uuid.New()
	errCh := make(chan error, 5)
	var wait sync.WaitGroup
	for range 5 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, reserveErr := store.ReserveSeats(ctx, flightID, 1, bookingID)
			errCh <- reserveErr
		}()
	}
	wait.Wait()
	close(errCh)
	for reserveErr := range errCh {
		if reserveErr != nil {
			t.Fatalf("idempotent reservation failed: %v", reserveErr)
		}
	}
	assertAvailableSeats(t, store, flightID, 4)
	if _, _, err := store.ReleaseReservation(ctx, bookingID); err != nil {
		t.Fatalf("release reservation: %v", err)
	}
	if _, _, err := store.ReleaseReservation(ctx, bookingID); err != nil {
		t.Fatalf("idempotent release: %v", err)
	}
	assertAvailableSeats(t, store, flightID, 5)

	errCh = make(chan error, 10)
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, reserveErr := store.ReserveSeats(ctx, flightID, 1, uuid.New())
			errCh <- reserveErr
		}()
	}
	wait.Wait()
	close(errCh)
	succeeded, exhausted := 0, 0
	for reserveErr := range errCh {
		switch {
		case reserveErr == nil:
			succeeded++
		case errors.Is(reserveErr, ErrInsufficientSeats):
			exhausted++
		default:
			t.Fatalf("unexpected concurrent reservation error: %v", reserveErr)
		}
	}
	if succeeded != 5 || exhausted != 5 {
		t.Fatalf("expected 5 successes and 5 exhausted errors, got %d and %d", succeeded, exhausted)
	}
	assertAvailableSeats(t, store, flightID, 0)
}

func assertAvailableSeats(t *testing.T, store *Store, flightID uuid.UUID, expected int32) {
	t.Helper()
	item, err := store.GetFlight(context.Background(), flightID)
	if err != nil {
		t.Fatalf("get test flight: %v", err)
	}
	if item.AvailableSeats != expected {
		t.Fatalf("expected %d available seats, got %d", expected, item.AvailableSeats)
	}
}

func cleanupIntegrationData(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `
		DELETE FROM seat_reservations
		WHERE flight_id IN (SELECT id FROM flights WHERE airline = 'Integration Air')
	`); err != nil {
		t.Fatalf("delete stale integration reservations: %v", err)
	}
	if _, err := store.pool.Exec(ctx, "DELETE FROM flights WHERE airline = 'Integration Air'"); err != nil {
		t.Fatalf("delete stale integration flights: %v", err)
	}
}
