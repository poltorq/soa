package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/auth"
	"marketplace-hw2/internal/fault"
	"marketplace-hw2/internal/store"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5"
	requestvalidator "github.com/oapi-codegen/nethttp-middleware"
)

type Server struct {
	store     *store.Store
	tokens    *auth.TokenManager
	rateLimit time.Duration
}

func New(store *store.Store, tokens *auth.TokenManager, rateLimit time.Duration) *Server {
	return &Server{store: store, tokens: tokens, rateLimit: rateLimit}
}

func (s *Server) Handler() (http.Handler, error) {
	spec, err := api.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load embedded OpenAPI specification: %w", err)
	}
	// Validate requests on any host.
	spec.Servers = nil
	validator := requestvalidator.OapiRequestValidatorWithOptions(spec, &requestvalidator.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			MultiError:         true,
		},
		ErrorHandlerWithOpts: handleValidationError,
	})
	strict := api.NewStrictHandlerWithOptions(s, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  validationErrorHandler,
		ResponseErrorHandlerFunc: internalErrorHandler,
	})

	router := chi.NewRouter()
	router.Use(maxBodyBytes(1 << 20))
	router.Use(requestLogMiddleware())
	router.Use(authMiddleware(s.tokens))
	router.Use(validator)
	api.HandlerWithOptions(strict, api.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: validationErrorHandler,
	})
	return router, nil
}

func (s *Server) GetHealth(ctx context.Context, _ api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	if err := s.store.Ping(ctx); err != nil {
		return nil, err
	}
	return api.GetHealth200JSONResponse{Status: "ok", Service: "marketplace-api"}, nil
}

func requireRole(ctx context.Context, roles ...string) (auth.Identity, *fault.Error) {
	identity, ok := identityFromContext(ctx)
	if !ok {
		return auth.Identity{}, fault.New("TOKEN_INVALID", "authentication is required")
	}
	for _, role := range roles {
		if identity.Role == role {
			return identity, nil
		}
	}
	return auth.Identity{}, fault.New("ACCESS_DENIED", "role does not permit this operation")
}
