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
	// Err is the cause, when there is one worth keeping. Reclassifying an
	// error as a ValidationError otherwise DISCARDS it: the Msg is a string,
	// so any sentinel underneath stops being findable and a caller that
	// wanted to distinguish two client errors has only prose to match on.
	// Nil for the many call sites that raise a ValidationError from scratch.
	Err error
}

func (e *ValidationError) Error() string { return e.Msg }

// Unwrap exposes the cause to errors.Is and errors.As. Safe when Err is nil:
// errors.Unwrap treats a nil return as "no cause", which is what the
// constructed-from-scratch case means.
func (e *ValidationError) Unwrap() error { return e.Err }

func validationErrorf(format string, args ...any) *ValidationError {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// wrapValidationErrorf is validationErrorf for a cause being RECLASSIFIED, so
// the message reads for the operator while the original error stays reachable
// underneath. Use it whenever the error being converted carries a sentinel.
func wrapValidationErrorf(cause error, format string, args ...any) *ValidationError {
	return &ValidationError{Msg: fmt.Sprintf(format, args...), Err: cause}
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

// ErrDownstreamKnockoutScored is the sentinel matched by errors.Is for
// DownstreamKnockoutScoredError. Handlers should return HTTP 409.
//
// mp-e2k1.
var ErrDownstreamKnockoutScored = errors.New("downstream knockout match already scored")

// DownstreamKnockoutScoredError is returned when a pool re-score would
// change a pool finisher who has already been consumed by a started or
// completed knockout (bracket) match. The operator must reset the
// knockout match first before correcting the pool result.
//
// mp-e2k1.
type DownstreamKnockoutScoredError struct {
	// Pool is the name of the pool whose re-score was rejected.
	Pool string
	// Finisher is the name of the pool finisher whose bracket placement
	// would be displaced by the re-score.
	Finisher string
	// MatchID is the ID of the downstream knockout match that has already
	// been started (running or completed) with the current finisher as a side.
	MatchID string
}

func (e *DownstreamKnockoutScoredError) Error() string {
	return fmt.Sprintf("pool %q re-score rejected: finisher %q is already in a started knockout match %q, reset that match first", e.Pool, e.Finisher, e.MatchID)
}

func (e *DownstreamKnockoutScoredError) Is(target error) bool {
	return target == ErrDownstreamKnockoutScored
}
