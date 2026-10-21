package store

import (
	"fmt"
	"math"
	"time"

	"marketplace-hw2/internal/fault"

	"github.com/google/uuid"
)

func validTransition(current, next string) bool {
	allowed := map[string]string{
		"CREATED":         "PAYMENT_PENDING",
		"PAYMENT_PENDING": "PAID",
		"PAID":            "SHIPPED",
		"SHIPPED":         "COMPLETED",
	}
	expected, exists := allowed[current]
	return exists && expected == next
}

func validateUniqueItems(items []OrderItemInput) error {
	seen := make(map[uuid.UUID]struct{}, len(items))
	for i, item := range items {
		if _, exists := seen[item.ProductID]; exists {
			return fault.WithDetails("VALIDATION_ERROR", "order contains duplicate products", map[string]any{
				"violations": []map[string]any{{
					"field":   fmt.Sprintf("items[%d].product_id", i),
					"message": "product_id must be unique within an order",
				}},
			})
		}
		seen[item.ProductID] = struct{}{}
	}
	return nil
}

func validateProducts(items []OrderItemInput, products map[uuid.UUID]Product, restored map[uuid.UUID]int) error {
	insufficient := make([]map[string]any, 0)
	for _, item := range items {
		product, exists := products[item.ProductID]
		if !exists {
			return fault.WithDetails("PRODUCT_NOT_FOUND", "product not found", map[string]any{
				"product_id": item.ProductID.String(),
			})
		}
		if product.Status != "ACTIVE" {
			return fault.WithDetails("PRODUCT_INACTIVE", "product is not active", map[string]any{
				"product_id": item.ProductID.String(),
			})
		}
		available := product.Stock + restored[item.ProductID]
		if available < item.Quantity {
			insufficient = append(insufficient, map[string]any{
				"product_id": item.ProductID.String(),
				"requested":  item.Quantity,
				"available":  available,
			})
		}
	}
	if len(insufficient) > 0 {
		return fault.WithDetails("INSUFFICIENT_STOCK", "insufficient stock for one or more products", map[string]any{
			"products": insufficient,
		})
	}
	return nil
}

func preserveOrderPrices(products map[uuid.UUID]Product, previous []OrderItem) {
	for _, item := range previous {
		product := products[item.ProductID]
		product.PriceCents = item.PriceAtOrderCents
		products[item.ProductID] = product
	}
}

func orderSubtotal(items []OrderItemInput, products map[uuid.UUID]Product) (int64, error) {
	var total int64
	for i, item := range items {
		price := products[item.ProductID].PriceCents
		if item.Quantity < 1 || price < 1 || price > (math.MaxInt64-total)/int64(item.Quantity) {
			return 0, fault.WithDetails("VALIDATION_ERROR", "order subtotal exceeds the supported monetary range", map[string]any{
				"violations": []map[string]string{{
					"field":   fmt.Sprintf("items[%d].quantity", i),
					"message": "order subtotal exceeds the supported integer range",
				}},
			})
		}
		total += price * int64(item.Quantity)
	}
	return total, nil
}

func validateOrderAmounts(subtotal, discount int64) error {
	if subtotal-discount > MaxAmountCents || discount > MaxAmountCents {
		return fault.WithDetails("VALIDATION_ERROR", "order amounts exceed the supported monetary range", map[string]any{
			"violations": []map[string]string{{
				"field": "items", "message": "total_amount and discount_amount must not exceed 9999999999.99",
			}},
		})
	}
	return nil
}

func validateNewPromo(promo PromoCode, subtotal int64, now time.Time) error {
	if !promo.Active || promo.CurrentUses >= promo.MaxUses || now.Before(promo.ValidFrom) || now.After(promo.ValidUntil) {
		return fault.New("PROMO_CODE_INVALID", "promo code is inactive, exhausted or outside its validity period")
	}
	if subtotal < promo.MinOrderAmountCents {
		return fault.WithDetails("PROMO_CODE_MIN_AMOUNT", "order amount is below the promo code minimum", map[string]any{
			"minimum_amount": Decimal(promo.MinOrderAmountCents),
			"order_amount":   Decimal(subtotal),
		})
	}
	return nil
}

func promoDiscount(promo PromoCode, subtotal int64) int64 {
	if promo.DiscountType == "FIXED_AMOUNT" {
		fixed, _ := Cents(promo.DiscountValue)
		return min(fixed, subtotal)
	}
	basisPoints := int64(math.Round(promo.DiscountValue * 100))
	if basisPoints > 7000 {
		return 0
	}
	// Divide first to avoid overflowing before rounding to cents.
	discount := subtotal/10000*basisPoints + (subtotal%10000*basisPoints+5000)/10000
	return discount
}
