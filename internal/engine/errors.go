package engine

import (
	"errors"
	"fmt"
	"strings"
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

// ErrDownstreamKnockoutPlayed is the sentinel matched by errors.Is for
// DownstreamKnockoutPlayedError. Handlers should return HTTP 409.
//
// bc-kcdg.
var ErrDownstreamKnockoutPlayed = errors.New("downstream knockout match already played")

// DownstreamKnockoutPlayedError is returned when correcting a completed
// bracket match (via a score write or OverrideBracketWinner) would change
// the winner already propagated into a downstream match -- the next round,
// or the bronze/3rd-place match a semifinal also feeds -- that carries a
// result of its own. propagateBracketWinner repaints a downstream slot's
// SideA/SideB unconditionally; without this guard the repaint left that
// match displaying a competitor its own recorded Winner/score/status
// disagreed with. Refused by default (operator ruling); the caller may
// retry with ForceOptions.Force set, which applies the correction and
// requeues exactly the matches named here. That is ONE match in every case
// but one: a semifinal feeds both the final and the bronze match, and when
// both are closed both are named and both are cleared together, because the
// second could never be asked about separately (see
// newDownstreamKnockoutPlayedError). Never a deeper round: those arrive on
// their own write, when the re-fought result propagates into them.
//
// Never raised for a matchWriteRestore (a K3 rollback replaying a trusted
// snapshot), and never for a downstream slot merely auto-completed by a bye
// (see bracketMatchCarriesOwnResult).
type DownstreamKnockoutPlayedError struct {
	// MatchID is the id of the match being corrected.
	MatchID string
	// BlockingMatchID is the FIRST blocking match (bronze before the next
	// round), kept as the single-value form every existing consumer reads.
	BlockingMatchID string
	// BlockingMatchIDs is every match this correction is blocked on, one hop
	// down and closed with a result of its own. Almost always one; a semifinal
	// feeds both the final and the bronze match, so it can be two, and both are
	// cleared by the one confirmation that names them (see
	// newDownstreamKnockoutPlayedError for why they cannot be split).
	BlockingMatchIDs []string
	// Displaced is the corrected match's currently stored winner name -- the
	// competitor the correction would knock out of BlockingMatchID.
	Displaced string
}

func (e *DownstreamKnockoutPlayedError) Error() string {
	blocked := e.BlockingMatchID
	if len(e.BlockingMatchIDs) > 1 {
		blocked = strings.Join(e.BlockingMatchIDs, " and ")
	}
	return fmt.Sprintf("correcting match %q would change the winner already propagated into %s, which has recorded its own result; this would displace %q without updating that result. Retry with forceDownstreamReopen to apply the correction and send %s back to the queue to be fought again",
		e.MatchID, blocked, e.Displaced, blocked)
}

func (e *DownstreamKnockoutPlayedError) Is(target error) bool {
	return target == ErrDownstreamKnockoutPlayed
}

// ErrSwissExportUnsupported is returned by Engine.ExportCompetitionXlsx (and
// therefore by ExportTournamentWorkbooks), and is aliased by
// internal/export.ErrSwissExportUnsupported for BuildResultsWorkbook. Swiss
// has no pools and no static bracket -- results are per-round pairings plus a
// running standings table -- so NEITHER the blank-template bracket export nor
// the results-workbook export has anything to render; the message below is
// shared by both and deliberately does not call either path a "bracket
// export". Handlers should return HTTP 422 with the sentinel's message,
// which points operators at the one place Swiss results ARE available today
// (the live standings view) rather than just naming what does not work.
// Lives here (engine), not in internal/export, because internal/export
// imports internal/engine and the reverse would be an import cycle. A
// dedicated Swiss export sheet is tracked as follow-up work (mp-4n9n); do not
// attempt to implement it here.
var ErrSwissExportUnsupported = errors.New("not yet implemented: Swiss competitions have no static bracket to export; use the live standings view instead")

// ErrBracketDrawMismatch is returned by RenderCompetitionWorkbook (and
// therefore by Engine.ExportCompetitionXlsx, internal/export.
// BuildResultsWorkbook, and ExportTournamentWorkbooks) when the persisted
// bracket carries knockout content -- a third-place bout, or any real round
// match (see bracketHasKnockoutContent, workbook.go) -- that cannot be
// re-derived from the competition's CURRENT settings. This happens when a
// setting the draw depends on (e.g. ExtraQualifiers) changes after the
// bracket was built, so the stored bracket and a freshly-derived draw
// disagree. Rendering anyway would produce a workbook with only the
// disagreeing fragment (a lone 3rd-place block, or no knockout content at
// all) and no way for the operator to tell the rest is missing, so this is
// refused outright rather than rendered partially. Handlers should return
// HTTP 422 with the sentinel's message, which tells the operator what to do
// about it without naming any internal identifier.
var ErrBracketDrawMismatch = errors.New("this competition's stored bracket does not match its current settings, so the knockout stage cannot be exported; discard and regenerate the draw, or restore the settings the bracket was originally built with")
