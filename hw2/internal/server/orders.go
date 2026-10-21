package server

import (
	"context"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/store"
)

func (s *Server) CreateOrder(ctx context.Context, request api.CreateOrderRequestObject) (api.CreateOrderResponseObject, error) {
	identity, denied := requireRole(ctx, "USER", "ADMIN")
	if denied != nil {
		body, _ := apiError(denied)
		return api.CreateOrder403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	order, err := s.store.CreateOrder(ctx, identity.UserID, orderInputs(request.Body.Items), request.Body.PromoCode, s.rateLimit)
	if err != nil {
		body, business := apiError(err)
		if business == nil {
			return nil, err
		}
		switch business.Code {
		case "VALIDATION_ERROR":
			return api.CreateOrder400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(body)}, nil
		case "PRODUCT_NOT_FOUND":
			return api.CreateOrder404JSONResponse{NotFoundErrorJSONResponse: api.NotFoundErrorJSONResponse(body)}, nil
		case "PROMO_CODE_INVALID", "PROMO_CODE_MIN_AMOUNT":
			return api.CreateOrder422JSONResponse{BusinessValidationErrorJSONResponse: api.BusinessValidationErrorJSONResponse(body)}, nil
		case "ORDER_LIMIT_EXCEEDED":
			return api.CreateOrder429JSONResponse{RateLimitErrorJSONResponse: api.RateLimitErrorJSONResponse(body)}, nil
		case "PRODUCT_INACTIVE", "ORDER_HAS_ACTIVE", "INSUFFICIENT_STOCK", "INVALID_STATE_TRANSITION":
			return api.CreateOrder409JSONResponse{ConflictErrorJSONResponse: api.ConflictErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.CreateOrder201JSONResponse(orderResponse(order)), nil
}

func (s *Server) GetOrder(ctx context.Context, request api.GetOrderRequestObject) (api.GetOrderResponseObject, error) {
	identity, denied := requireRole(ctx, "USER", "ADMIN")
	if denied != nil {
		body, _ := apiError(denied)
		return api.GetOrder403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	order, err := s.store.Order(ctx, request.Id, identity.UserID, identity.Role)
	if err != nil {
		body, business := apiError(err)
		if business == nil {
			return nil, err
		}
		switch business.Code {
		case "ORDER_NOT_FOUND":
			return api.GetOrder404JSONResponse{NotFoundErrorJSONResponse: api.NotFoundErrorJSONResponse(body)}, nil
		case "ORDER_OWNERSHIP_VIOLATION", "ACCESS_DENIED":
			return api.GetOrder403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.GetOrder200JSONResponse(orderResponse(order)), nil
}

func (s *Server) UpdateOrder(ctx context.Context, request api.UpdateOrderRequestObject) (api.UpdateOrderResponseObject, error) {
	identity, denied := requireRole(ctx, "USER", "ADMIN")
	if denied != nil {
		body, _ := apiError(denied)
		return api.UpdateOrder403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	order, err := s.store.UpdateOrder(ctx, request.Id, identity.UserID, identity.Role,
		orderInputs(request.Body.Items), s.rateLimit)
	if err != nil {
		body, business := apiError(err)
		if business == nil {
			return nil, err
		}
		switch business.Code {
		case "VALIDATION_ERROR":
			return api.UpdateOrder400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(body)}, nil
		case "PRODUCT_NOT_FOUND", "ORDER_NOT_FOUND":
			return api.UpdateOrder404JSONResponse{NotFoundErrorJSONResponse: api.NotFoundErrorJSONResponse(body)}, nil
		case "ORDER_OWNERSHIP_VIOLATION", "ACCESS_DENIED":
			return api.UpdateOrder403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
		case "PROMO_CODE_INVALID", "PROMO_CODE_MIN_AMOUNT":
			return api.UpdateOrder422JSONResponse{BusinessValidationErrorJSONResponse: api.BusinessValidationErrorJSONResponse(body)}, nil
		case "ORDER_LIMIT_EXCEEDED":
			return api.UpdateOrder429JSONResponse{RateLimitErrorJSONResponse: api.RateLimitErrorJSONResponse(body)}, nil
		case "PRODUCT_INACTIVE", "INSUFFICIENT_STOCK", "INVALID_STATE_TRANSITION":
			return api.UpdateOrder409JSONResponse{ConflictErrorJSONResponse: api.ConflictErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.UpdateOrder200JSONResponse(orderResponse(order)), nil
}

func (s *Server) CancelOrder(ctx context.Context, request api.CancelOrderRequestObject) (api.CancelOrderResponseObject, error) {
	identity, denied := requireRole(ctx, "USER", "ADMIN")
	if denied != nil {
		body, _ := apiError(denied)
		return api.CancelOrder403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	order, err := s.store.CancelOrder(ctx, request.Id, identity.UserID, identity.Role)
	if err != nil {
		body, business := apiError(err)
		if business == nil {
			return nil, err
		}
		switch business.Code {
		case "ORDER_NOT_FOUND":
			return api.CancelOrder404JSONResponse{NotFoundErrorJSONResponse: api.NotFoundErrorJSONResponse(body)}, nil
		case "ORDER_OWNERSHIP_VIOLATION", "ACCESS_DENIED":
			return api.CancelOrder403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
		case "INVALID_STATE_TRANSITION":
			return api.CancelOrder409JSONResponse{ConflictErrorJSONResponse: api.ConflictErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.CancelOrder200JSONResponse(orderResponse(order)), nil
}

func (s *Server) TransitionOrder(ctx context.Context, request api.TransitionOrderRequestObject) (api.TransitionOrderResponseObject, error) {
	if _, denied := requireRole(ctx, "ADMIN"); denied != nil {
		body, _ := apiError(denied)
		return api.TransitionOrder403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	order, err := s.store.TransitionOrder(ctx, request.Id, string(request.Body.Status))
	if err != nil {
		body, business := apiError(err)
		if business == nil {
			return nil, err
		}
		switch business.Code {
		case "ORDER_NOT_FOUND":
			return api.TransitionOrder404JSONResponse{NotFoundErrorJSONResponse: api.NotFoundErrorJSONResponse(body)}, nil
		case "INVALID_STATE_TRANSITION":
			return api.TransitionOrder409JSONResponse{ConflictErrorJSONResponse: api.ConflictErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.TransitionOrder200JSONResponse(orderResponse(order)), nil
}

func orderInputs(items []api.OrderItemInput) []store.OrderItemInput {
	result := make([]store.OrderItemInput, 0, len(items))
	for _, item := range items {
		result = append(result, store.OrderItemInput{ProductID: item.ProductId, Quantity: item.Quantity})
	}
	return result
}

func orderResponse(order store.Order) api.OrderResponse {
	items := make([]api.OrderItemResponse, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, api.OrderItemResponse{
			Id: item.ID, ProductId: item.ProductID, Quantity: item.Quantity,
			PriceAtOrder: store.Decimal(item.PriceAtOrderCents),
		})
	}
	return api.OrderResponse{
		Id: order.ID, UserId: order.UserID, Status: api.OrderStatus(order.Status),
		PromoCode: order.PromoCode, Items: items,
		TotalAmount:    store.Decimal(order.TotalAmountCents),
		DiscountAmount: store.Decimal(order.DiscountAmountCents),
		CreatedAt:      order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}
}
