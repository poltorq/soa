package server

import (
	"context"
	"fmt"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/store"
)

func (s *Server) ListProducts(ctx context.Context, request api.ListProductsRequestObject) (api.ListProductsResponseObject, error) {
	if _, denied := requireRole(ctx, "USER", "SELLER", "ADMIN"); denied != nil {
		body, _ := apiError(denied)
		return api.ListProducts401JSONResponse{UnauthorizedErrorJSONResponse: api.UnauthorizedErrorJSONResponse(body)}, nil
	}
	page, size := 0, 20
	if request.Params.Page != nil {
		page = *request.Params.Page
	}
	if request.Params.Size != nil {
		size = *request.Params.Size
	}
	var status *string
	if request.Params.Status != nil {
		value := string(*request.Params.Status)
		status = &value
	}
	products, total, err := s.store.Products(ctx, store.ProductFilter{
		Page: page, Size: size, Status: status, Category: request.Params.Category,
	})
	if err != nil {
		return nil, err
	}
	items := make([]api.ProductResponse, 0, len(products))
	for _, product := range products {
		items = append(items, productResponse(product))
	}
	return api.ListProducts200JSONResponse{Items: items, TotalElements: total, Page: page, Size: size}, nil
}

func (s *Server) CreateProduct(ctx context.Context, request api.CreateProductRequestObject) (api.CreateProductResponseObject, error) {
	identity, denied := requireRole(ctx, "SELLER", "ADMIN")
	if denied != nil {
		body, _ := apiError(denied)
		return api.CreateProduct403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	input, err := productInput(request.Body.Name, request.Body.Description, request.Body.Price,
		request.Body.Stock, request.Body.Category, request.Body.Status)
	if err != nil {
		body := validationBody("request validation failed", map[string]any{
			"violations": []map[string]string{{"field": "price", "message": err.Error()}},
		})
		return api.CreateProduct400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(body)}, nil
	}
	product, err := s.store.CreateProduct(ctx, identity.UserID, input)
	if err != nil {
		return nil, err
	}
	return api.CreateProduct201JSONResponse(productResponse(product)), nil
}

func (s *Server) GetProduct(ctx context.Context, request api.GetProductRequestObject) (api.GetProductResponseObject, error) {
	if _, denied := requireRole(ctx, "USER", "SELLER", "ADMIN"); denied != nil {
		body, _ := apiError(denied)
		return api.GetProduct401JSONResponse{UnauthorizedErrorJSONResponse: api.UnauthorizedErrorJSONResponse(body)}, nil
	}
	product, err := s.store.Product(ctx, request.Id)
	if err != nil {
		body, business := apiError(err)
		if business != nil && business.Code == "PRODUCT_NOT_FOUND" {
			return api.GetProduct404JSONResponse{NotFoundErrorJSONResponse: api.NotFoundErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.GetProduct200JSONResponse(productResponse(product)), nil
}

func (s *Server) UpdateProduct(ctx context.Context, request api.UpdateProductRequestObject) (api.UpdateProductResponseObject, error) {
	identity, denied := requireRole(ctx, "SELLER", "ADMIN")
	if denied != nil {
		body, _ := apiError(denied)
		return api.UpdateProduct403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	input, err := productInput(request.Body.Name, request.Body.Description, request.Body.Price,
		request.Body.Stock, request.Body.Category, request.Body.Status)
	if err != nil {
		body := validationBody("request validation failed", map[string]any{
			"violations": []map[string]string{{"field": "price", "message": err.Error()}},
		})
		return api.UpdateProduct400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(body)}, nil
	}
	product, err := s.store.UpdateProduct(ctx, request.Id, identity.UserID, identity.Role, input)
	if err != nil {
		body, business := apiError(err)
		if business == nil {
			return nil, err
		}
		switch business.Code {
		case "VALIDATION_ERROR":
			return api.UpdateProduct400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(body)}, nil
		case "PRODUCT_NOT_FOUND":
			return api.UpdateProduct404JSONResponse{NotFoundErrorJSONResponse: api.NotFoundErrorJSONResponse(body)}, nil
		case "ACCESS_DENIED":
			return api.UpdateProduct403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.UpdateProduct200JSONResponse(productResponse(product)), nil
}

func (s *Server) ArchiveProduct(ctx context.Context, request api.ArchiveProductRequestObject) (api.ArchiveProductResponseObject, error) {
	identity, denied := requireRole(ctx, "SELLER", "ADMIN")
	if denied != nil {
		body, _ := apiError(denied)
		return api.ArchiveProduct403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
	}
	if err := s.store.ArchiveProduct(ctx, request.Id, identity.UserID, identity.Role); err != nil {
		body, business := apiError(err)
		if business == nil {
			return nil, err
		}
		switch business.Code {
		case "PRODUCT_NOT_FOUND":
			return api.ArchiveProduct404JSONResponse{NotFoundErrorJSONResponse: api.NotFoundErrorJSONResponse(body)}, nil
		case "ACCESS_DENIED":
			return api.ArchiveProduct403JSONResponse{ForbiddenErrorJSONResponse: api.ForbiddenErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	return api.ArchiveProduct204Response{}, nil
}

func productInput(name string, description *string, price float64, stock int, category string, status api.ProductStatus) (store.ProductInput, error) {
	priceCents, ok := store.Cents(price)
	if !ok || priceCents < 1 {
		return store.ProductInput{}, fmt.Errorf("price must be a finite value between 0.01 and 9999999999.99")
	}
	return store.ProductInput{
		Name: name, Description: description, PriceCents: priceCents,
		Stock: stock, Category: category, Status: string(status),
	}, nil
}

func productResponse(product store.Product) api.ProductResponse {
	id := product.ID
	createdAt := product.CreatedAt
	updatedAt := product.UpdatedAt
	return api.ProductResponse{
		Id: &id, Name: product.Name, Description: product.Description,
		Price: store.Decimal(product.PriceCents), Stock: product.Stock,
		Category: product.Category, Status: api.ProductStatus(product.Status),
		SellerId: product.SellerID, CreatedAt: &createdAt, UpdatedAt: &updatedAt,
	}
}
