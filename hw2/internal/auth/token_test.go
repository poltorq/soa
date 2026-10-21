package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTokenManagerIssueAndParse(t *testing.T) {
	manager := NewTokenManager("a-secret-that-is-long-enough-for-tests", 20*time.Minute, 14*24*time.Hour)
	identity := Identity{UserID: uuid.New(), Role: "USER"}

	pair, err := manager.Issue(identity)
	if err != nil {
		t.Fatalf("issue token pair: %v", err)
	}
	got, err := manager.ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	if got != identity {
		t.Fatalf("expected identity %+v, got %+v", identity, got)
	}
	if pair.ExpiresIn != 1200 {
		t.Fatalf("expected 1200 seconds, got %d", pair.ExpiresIn)
	}
	if _, err := manager.ParseAccess(pair.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected refresh token to be rejected as access token, got %v", err)
	}
}

func TestTokenManagerRejectsExpiredToken(t *testing.T) {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	manager := NewTokenManager("a-secret-that-is-long-enough-for-tests", time.Minute, time.Hour)
	manager.now = func() time.Time { return now }
	pair, err := manager.Issue(Identity{UserID: uuid.New(), Role: "USER"})
	if err != nil {
		t.Fatalf("issue token pair: %v", err)
	}
	manager.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := manager.ParseAccess(pair.AccessToken); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected expired token error, got %v", err)
	}
}

func TestTokenPairsAreUnique(t *testing.T) {
	manager := NewTokenManager("a-secret-that-is-long-enough-for-tests", 20*time.Minute, 14*24*time.Hour)
	identity := Identity{UserID: uuid.New(), Role: "USER"}
	first, err := manager.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	if first.AccessToken == second.AccessToken || first.RefreshToken == second.RefreshToken {
		t.Fatal("new token pairs must have unique IDs")
	}
}
