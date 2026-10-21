package store

import (
	"context"
	"errors"
	"fmt"

	"marketplace-hw2/internal/fault"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, role string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role)
		VALUES (lower($1), $2, $3)
		RETURNING id, email, password_hash, role
	`, email, passwordHash, role).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, fault.New("EMAIL_ALREADY_EXISTS", "a user with this email already exists")
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role
		FROM users
		WHERE email = lower($1)
	`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fault.New("INVALID_CREDENTIALS", "email or password is invalid")
	}
	if err != nil {
		return User{}, fmt.Errorf("find user by email: %w", err)
	}
	return user, nil
}

func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fault.New("REFRESH_TOKEN_INVALID", "refresh token user no longer exists")
	}
	if err != nil {
		return User{}, fmt.Errorf("find user by id: %w", err)
	}
	return user, nil
}
