package store

import (
	"context"
	"errors"
	"fmt"

	"marketplace-hw2/internal/fault"

	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) CreatePromoCode(ctx context.Context, input PromoCodeInput) (PromoCode, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO promo_codes (
			code, discount_type, discount_value, min_order_amount,
			max_uses, valid_from, valid_until, active
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, code, discount_type, discount_value::text, min_order_amount::text,
			max_uses, current_uses, valid_from, valid_until, active
	`, input.Code, input.DiscountType, input.DiscountValue, decimalString(input.MinOrderAmountCents),
		input.MaxUses, input.ValidFrom, input.ValidUntil, input.Active)
	promo, err := scanPromo(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return PromoCode{}, fault.New("PROMO_CODE_ALREADY_EXISTS", "promo code already exists")
		}
		return PromoCode{}, fmt.Errorf("create promo code: %w", err)
	}
	return promo, nil
}

type promoScanner interface {
	Scan(dest ...any) error
}

func scanPromo(scanner promoScanner) (PromoCode, error) {
	var promo PromoCode
	var discountValue string
	var minAmount string
	if err := scanner.Scan(
		&promo.ID,
		&promo.Code,
		&promo.DiscountType,
		&discountValue,
		&minAmount,
		&promo.MaxUses,
		&promo.CurrentUses,
		&promo.ValidFrom,
		&promo.ValidUntil,
		&promo.Active,
	); err != nil {
		return PromoCode{}, err
	}
	var err error
	promo.MinOrderAmountCents, err = parseDecimal(minAmount)
	if err != nil {
		return PromoCode{}, err
	}
	var discountCents int64
	discountCents, err = parseDecimal(discountValue)
	if err != nil {
		return PromoCode{}, err
	}
	promo.DiscountValue = Decimal(discountCents)
	return promo, nil
}
