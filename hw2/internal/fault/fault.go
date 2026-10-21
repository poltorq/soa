package fault

import "fmt"

type Error struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func WithDetails(code, message string, details map[string]any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}
