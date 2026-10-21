package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrExpiredToken = errors.New("token expired")
	ErrInvalidToken = errors.New("token invalid")
)

type Identity struct {
	UserID uuid.UUID
	Role   string
}

type Pair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

type claims struct {
	Role string `json:"role"`
	Type string `json:"type"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		now:        time.Now,
	}
}

func (m *TokenManager) Issue(identity Identity) (Pair, error) {
	if identity.UserID == uuid.Nil || !validRole(identity.Role) {
		return Pair{}, ErrInvalidToken
	}
	access, err := m.sign(identity, "access", m.accessTTL)
	if err != nil {
		return Pair{}, fmt.Errorf("sign access token: %w", err)
	}
	refresh, err := m.sign(identity, "refresh", m.refreshTTL)
	if err != nil {
		return Pair{}, fmt.Errorf("sign refresh token: %w", err)
	}
	return Pair{AccessToken: access, RefreshToken: refresh, ExpiresIn: int64(m.accessTTL.Seconds())}, nil
}

func (m *TokenManager) ParseAccess(raw string) (Identity, error) {
	return m.parse(raw, "access")
}

func (m *TokenManager) ParseRefresh(raw string) (Identity, error) {
	return m.parse(raw, "refresh")
}

func (m *TokenManager) sign(identity Identity, tokenType string, ttl time.Duration) (string, error) {
	now := m.now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: identity.Role,
		Type: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Subject:   identity.UserID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	})
	return token.SignedString(m.secret)
}

func (m *TokenManager) parse(raw, expectedType string) (Identity, error) {
	parsed := &claims{}
	token, err := jwt.ParseWithClaims(raw, parsed, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	}, jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(m.now))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenSignatureInvalid) || errors.Is(err, jwt.ErrTokenMalformed) || errors.Is(err, jwt.ErrTokenUnverifiable) {
			return Identity{}, ErrInvalidToken
		}
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Identity{}, ErrExpiredToken
		}
		return Identity{}, ErrInvalidToken
	}
	if !token.Valid || parsed.Type != expectedType {
		return Identity{}, ErrInvalidToken
	}
	userID, err := uuid.Parse(parsed.Subject)
	if err != nil || userID == uuid.Nil || !validRole(parsed.Role) {
		return Identity{}, ErrInvalidToken
	}
	return Identity{UserID: userID, Role: parsed.Role}, nil
}

func validRole(role string) bool {
	return role == "USER" || role == "SELLER" || role == "ADMIN"
}
