package engine

import "fmt"

// ValidationError represents a client-caused precondition or input failure.
// Handlers should return HTTP 400 for these.
type ValidationError struct {
	msg string
}

func (e *ValidationError) Error() string { return e.msg }

func validationErrorf(format string, args ...any) *ValidationError {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// NotFoundError represents a missing resource. Handlers should return HTTP 404.
type NotFoundError struct {
	msg string
}

func (e *NotFoundError) Error() string { return e.msg }

func notFoundErrorf(format string, args ...any) *NotFoundError {
	return &NotFoundError{msg: fmt.Sprintf(format, args...)}
}
