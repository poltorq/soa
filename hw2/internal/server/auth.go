package server

import (
	"context"
	"errors"
	"fmt"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/auth"
	"marketplace-hw2/internal/fault"

	"golang.org/x/crypto/bcrypt"
)

func (s *Server) Register(ctx context.Context, request api.RegisterRequestObject) (api.RegisterResponseObject, error) {

	hash, err := bcrypt.GenerateFromPassword([]byte(request.Body.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	user, err := s.store.CreateUser(ctx, string(request.Body.Email), string(hash), string(request.Body.Role))
	if err != nil {
		body, business := apiError(err)
		if business != nil && business.Code == "EMAIL_ALREADY_EXISTS" {
			return api.Register409JSONResponse{ConflictErrorJSONResponse: api.ConflictErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	pair, err := s.tokens.Issue(auth.Identity{UserID: user.ID, Role: user.Role})
	if err != nil {
		return nil, err
	}
	return api.Register201JSONResponse(authResponse(pair)), nil
}

func (s *Server) Login(ctx context.Context, request api.LoginRequestObject) (api.LoginResponseObject, error) {

	user, err := s.store.UserByEmail(ctx, string(request.Body.Email))
	if err != nil {
		body, business := apiError(err)
		if business != nil && business.Code == "INVALID_CREDENTIALS" {
			return api.Login401JSONResponse{UnauthorizedErrorJSONResponse: api.UnauthorizedErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Body.Password)); err != nil {
		body, _ := apiError(fault.New("INVALID_CREDENTIALS", "email or password is invalid"))
		return api.Login401JSONResponse{UnauthorizedErrorJSONResponse: api.UnauthorizedErrorJSONResponse(body)}, nil
	}
	pair, err := s.tokens.Issue(auth.Identity{UserID: user.ID, Role: user.Role})
	if err != nil {
		return nil, err
	}
	return api.Login200JSONResponse(authResponse(pair)), nil
}

func (s *Server) Refresh(ctx context.Context, request api.RefreshRequestObject) (api.RefreshResponseObject, error) {
	identity, err := s.tokens.ParseRefresh(request.Body.RefreshToken)
	if err != nil {
		body, _ := apiError(fault.New("REFRESH_TOKEN_INVALID", "refresh token is invalid or expired"))
		return api.Refresh401JSONResponse{UnauthorizedErrorJSONResponse: api.UnauthorizedErrorJSONResponse(body)}, nil
	}
	user, err := s.store.UserByID(ctx, identity.UserID)
	if err != nil {
		body, business := apiError(err)
		if business != nil {
			return api.Refresh401JSONResponse{UnauthorizedErrorJSONResponse: api.UnauthorizedErrorJSONResponse(body)}, nil
		}
		return nil, err
	}
	pair, err := s.tokens.Issue(auth.Identity{UserID: user.ID, Role: user.Role})
	if err != nil {
		return nil, err
	}
	return api.Refresh200JSONResponse(authResponse(pair)), nil
}

func authResponse(pair auth.Pair) api.AuthResponse {
	return api.AuthResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    api.Bearer,
		ExpiresIn:    pair.ExpiresIn,
	}
}

func tokenFault(err error) *fault.Error {
	if errors.Is(err, auth.ErrExpiredToken) {
		return fault.New("TOKEN_EXPIRED", "access token has expired")
	}
	return fault.New("TOKEN_INVALID", "access token is invalid")
}
