package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/google/uuid"
)

func init() {
	openapi3.DefineStringFormatValidator("email", openapi3.NewCallbackValidator(validateEmail))
	openapi3.DefineStringFormatValidator("uuid", openapi3.NewCallbackValidator(validateUUID))
	openapi3.DefineStringFormatValidator("password", openapi3.NewCallbackValidator(validatePassword))
	openapi3.DefineStringFormatValidator("date-time", openapi3.NewCallbackValidator(validateDateTime))
	openapi3filter.RegisterBodyDecoder("application/json", decodeJSONBody)
}

func validateEmail(value string) error {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		return errors.New("enter an email address without a display name")
	}
	return nil
}

func validateUUID(value string) error {
	if _, err := uuid.Parse(value); err != nil || len(value) != 36 {
		return errors.New("expected a UUID in 8-4-4-4-12 form")
	}
	return nil
}

func validatePassword(value string) error {
	if len(value) > 72 {
		return errors.New("password must not exceed 72 UTF-8 bytes")
	}
	return nil
}

func validateDateTime(value string) error {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Year() < 1 {
		return errors.New("expected an RFC3339 date and time with a year from 0001 to 9999")
	}
	return nil
}

func decodeJSONBody(body io.Reader, _ http.Header, _ *openapi3.SchemaRef, _ openapi3filter.EncodingFn) (any, error) {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Cause: err}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, &openapi3filter.ParseError{
			Kind: openapi3filter.KindInvalidFormat, Cause: errors.New("body must contain exactly one JSON value"),
		}
	}
	return value, nil
}
