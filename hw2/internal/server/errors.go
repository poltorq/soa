package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/fault"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	requestvalidator "github.com/oapi-codegen/nethttp-middleware"
)

func handleValidationError(_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts requestvalidator.ErrorHandlerOpts) {
	switch {
	case errors.Is(err, routers.ErrMethodNotAllowed):
		writeError(w, http.StatusMethodNotAllowed, api.ErrorResponse{
			ErrorCode: api.METHODNOTALLOWED,
			Message:   "method not allowed",
		})
	case opts.StatusCode == http.StatusNotFound:
		writeError(w, http.StatusNotFound, api.ErrorResponse{
			ErrorCode: api.ENDPOINTNOTFOUND,
			Message:   "endpoint not found",
		})
	default:
		validationErrorHandler(w, r, err)
	}
}

func apiError(err error) (api.ErrorResponse, *fault.Error) {
	var business *fault.Error
	if !errors.As(err, &business) {
		return api.ErrorResponse{}, nil
	}
	var details *map[string]any
	if business.Details != nil {
		details = &business.Details
	}
	return api.ErrorResponse{
		ErrorCode: api.ErrorCode(business.Code),
		Message:   business.Message,
		Details:   details,
	}, business
}

func validationBody(message string, details map[string]any) api.ErrorResponse {
	return api.ErrorResponse{
		ErrorCode: api.VALIDATIONERROR,
		Message:   message,
		Details:   &details,
	}
}

func validationErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	details := validationDetails(err)
	writeError(w, http.StatusBadRequest, validationBody("request validation failed", details))
}

func validationDetails(err error) map[string]any {
	violations := make([]map[string]string, 0)
	var collect func(error, string)
	collect = func(err error, field string) {
		switch current := err.(type) {
		case openapi3.MultiError:
			for _, nested := range current {
				collect(nested, field)
			}
			return
		case *openapi3filter.RequestError:
			if current.Parameter != nil {
				field = current.Parameter.Name
			}
			if current.Err != nil {
				collect(current.Err, field)
				return
			}
		case *openapi3.SchemaError:
			if path := validationPath(current.JSONPointer()); path != "" {
				field = path
			}
			violations = append(violations, map[string]string{"field": field, "message": current.Reason})
			return
		case *api.InvalidParamFormatError:
			field = current.ParamName
		default:
			if nested := errors.Unwrap(err); nested != nil {
				collect(nested, field)
				return
			}
		}
		violations = append(violations, map[string]string{"field": field, "message": err.Error()})
	}
	collect(err, "request")
	return map[string]any{"violations": violations}
}

func validationPath(pointer []string) string {
	var path strings.Builder
	for _, part := range pointer {
		if _, err := strconv.Atoi(part); err == nil {
			path.WriteString("[" + part + "]")
		} else {
			if path.Len() > 0 {
				path.WriteByte('.')
			}
			path.WriteString(part)
		}
	}
	return path.String()
}

func internalErrorHandler(w http.ResponseWriter, _ *http.Request, _ error) {
	writeError(w, http.StatusInternalServerError, api.ErrorResponse{
		ErrorCode: api.INTERNALERROR,
		Message:   "internal server error",
	})
}

func writeError(w http.ResponseWriter, status int, body api.ErrorResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
