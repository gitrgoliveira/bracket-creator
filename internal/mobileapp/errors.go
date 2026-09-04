package mobileapp

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// internalError logs err (with request method + path for context) and returns a
// generic HTTP 500 body, so wrapped internal details, filesystem paths, and
// underlying causes never reach the client. The full error is always preserved in
// the server log for operator diagnostics.
//
// Pass a single SAFE, caller-controlled publicMsg to keep an operator-friendly
// label in the response (e.g. "failed to save participants"); it must NOT embed
// err.Error() or any dynamic internal detail. When omitted or empty, a generic
// "internal error" is returned.
//
// Use this for the catch-all/unexpected 500 path. Specific, user-actionable
// failures should still return an explicit 4xx with their own message.
func internalError(c *gin.Context, err error, publicMsg ...string) {
	log.Printf("mobileapp: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	// A file the operator can repair is the ONE internal failure worth naming.
	// Everything else here is deliberately opaque, but "internal error" on a
	// corrupt competition file tells an organiser mid tournament that scoring
	// has stopped and nothing else -- while the cause, the file and the exact
	// line sit in a server log they are not reading. The detail is a parser's
	// description of syntax, never competitor data, so it is safe to return.
	//
	// Hooked HERE rather than at each call site on purpose: this function is
	// the catch-all every handler already funnels its unexpected failures
	// through, so one branch upgrades all of them, and a future handler that
	// can reach a corrupt file inherits the message without having to know it
	// exists.
	if cf, ok := state.AsCorruptFile(err); ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "a competition data file could not be read",
			"code":   "corrupt_file",
			"file":   cf.File,
			"line":   cf.Line,
			"column": cf.Column,
			"detail": cf.Detail,
		})
		return
	}
	msg := "internal error"
	if len(publicMsg) > 0 && publicMsg[0] != "" {
		msg = publicMsg[0]
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
}

// respondUnexportableCompetitionError asks engine.IsUnexportable whether err
// is one of the sentinels a workbook-export path can fail with (today: Swiss
// has no static bracket to export, or the stored bracket no longer matches
// the competition's current settings) and, if so, writes
// {"error": err.Error()} as an HTTP 422
// and reports true; the caller must return immediately when this returns
// true. Both are state conflicts the operator can resolve (regenerate the
// draw, restore the settings, or use the live standings view), not server
// faults, hence 422 rather than 500.
//
// Shared by the blank-template export route (GET .../export,
// handlers_competition.go) and the results-archive export route (GET
// .../export-results, handlers_export.go) so the same two-sentinel mapping
// does not drift into two hand-copied bodies -- mirrors
// respondRosterWriteError's shape below for the same reason.
//
// The set itself is NOT re-listed here: engine.IsUnexportable owns it, so a
// third sentinel is added once rather than in two packages that fail to
// compile-check each other. export.ErrSwissExportUnsupported is a plain alias
// of engine.ErrSwissExportUnsupported (see that var's doc comment), so this
// matches errors produced by either export path.
func respondUnexportableCompetitionError(c *gin.Context, err error) bool {
	if !engine.IsUnexportable(err) {
		return false
	}
	c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	return true
}

// classifyRosterWriteError maps one of the participant-roster write sentinel
// errors -- returned by Store.AddParticipant, Store.SaveParticipants,
// Store.UpdateParticipant, Store.BulkCheckIn, and every other write that
// funnels through saveParticipantsNoLock -- to the HTTP status this package
// answers with for it. ok is false when err does not match any of them,
// leaving the caller free to check its own site-specific sentinels (e.g.
// state.ErrCompetitionNotInSetup) before falling back to internalError.
//
// This is the ONE place these four map to a status; every participant-write
// call site should classify through here (directly, or via
// respondRosterWriteError) rather than hand-copying its own
// errors.Is(...)-then-c.JSON chain. Before this existed, ErrBlankDojo's own
// doc comment promised "a per-field 400 naming the offending row" but six
// call sites hand-copied that mapping while three check-in paths (PUT/DELETE
// .../checkin, POST .../checkin-bulk) never checked it at all and fell
// through to a generic 500 -- exactly the class of drift a single shared
// classifier prevents.
func classifyRosterWriteError(err error) (status int, ok bool) {
	switch {
	case errors.Is(err, state.ErrParticipantNotFound):
		return http.StatusNotFound, true
	case errors.Is(err, state.ErrDuplicateName):
		return http.StatusConflict, true
	case errors.Is(err, state.ErrReservedName):
		return http.StatusBadRequest, true
	case errors.Is(err, state.ErrBlankDojo):
		return http.StatusBadRequest, true
	default:
		return 0, false
	}
}

// respondRosterWriteError classifies err via classifyRosterWriteError and, if
// it matches, writes {"error": err.Error()} at the classified status and
// reports true; the caller must return immediately when this returns true.
// It reports false, writing nothing to c, for any other error, so a call
// site can chain its own site-specific handling (a sentinel this classifier
// doesn't know, then internalError) after it.
//
// err.Error() is used verbatim for every matched case -- in particular NOT
// errors.Unwrap(err).Error() for ErrDuplicateName: the duplicate-team-name
// wrap is fmt.Errorf("%w: %w", ...), whose multi-error type implements
// Unwrap() []error, so errors.Unwrap returns nil and a caller dereferencing
// it panics. err.Error() already contains the full, colliding-entry-naming
// message. A call site that needs a DIFFERENT, friendlier message for one
// specific sentinel (self-registration overrides ErrDuplicateName's) must
// check that sentinel itself BEFORE calling respondRosterWriteError, since
// neither this function nor classifyRosterWriteError lets a caller override
// a matched message.
func respondRosterWriteError(c *gin.Context, err error) bool {
	status, ok := classifyRosterWriteError(err)
	if !ok {
		return false
	}
	c.JSON(status, gin.H{"error": err.Error()})
	return true
}
