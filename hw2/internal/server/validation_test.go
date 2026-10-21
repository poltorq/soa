package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/auth"

	"github.com/google/uuid"
)

func TestOpenAPIContract(t *testing.T) {
	spec, err := api.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatalf("invalid OpenAPI specification: %v", err)
	}
}

func TestValidationRejectsRequestsBeforeDatabaseAccess(t *testing.T) {
	tokens := auth.NewTokenManager("validation-test-secret-long-enough", 20*time.Minute, 14*24*time.Hour)
	pair, err := tokens.Issue(auth.Identity{UserID: uuid.New(), Role: "SELLER"})
	if err != nil {
		t.Fatal(err)
	}
	// No store: invalid input must be rejected before accessing the database.
	handler, err := New(nil, tokens, time.Minute).Handler()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, method, path, body string
		fields                   []string
	}{
		{"product UUID", "GET", "/products/not-a-uuid", "", []string{"id"}},
		{"archive UUID", "DELETE", "/products/not-a-uuid", "", []string{"id"}},
		{"order UUID", "GET", "/orders/not-a-uuid", "", []string{"id"}},
		{"cancel UUID", "POST", "/orders/not-a-uuid/cancel", "", []string{"id"}},
		{"invalid page", "GET", "/products?page=abc", "", []string{"page"}},
		{"page overflow", "GET", "/products?page=9223372036854775807&size=100", "", []string{"page"}},
		{"price overflow", "POST", "/products", `{"name":"Test","price":10000000000,"stock":1,"category":"test","status":"ACTIVE"}`, []string{"price"}},
		{"stock overflow", "POST", "/products", `{"name":"Test","price":1,"stock":2147483648,"category":"test","status":"ACTIVE"}`, []string{"stock"}},
		{"multiple violations", "POST", "/products", `{"name":"","price":0,"stock":-1,"category":"","status":"ACTIVE"}`, []string{"name", "price", "stock", "category"}},
		{"required fields", "POST", "/products", `{}`, []string{"name", "price", "stock", "category", "status"}},
		{"nested violations", "POST", "/orders", `{"items":[{"product_id":"10000000-0000-0000-0000-000000000001","quantity":0},{"product_id":"10000000-0000-0000-0000-000000000002","quantity":1000}]}`, []string{"items[0].quantity", "items[1].quantity"}},
		{"promo amount overflow", "POST", "/promo-codes", `{"code":"TEST","discount_type":"FIXED_AMOUNT","discount_value":10000000000,"min_order_amount":10000000000,"max_uses":2147483648,"valid_from":"2020-01-01T00:00:00Z","valid_until":"2099-01-01T00:00:00Z","active":true}`, []string{"discount_value", "min_order_amount", "max_uses"}},
		{"register UTF-8 byte limit", "POST", "/auth/register", `{"email":"test@example.com","password":"` + strings.Repeat("я", 40) + `","role":"USER"}`, []string{"password"}},
		{"login UTF-8 byte limit", "POST", "/auth/login", `{"email":"test@example.com","password":"` + strings.Repeat("я", 40) + `"}`, []string{"password"}},
		{"email format", "POST", "/auth/register", `{"email":"not-an-email","password":"password123","role":"USER"}`, []string{"email"}},
		{"UUID format in body", "POST", "/orders", `{"items":[{"product_id":"bad-uuid","quantity":1}]}`, []string{"items[0].product_id"}},
		{"concatenated JSON", "POST", "/products", `{"name":"Test","price":1,"stock":1,"category":"test","status":"ACTIVE"} {}`, []string{"request"}},
		{"trailing garbage", "POST", "/products", `{"name":"Test","price":1,"stock":1,"category":"test","status":"ACTIVE"} garbage`, []string{"request"}},
		{"name length", "POST", "/products", `{"name":"` + strings.Repeat("x", 256) + `","price":1,"stock":0,"category":"test","status":"ACTIVE"}`, []string{"name"}},
		{"description length", "POST", "/products", `{"name":"Test","description":"` + strings.Repeat("x", 4001) + `","price":1,"stock":0,"category":"test","status":"ACTIVE"}`, []string{"description"}},
		{"category length", "POST", "/products", `{"name":"Test","price":1,"stock":0,"category":"` + strings.Repeat("x", 101) + `","status":"ACTIVE"}`, []string{"category"}},
		{"product status", "POST", "/products", `{"name":"Test","price":1,"stock":0,"category":"test","status":"UNKNOWN"}`, []string{"status"}},
		{"minimum price", "POST", "/products", `{"name":"Test","price":0.001,"stock":0,"category":"test","status":"ACTIVE"}`, []string{"price"}},
		{"empty items", "POST", "/orders", `{"items":[]}`, []string{"items"}},
		{"too many items", "POST", "/orders", `{"items":[` + strings.TrimSuffix(strings.Repeat(`{"product_id":"10000000-0000-0000-0000-000000000001","quantity":1},`, 51), ",") + `]}`, []string{"items"}},
		{"promo pattern", "POST", "/orders", `{"items":[{"product_id":"10000000-0000-0000-0000-000000000001","quantity":1}],"promo_code":"bad"}`, []string{"promo_code"}},
		{"invalid calendar date", "POST", "/promo-codes", `{"code":"TEST","discount_type":"FIXED_AMOUNT","discount_value":1,"min_order_amount":0,"max_uses":1,"valid_from":"2026-02-31T00:00:00Z","valid_until":"2099-01-01T00:00:00Z","active":true}`, []string{"valid_from"}},
		{"registration cannot grant ADMIN", "POST", "/auth/register", `{"email":"test@example.com","password":"password123","role":"ADMIN"}`, []string{"role"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+pair.AccessToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("expected JSON 400, got %d, %q: %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
			}
			if _, err := uuid.Parse(w.Header().Get("X-Request-Id")); err != nil {
				t.Fatalf("missing or invalid request ID: %v", err)
			}
			var body api.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.ErrorCode != api.VALIDATIONERROR || body.Details == nil {
				t.Fatalf("expected detailed VALIDATION_ERROR: %s", w.Body.String())
			}
			fields := make(map[string]bool)
			violations, ok := (*body.Details)["violations"].([]any)
			if !ok {
				t.Fatalf("missing violations: %s", w.Body.String())
			}
			for _, value := range violations {
				violation := value.(map[string]any)
				if violation["message"] == "" {
					t.Fatal("empty validation message")
				}
				fields[violation["field"].(string)] = true
			}
			for _, field := range test.fields {
				if !fields[field] {
					t.Errorf("missing violation for %s: %s", field, w.Body.String())
				}
			}
		})
	}
}

func TestRoutingErrorsUseContract(t *testing.T) {
	tokens := auth.NewTokenManager("routing-test-secret-long-enough-123", 20*time.Minute, 14*24*time.Hour)
	pair, err := tokens.Issue(auth.Identity{UserID: uuid.New(), Role: "USER"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(nil, tokens, time.Minute).Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path string
		status       int
		code         api.ErrorCode
	}{
		{"GET", "/missing-endpoint", 404, api.ENDPOINTNOTFOUND},
		{"PATCH", "/products", 405, api.METHODNOTALLOWED},
	} {
		r := httptest.NewRequest(test.method, test.path, nil)
		r.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var body api.ErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != test.status || body.ErrorCode != test.code {
			t.Fatalf("%s %s: %d %s (%v)", test.method, test.path, w.Code, w.Body.String(), err)
		}
	}
}
