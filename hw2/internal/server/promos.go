package server

import (
	"context"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/store"
)

func (s *Server) CreatePromoCode(ctx context.Context, request api.CreatePromoCodeRequestObject) (api.CreatePromoCodeResponseObject, error) {
	if _, denied := requireRole(ctx, "SELLER", "ADMIN"); denied != nil {
		body, _ := apiError(denied)
		return api.CreatePromoCode403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	minAmount, ok := store.Cents(request.Body.MinOrderAmount)
	violations := make([]map[string]string, 0)
	if !ok {
		violations = append(violations, map[string]string{"field": "min_order_amount", "message": "must be a finite non-negative amount"})
	}
	if !request.Body.ValidUntil.After(request.Body.ValidFrom) {
		violations = append(violations, map[string]string{"field": "valid_until", "message": "must be after valid_from"})
	}
	if request.Body.DiscountType == api.PERCENTAGE && request.Body.DiscountValue > 100 {
		violations = append(violations, map[string]string{"field": "discount_value", "message": "percentage must not exceed 100"})
	}
	if len(violations) > 0 {
		body := validationBody("request validation failed", map[string]any{"violations": violations})
		return api.CreatePromoCode400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(body)}, nil
	}
	promo, err := s.store.CreatePromoCode(ctx, store.PromoCodeInput{
		Code: request.Body.Code, DiscountType: string(request.Body.DiscountType),
		DiscountValue: request.Body.DiscountValue, MinOrderAmountCents: minAmount,
		MaxUses: request.Body.MaxUses, ValidFrom: request.Body.ValidFrom,
		ValidUntil: request.Body.ValidUntil, Active: request.Body.Active,
	})
	if err != nil {
		body, business := apiError(err)
		if business != nil && business.Code == "PROMO_CODE_ALREADY_EXISTS" {
			return api.CreatePromoCode409JSONResponse{ConflictErrorJSONResponse: api.ConflictErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.CreatePromoCode201JSONResponse(promoResponse(promo)), nil
}

func promoResponse(promo store.PromoCode) api.PromoCodeResponse {
	return api.PromoCodeResponse{
		Id: promo.ID, Code: promo.Code, DiscountType: api.DiscountType(promo.DiscountType),
		DiscountValue: promo.DiscountValue, MinOrderAmount: store.Decimal(promo.MinOrderAmountCents),
		MaxUses: promo.MaxUses, CurrentUses: promo.CurrentUses,
		ValidFrom: promo.ValidFrom, ValidUntil: promo.ValidUntil, Active: promo.Active,
	}
}
