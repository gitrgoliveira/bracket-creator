package engine

import (
	"errors"
	"fmt"
)

// ValidationError represents a client-caused precondition or input failure.
// Handlers typically return HTTP 400, but may return HTTP 409 when the
// failure is a state conflict (e.g. reinstatement of a non-reinstateable
// competitor).
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

func validationErrorf(format string, args ...any) *ValidationError {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// NotFoundError represents a missing resource. Handlers should return HTTP 404.
type NotFoundError struct {
	Msg string
}

func (e *NotFoundError) Error() string { return e.Msg }

func notFoundErrorf(format string, args ...any) *NotFoundError {
	return &NotFoundError{Msg: fmt.Sprintf(format, args...)}
}

// ErrDecisionLocked is returned when a decision-overwrite (kiken-undo
// or similar) is attempted on a match whose participants have started
// a subsequent match. Handlers should return HTTP 409.
//
// T103, CHK024.
var ErrDecisionLocked = errors.New("decision locked: a subsequent match has started")
