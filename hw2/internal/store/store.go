package store

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

// MaxAmountCents is the largest amount representable by NUMERIC(12,2).
const MaxAmountCents int64 = 999_999_999_999

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func Cents(value float64) (int64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	result := math.Round(value * 100)
	if result > float64(MaxAmountCents) {
		return 0, false
	}
	return int64(result), true
}

func Decimal(cents int64) float64 {
	return float64(cents) / 100
}

func decimalString(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func parseDecimal(value string) (int64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse database decimal %q: %w", value, err)
	}
	cents, ok := Cents(parsed)
	if !ok {
		return 0, fmt.Errorf("database decimal %q is out of range", value)
	}
	return cents, nil
}
