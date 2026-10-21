package store

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Role         string
}

type Product struct {
	ID          uuid.UUID
	Name        string
	Description *string
	PriceCents  int64
	Stock       int
	Category    string
	Status      string
	SellerID    uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProductInput struct {
	Name        string
	Description *string
	PriceCents  int64
	Stock       int
	Category    string
	Status      string
}

type ProductFilter struct {
	Page     int
	Size     int
	Status   *string
	Category *string
}

type OrderItemInput struct {
	ProductID uuid.UUID
	Quantity  int
}

type OrderItem struct {
	ID                uuid.UUID
	ProductID         uuid.UUID
	Quantity          int
	PriceAtOrderCents int64
}

type Order struct {
	ID                  uuid.UUID
	UserID              uuid.UUID
	Status              string
	PromoCodeID         *uuid.UUID
	PromoCode           *string
	Items               []OrderItem
	TotalAmountCents    int64
	DiscountAmountCents int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type PromoCodeInput struct {
	Code                string
	DiscountType        string
	DiscountValue       float64
	MinOrderAmountCents int64
	MaxUses             int
	ValidFrom           time.Time
	ValidUntil          time.Time
	Active              bool
}

type PromoCode struct {
	ID                  uuid.UUID
	Code                string
	DiscountType        string
	DiscountValue       float64
	MinOrderAmountCents int64
	MaxUses             int
	CurrentUses         int
	ValidFrom           time.Time
	ValidUntil          time.Time
	Active              bool
}
