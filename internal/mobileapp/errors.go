package mobileapp

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// respondEngineError classifies err against the two typed engine sentinels
// every handler in this package already checks by hand -- *engine.NotFoundError
// (404) and *engine.ValidationError (400) -- and falls back to internalError
// (500) for anything else. This is the ONE place that maps those two types to
// a status; call it instead of hand-copying the same three-way switch at a new
// or existing call site (PR #416 finding 1).
func respondEngineError(c *gin.Context, err error) {
	var notFound *engine.NotFoundError
	var validation *engine.ValidationError
	switch {
	case errors.As(err, &notFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.As(err, &validation):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		internalError(c, err)
	}
}

// reasonHumanForBarredCompetitor builds the ONE operator sentence for a 409
// ineligible_competitor / already_ineligible refusal (bc-rawm acceptance:
// "None of the messages reaches the operator raw: each names the match in
// operator terms and says what to do"): the barred competitor's name, the
// match they were barred IN (its operator label), and the remedy -- record
// the default win for the OPPONENT of thisMatchID (the match the operator
// was trying to start/decide when the refusal fired), or reinstate for a
// kiken-injury. Falls back to domain.ResolveReasonHuman(reason) whenever any
// piece cannot be resolved, so the operator is never shown nothing.
//
// Both names are read off thisMatchID's OWN stored sides
// (store.MatchSidesByID) rather than a separate roster lookup: the barred
// competitor is provably one of thisMatchID's two sides in every producer of
// these two errors -- StartMatchTx/checkEligibilityExcludingMatch resolve
// playerID FROM this match's own ids (engine.BarredSides against
// matchSideParticipantIDs' return), and checkConcurrentIneligibility's loser
// is this match's own decisionBy side -- so playerID always equals one of
// sideAID/sideBID when it can be resolved at all. An id-unstamped row (a
// bye, an unresolved feeder, an unrepaired legacy row) degrades to the
// fallback rather than guessing which side the id names.
//
// barringMatchID/decision come from IneligibleCompetitorError.MatchID/
// Decision or AlreadyIneligibleError.MatchID/Decision (engine.BarredSides'
// two producers, eligibility.go); thisMatchID is always the match id in the
// URL the handler is answering for.
func reasonHumanForBarredCompetitor(store CompetitionStore, compID, thisMatchID, playerID, reason, barringMatchID, decision string) string {
	fallback := domain.ResolveReasonHuman(reason)
	// No barring match to name at all (a status set directly via
	// POST /competitor-status, with no matchId): OperatorMatchLabel would
	// degrade an empty id to an empty label, producing a sentence naming no
	// match ("withdrew in  and cannot fight again"). Prefer the translated
	// fallback; ResolveReasonHuman's own contract is to return "" rather
	// than echo its input so the CALLER can decide whether to fall back to
	// the raw reason (its doc comment), so an untranslatable reason here
	// still surfaces SOMETHING rather than a silently empty reasonHuman.
	if barringMatchID == "" {
		if fallback != "" {
			return fallback
		}
		return reason
	}
	sideA, sideB, sideAID, sideBID, found, err := store.MatchSidesByID(compID, thisMatchID)
	if err != nil || !found {
		return fallback
	}
	var name, opponent string
	switch playerID {
	case sideAID:
		name, opponent = sideA, sideB
	case sideBID:
		name, opponent = sideB, sideA
	default:
		return fallback
	}
	if name == "" {
		return fallback
	}
	comp, err := store.LoadCompetition(compID)
	if err != nil || comp == nil {
		return fallback
	}
	// A bracket-load failure degrades the LABEL (OperatorMatchLabel falls
	// back to the bare match id with a nil bracket) rather than the whole
	// sentence: the barring match is very unlikely to be a knockout one
	// anyway (a withdrawal is almost always recorded in the pool phase), and
	// even a bare id beats the generic fallback losing the name/remedy too.
	bracket, _ := store.LoadBracket(compID)
	label := engine.OperatorMatchLabel(comp, bracket, barringMatchID)
	return barredCompetitorSentence(name, label, decision, opponent)
}

// bothSidesBarredReasonHuman builds the operator sentence for a match whose
// BOTH sides are already barred (IneligibleCompetitorError.BothSidesBarred,
// bc-cse item 10): unlike a one-sided bar, there is no single opponent to
// hand the default win to, so reasonHumanForBarredCompetitor's remedy
// clauses do not apply -- the fix is upstream of this match entirely
// (correct the earlier withdrawal that barred one of them, or the draw that
// paired two already-barred competitors together).
func bothSidesBarredReasonHuman(store CompetitionStore, compID, matchID string) string {
	const fallback = "Neither competitor can fight: correct the earlier withdrawal or the draw."
	sideA, sideB, _, _, found, err := store.MatchSidesByID(compID, matchID)
	if err != nil || !found || sideA == "" || sideB == "" {
		return fallback
	}
	label := matchLabelOrID(store, compID, matchID)
	return fmt.Sprintf("Neither %s nor %s can fight %s: correct the earlier withdrawal or the draw.", sideA, sideB, label)
}

// matchLabelOrID resolves matchID's operator label within compID, via
// engine.OperatorMatchLabelFromStore (bc-cse/11b: the one load-then-degrade
// body this and Engine.operatorMatchLabel both delegate to). Every call site
// here is building operator-facing prose, never gating logic, so a load
// failure degrades the label rather than the response. compID is the
// match's OWN competition, which for a cross-competition refusal (e.g.
// *engine.CourtBusyError.CompID, court occupancy is tournament-global) is
// NOT necessarily the competition the current request targets.
func matchLabelOrID(store CompetitionStore, compID, matchID string) string {
	return engine.OperatorMatchLabelFromStore(store, compID, matchID)
}

// barredCompetitorSentence is the one place the barred-competitor operator
// sentences are spelled, for each decision that can bar someone
// (kiken/kiken-voluntary, kiken-injury, fusenpai). opponent == "" (the
// current match's other side could not be resolved) drops the remedy clause
// rather than naming nobody; label names the BARRING match (barringMatchID
// in reasonHumanForBarredCompetitor, the one call site), never thisMatchID
// -- the sentence says WHERE the competitor was barred, and the remedy
// (record the default win for opponent) is what to do about THIS match.
func barredCompetitorSentence(name, label, decision, opponent string) string {
	switch decision {
	case string(domain.DecisionKikenInjury):
		if opponent == "" {
			return fmt.Sprintf("%s withdrew injured in %s. Reinstate %s if the doctor allows.", name, label, name)
		}
		return fmt.Sprintf("%s withdrew injured in %s. Reinstate %s if the doctor allows, or record the default win for %s.", name, label, name, opponent)
	case string(domain.DecisionFusenpai):
		if opponent == "" {
			return fmt.Sprintf("%s did not appear for %s and cannot fight again.", name, label)
		}
		return fmt.Sprintf("%s did not appear for %s and cannot fight again. Record the default win for %s.", name, label, opponent)
	default:
		// kiken, kiken-voluntary, and any other/legacy barring decision this
		// app never itself writes (fusensho/daihyosen do not bar anyone, so
		// BarredSides never surfaces one in practice): the withdrawal
		// sentence is the safe default, since every reachable barring
		// decision besides the two cases above IS a kiken variant.
		if opponent == "" {
			return fmt.Sprintf("%s withdrew in %s and cannot fight again.", name, label)
		}
		return fmt.Sprintf("%s withdrew in %s and cannot fight again. Record the default win for %s.", name, label, opponent)
	}
}

// respondIfEngineWriteError composes the sentinel checks a match-write
// handler (score, decision, daihyosen) needs after its engine call: a
// superseded write (200 {"applied":false}), a rejected precondition
// (*engine.ValidationError, 400), and a corrupt overrides.json (422) -- in
// that order, matching the order these were hand-copied in before. Reports
// whether it answered, so the caller's tail collapses to
// `if respondIfEngineWriteError(c, err) { return }; internalError(c, err)`.
//
// Does NOT check *engine.NotFoundError: none of these three write paths can
// reach one at this point in their own flow (the match/competition lookup
// already happened earlier in each handler), so folding it in here would
// silently swallow a future 404 into this function's callers that don't
// separately guard for it. Add it explicitly at the call site if a new write
// path needs it, the way respondEngineError does for read handlers.
func respondIfEngineWriteError(c *gin.Context, err error) bool {
	if respondIfSuperseded(c, err) {
		return true
	}
	if respondIfValidationError(c, err) {
		return true
	}
	if respondIfCorruptOverrides(c, err) {
		return true
	}
	return false
}

// respondIfCorruptOverrides answers a corrupt overrides.json
// (state.ErrCorruptOverrides) with a terminal 422 and reports that it
// handled it, so the caller can return without falling through to its own
// internalError (500) arm.
//
// Every override writer except the load-free repair primitive
// (Store.ResetOverridesForce, used by DELETE .../overrides) loads and
// parses the existing file before saving, so a corrupt file makes them fail
// identically -- and every engine call that reads standings
// (computeStandingsFrom, reached via the pool requalification check,
// LeagueTiebreakCandidates, ChusenStatus) hits the same LoadOverrides
// call underneath. Left unmapped, that surfaces as an opaque 500, which the
// SPA's offline write queue retries forever for the write endpoints
// (mp-q8c6 poisoned-queue pattern) -- a genuinely corrupt file on disk
// would poison the queue rather than surface once. This is state on disk
// the operator CAN repair (DELETE .../overrides, wired to
// Store.ResetOverridesForce), so it is answered the same way
// respondUnexportableCompetitionError answers its own "state conflict, not
// a server fault" cases: a terminal 422 naming the file, never a 500.
//
// Shared by every handler whose engine call can reach LoadOverrides: the
// score handler, the decision handler, the daihyosen add/remove handlers,
// the league-tiebreak candidates/generate handlers, and the chusen-
// candidates handler. Before this was extracted, only the score handler
// had this mapping and every sibling call site fell through to a 500 for
// the identical failure.
func respondIfCorruptOverrides(c *gin.Context, err error) bool {
	if !errors.Is(err, state.ErrCorruptOverrides) {
		return false
	}
	c.JSON(http.StatusUnprocessableEntity, gin.H{
		"error": "overrides.json could not be read; ask an administrator to reset overrides for this competition",
		"code":  "corrupt_overrides",
	})
	return true
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

// respondIfDownstreamKnockoutPlayed answers engine.DownstreamKnockoutPlayedError
// (bc-kcdg) with the ONE fixed wire contract every knockout-correction write
// shares -- HTTP 409 {"error":"downstream_knockout_played","matchId",
// "blockingMatchId","blockingMatches","displaced","qualifierChange","message"}
// -- and reports whether it answered, so the caller's switch can fall through
// to its own remaining arms exactly like the other respondIf* helpers in this
// file.
//
// Correcting a completed bracket match (via /score, /override-winner,
// /decision, or /quick-score) can change a winner already propagated into a
// downstream match that has since recorded its own result; the engine
// refuses by default and the operator retries with forceDownstreamReopen
// once they've confirmed the override. Correcting a POOL match in a mixed
// competition answers the same way when it moves who holds a qualifying
// place and the old qualifier has already fought a knockout match:
// qualifierChange then names each place that moves (from, to), so the
// operator is told who moves, not only which match reopens; it is an empty
// list for a knockout correction (see ForceOptions.Force on the
// matching request field of whichever endpoint they're using). Before this
// existed, only /score and /override-winner had this mapping hand-copied
// into their own error switches (identically, since both need the exact
// same four fields); /decision and /quick-score fell through to a generic
// 500, which the SPA's offline write queue retries forever (mp-q8c6
// poisoned-queue pattern) for a write that can never win.
func respondIfDownstreamKnockoutPlayed(c *gin.Context, err error) bool {
	var downstreamPlayedErr *engine.DownstreamKnockoutPlayedError
	if !errors.As(err, &downstreamPlayedErr) {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{
		"error":           "downstream_knockout_played",
		"matchId":         downstreamPlayedErr.MatchID,
		"blockingMatchId": downstreamPlayedErr.BlockingMatchID,
		// Every blocked match, so the dialog can name what it will clear. One
		// entry except for a semifinal, which feeds the final AND the bronze
		// match; blockingMatchId stays as the first for older clients.
		// Each blocked match with the number the operator knows it by; the id
		// rides along for addressing, never for display.
		"blockingMatches": blockedMatchesPayload(downstreamPlayedErr.Blocking),
		"displaced":       downstreamPlayedErr.Displaced,
		"qualifierChange": qualifierChangePayload(downstreamPlayedErr.QualifierChange),
		"message":         downstreamPlayedErr.Error(),
	})
	return true
}

// qualifierChangePayload renders a refusal's qualifier changes as a list,
// never null, so a client reads one shape whichever kind of correction it
// made.
func qualifierChangePayload(changes []engine.QualifierChange) []engine.QualifierChange {
	if changes == nil {
		return []engine.QualifierChange{}
	}
	return changes
}

// respondIfDownstreamKnockoutRunning answers engine.DownstreamKnockoutRunningError
// with HTTP 409 {"error":"downstream_knockout_running","matchId",
// "runningMatches","message"} and reports whether it answered. A pool
// correction in a mixed competition that would move a qualifier out of a
// knockout match somebody is fighting right now is refused outright: unlike
// downstream_knockout_played it is NOT confirmable (forceDownstreamReopen does
// not get past it), because reopening a match mid-bout would wipe what is
// being scored at the shiaijo. message is the operator's copy ("Match 9 (Quarterfinals) is
// being fought now. Finish it or send it back to the queue, then save
// again."). A 409, never a 5xx, so the offline write queue drops a replay
// that meets it instead of retrying it forever (mp-q8c6).
func respondIfDownstreamKnockoutRunning(c *gin.Context, err error) bool {
	var runningErr *engine.DownstreamKnockoutRunningError
	if !errors.As(err, &runningErr) {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{
		"error":          "downstream_knockout_running",
		"matchId":        runningErr.MatchID,
		"runningMatches": blockedMatchesPayload(runningErr.Running),
		"message":        runningErr.Error(),
	})
	return true
}

// blockedMatchesPayload renders knockout matches for the wire, for every
// payload that names them to the operator (blockingMatches, runningMatches,
// reopenedMatches): id for addressing, number, and label, the words the
// operator is shown ("Match 3 (Final)", engine.MatchLabel). The client prints
// the label as it is rather than composing its own from the number, so the
// dialog and this payload's message cannot name the same match two ways. A
// number of 0 means the match never got one (a bye placeholder, or a
// pre-numbering bracket); the label then falls back to the match's name or id
// rather than "Match 0".
func blockedMatchesPayload(blocking []engine.ReopenedMatch) []map[string]any {
	out := make([]map[string]any, 0, len(blocking))
	for _, b := range blocking {
		out = append(out, map[string]any{"id": b.ID, "number": b.Number, "label": engine.MatchLabel(b)})
	}
	return out
}

// respondIfReopenDownstreamResolved maps *engine.ReopenDownstreamResolvedError
// (bc-cse) onto the same wire SHAPE as respondIfDownstreamKnockoutRunning
// (matchId, the blocked matches, and message), under its own `error` code
// since it is a different refusal with a different remedy: a downstream
// match auto-completed by a bye, not one being fought, so there is nothing
// to finish or requeue. Terminal, like the running case: never confirmable
// with forceDownstreamReopen. A 409, never a 5xx, so the offline write
// queue drops a replay that meets it instead of retrying it forever
// (mp-q8c6).
func respondIfReopenDownstreamResolved(c *gin.Context, err error) bool {
	var resolvedErr *engine.ReopenDownstreamResolvedError
	if !errors.As(err, &resolvedErr) {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{
		"error":           "downstream_knockout_resolved",
		"matchId":         resolvedErr.MatchID,
		"resolvedMatches": blockedMatchesPayload(resolvedErr.Resolved),
		"message":         resolvedErr.Error(),
	})
	return true
}

// broadcastReopenedDownstream announces the downstream matches a confirmed
// reopen or correction reopened: match_updated for each (each is a distinct
// match from the one the operator acted on, so a client watching only that
// court or match must hear its verdict was cleared), and
// competitor_status_updated for each one whose cleared verdict was a
// withdrawal, since the engine restored the competitor it barred
// (engine.ReopenedMatch.Restored). One helper for every door that can reopen
// downstream, so none of them can announce the reopen and miss the restore.
func broadcastReopenedDownstream(hub Broadcaster, compID string, reopened []engine.ReopenedMatch) {
	for _, r := range reopened {
		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": compID, "matchId": r.ID})
		if r.Restored != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{"competitionId": compID, "status": r.Restored})
		}
	}
}

// classifyRosterWriteError maps one of the participant-roster write sentinel
// errors -- returned by Store.AddParticipant, Store.SaveParticipants,
// Store.UpdateParticipant, Store.BulkCheckIn, and every other write that
// funnels through saveParticipantsNoLock -- to the HTTP status this package
// answers with for it. ok is false when err does not match any of them,
// leaving the caller free to check its own site-specific sentinels (e.g.
// state.ErrCompetitionNotInSetup) before falling back to internalError.
//
// This is the ONE place these five map to a status; every participant-write
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
	case errors.Is(err, state.ErrDuplicateTeamMember):
		return http.StatusConflict, true
	case errors.Is(err, state.ErrReservedName):
		return http.StatusBadRequest, true
	case errors.Is(err, state.ErrBlankDojo), errors.Is(err, state.ErrBlankName):
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
