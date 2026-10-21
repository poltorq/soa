package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/auth"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestAuthenticationErrorsMatchContract(t *testing.T) {
	secret := "authentication-test-secret-long-enough"
	tokens := auth.NewTokenManager(secret, 20*time.Minute, 14*24*time.Hour)
	identity := auth.Identity{UserID: uuid.New(), Role: "USER"}
	pair, err := tokens.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := auth.NewTokenManager(secret, -time.Minute, -time.Minute).Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(role, key string, expiry bool, method jwt.SigningMethod) string {
		claims := jwt.MapClaims{"sub": identity.UserID.String(), "role": role, "type": "access"}
		if expiry {
			claims["exp"] = time.Now().Add(time.Hour).Unix()
		}
		raw, err := jwt.NewWithClaims(method, claims).SignedString([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	handler, err := New(nil, tokens, time.Minute).Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, raw string
		refresh   bool
		code      api.ErrorCode
	}{
		{"missing", "", false, api.TOKENINVALID},
		{"malformed", "broken", false, api.TOKENINVALID},
		{"expired access", expired.AccessToken, false, api.TOKENEXPIRED},
		{"refresh used as access", pair.RefreshToken, false, api.TOKENINVALID},
		{"unknown role", sign("UNKNOWN", secret, true, jwt.SigningMethodHS256), false, api.TOKENINVALID},
		{"wrong signature", sign("USER", "different-secret", true, jwt.SigningMethodHS256), false, api.TOKENINVALID},
		{"unsupported algorithm", sign("USER", secret, true, jwt.SigningMethodHS384), false, api.TOKENINVALID},
		{"missing expiry", sign("USER", secret, false, jwt.SigningMethodHS256), false, api.TOKENINVALID},
		{"invalid refresh", "broken", true, api.REFRESHTOKENINVALID},
		{"expired refresh", expired.RefreshToken, true, api.REFRESHTOKENINVALID},
		{"access used as refresh", pair.AccessToken, true, api.REFRESHTOKENINVALID},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/products", nil)
			if test.raw != "" {
				r.Header.Set("Authorization", "Bearer "+test.raw)
			}
			if test.refresh {
				body, _ := json.Marshal(api.RefreshRequest{RefreshToken: test.raw})
				r = httptest.NewRequest("POST", "/auth/refresh", strings.NewReader(string(body)))
				r.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			var body api.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 401 || body.ErrorCode != test.code {
				t.Fatalf("expected 401 %s, got %d %s (%v)", test.code, w.Code, w.Body.String(), err)
			}
		})
	}
}
