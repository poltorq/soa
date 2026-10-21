package store

import (
	"errors"
	"math"
	"testing"
	"time"

	"marketplace-hw2/internal/fault"

	"github.com/google/uuid"
)

func TestCents(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  int64
		ok    bool
	}{
		{name: "whole", value: 12, want: 1200, ok: true},
		{name: "rounds cents", value: 12.345, want: 1235, ok: true},
		{name: "negative", value: -1, ok: false},
		{name: "nan", value: math.NaN(), ok: false},
		{name: "infinity", value: math.Inf(1), ok: false},
		{name: "maximum amount", value: 9999999999.99, want: MaxAmountCents, ok: true},
		{name: "above database limit", value: 10000000000, ok: false},
		{name: "above integer limit", value: 1e18, ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := Cents(test.value)
			if got != test.want || ok != test.ok {
				t.Fatalf("Cents(%v) = (%d, %v), want (%d, %v)", test.value, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestValidateUniqueItems(t *testing.T) {
	id := uuid.New()
	err := validateUniqueItems([]OrderItemInput{{ProductID: id, Quantity: 1}, {ProductID: id, Quantity: 2}})
	var business *fault.Error
	if !errors.As(err, &business) || business.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestValidateProductsReportsAllInsufficientProducts(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	products := map[uuid.UUID]Product{
		first:  {ID: first, Status: "ACTIVE", Stock: 1},
		second: {ID: second, Status: "ACTIVE", Stock: 2},
	}
	err := validateProducts([]OrderItemInput{
		{ProductID: first, Quantity: 3},
		{ProductID: second, Quantity: 4},
	}, products, nil)
	var business *fault.Error
	if !errors.As(err, &business) || business.Code != "INSUFFICIENT_STOCK" {
		t.Fatalf("expected INSUFFICIENT_STOCK, got %v", err)
	}
	items, ok := business.Details["products"].([]map[string]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected details for two products, got %#v", business.Details)
	}
}

func TestPromoDiscount(t *testing.T) {
	tests := []struct {
		name     string
		promo    PromoCode
		subtotal int64
		want     int64
	}{
		{name: "percentage", promo: PromoCode{DiscountType: "PERCENTAGE", DiscountValue: 10}, subtotal: 10000, want: 1000},
		{name: "seventy percent allowed", promo: PromoCode{DiscountType: "PERCENTAGE", DiscountValue: 70}, subtotal: 10000, want: 7000},
		{name: "above seventy percent gives full price", promo: PromoCode{DiscountType: "PERCENTAGE", DiscountValue: 70.01}, subtotal: 10000, want: 0},
		{name: "ninety percent gives full price", promo: PromoCode{DiscountType: "PERCENTAGE", DiscountValue: 90}, subtotal: 10000, want: 0},
		{name: "percentage rounds to cents", promo: PromoCode{DiscountType: "PERCENTAGE", DiscountValue: 10}, subtotal: 105, want: 11},
		{name: "large subtotal has no intermediate overflow", promo: PromoCode{DiscountType: "PERCENTAGE", DiscountValue: 50}, subtotal: 50_000_000_000_000_000, want: 25_000_000_000_000_000},
		{name: "fixed", promo: PromoCode{DiscountType: "FIXED_AMOUNT", DiscountValue: 5}, subtotal: 10000, want: 500},
		{name: "fixed cannot make total negative", promo: PromoCode{DiscountType: "FIXED_AMOUNT", DiscountValue: 500}, subtotal: 10000, want: 10000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := promoDiscount(test.promo, test.subtotal); got != test.want {
				t.Fatalf("promoDiscount() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestUpdatePricesPreserveExistingSnapshots(t *testing.T) {
	existing, added := uuid.New(), uuid.New()
	products := map[uuid.UUID]Product{
		existing: {ID: existing, PriceCents: 20000, Stock: 8},
		added:    {ID: added, PriceCents: 30000, Stock: 5},
	}
	preserveOrderPrices(products, []OrderItem{{ProductID: existing, PriceAtOrderCents: 10000}})
	items := []OrderItemInput{{ProductID: existing, Quantity: 2}, {ProductID: added, Quantity: 1}}
	subtotal, err := orderSubtotal(items, products)
	if err != nil || subtotal != 50000 {
		t.Fatalf("updated subtotal = %d, %v; want 50000", subtotal, err)
	}
	if products[existing].Stock != 8 || products[added].PriceCents != 30000 {
		t.Fatalf("stock and newly added product price must be preserved: %#v", products)
	}
}

func TestOrderSubtotalBounds(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	products := map[uuid.UUID]Product{
		first:  {PriceCents: MaxAmountCents},
		second: {PriceCents: 1},
	}
	subtotal, err := orderSubtotal([]OrderItemInput{{ProductID: first, Quantity: 1}}, products)
	if err != nil || subtotal != MaxAmountCents {
		t.Fatalf("maximum subtotal = %d, %v", subtotal, err)
	}
	for _, items := range [][]OrderItemInput{
		{{ProductID: first, Quantity: 2}},
		{{ProductID: first, Quantity: 1}, {ProductID: second, Quantity: 1}},
	} {
		subtotal, err := orderSubtotal(items, products)
		if err != nil {
			t.Fatal(err)
		}
		err = validateOrderAmounts(subtotal, 0)
		var business *fault.Error
		if !errors.As(err, &business) || business.Code != "VALIDATION_ERROR" || business.Details["violations"] == nil {
			t.Fatalf("expected detailed VALIDATION_ERROR, got %v", err)
		}
	}
	if err := validateOrderAmounts(1_200_000_000_000, 600_000_000_000); err != nil {
		t.Fatalf("representable discounted order must be accepted: %v", err)
	}
	if err := validateOrderAmounts(1_500_000_000_000, 1_050_000_000_000); err == nil {
		t.Fatal("discount must also fit NUMERIC(12,2)")
	}
}

func TestValidateNewPromo(t *testing.T) {
	now := time.Now()
	valid := PromoCode{Active: true, MaxUses: 2, CurrentUses: 1, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), MinOrderAmountCents: 1000}
	if err := validateNewPromo(valid, 1000, now); err != nil {
		t.Fatalf("expected valid promo, got %v", err)
	}
	valid.CurrentUses = 2
	if err := validateNewPromo(valid, 1000, now); err == nil {
		t.Fatal("expected exhausted promo to fail")
	}
}

func TestValidTransition(t *testing.T) {
	tests := []struct {
		current string
		next    string
		valid   bool
	}{
		{current: "CREATED", next: "PAYMENT_PENDING", valid: true},
		{current: "PAYMENT_PENDING", next: "PAID", valid: true},
		{current: "PAID", next: "SHIPPED", valid: true},
		{current: "SHIPPED", next: "COMPLETED", valid: true},
		{current: "CREATED", next: "PAID", valid: false},
		{current: "COMPLETED", next: "CREATED", valid: false},
		{current: "COMPLETED", next: "", valid: false},
		{current: "UNKNOWN", next: "", valid: false},
	}
	for _, test := range tests {
		if got := validTransition(test.current, test.next); got != test.valid {
			t.Errorf("validTransition(%q, %q) = %v, want %v", test.current, test.next, got, test.valid)
		}
	}
}
