package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
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
//
// Also returned for a POOL correction in a mixed competition that moves who
// holds a qualifying place while a knockout match the old qualifier already
// fought stands on it (requalifyAfterPoolWrite): MatchID is then the pool
// match, Blocking the knockout matches fought, QualifierChange the places
// that move, and Force reopens those matches with the new qualifier seated.
// A pool-rank override (OverridePoolRank) is answered the same way, with an
// empty MatchID: it moves the pool's order without correcting any match.
type DownstreamKnockoutPlayedError struct {
	// MatchID is the id of the match being corrected; "" for a pool-rank
	// override, which corrects none.
	MatchID string
	// Label is MatchID's operator-facing label (bc-cse item 14), populated
	// PURELY at construction (MatchLabel/OperatorMatchLabel take an
	// already-loaded comp/bracket, no store I/O), never resolved inside
	// Error() itself: Error() has no store handle to resolve one with, and
	// this error is typically raised from inside a live transaction, where
	// a label helper that reads through the store would deadlock against
	// the lock the transaction already holds (non-reentrant). Empty only
	// for a pool-rank override (MatchID is also empty there; Error()
	// reports the pool by name instead). Every production constructor
	// sets it otherwise; there is no bare-MatchID fallback in Error(), so a
	// hand-built struct (a test value) must set it too.
	Label string
	// BlockingMatchID is the FIRST blocking match (bronze before the next
	// round), kept as the single-value form every existing consumer reads.
	BlockingMatchID string
	// Blocking is every match this correction is blocked on, one hop down and
	// closed with a result of its own, each with the MATCH NUMBER and knockout
	// round the operator knows it by (MatchLabel). Almost always one; a semifinal feeds both the final and the
	// bronze match, so it can be two, and both are cleared by the one
	// confirmation that names them (see newDownstreamKnockoutPlayedError for
	// why they cannot be split).
	Blocking []ReopenedMatch
	// Displaced names the competitor sitting in the slot BlockingMatchID took
	// from this match -- the one the correction would knock out of it.
	//
	// It describes THAT ONE MATCH and is only stated when there is one. The
	// two siblings a semifinal feeds hold DIFFERENT people (the final holds
	// its winner, the bronze its loser), so a single name is true of one and
	// false of the other: the dialog read "Ren Takada already played the
	// 3rd-place match and Match 3" when Ren had played only the bronze and
	// the final was the other competitor's. Consumers must keep that scope --
	// see the plural arm below and downstreamKnockoutPlayedConfirm in
	// write_result.jsx, which mirrors it.
	Displaced string
	// QualifierChange is set only when the write being corrected is a POOL
	// match in a mixed competition: the correction moves who holds one or more
	// of that pool's qualifying places, and Blocking lists the knockout matches
	// the old qualifier has already fought (planRequalification). Each entry
	// names the place and both occupants, so the operator is told who moves,
	// not just which match reopens. Empty for a knockout correction.
	QualifierChange []QualifierChange
}

// QualifierChange is one pool place a correction moves: from the competitor
// seated in the knockout for it to the one the corrected standings now put
// there. Tied means the corrected standings leave that place undecided until
// a tie-break is fought, so the slot returns to its draw label ("Pool A-1st")
// and To is empty.
type QualifierChange struct {
	Pool  string            `json:"pool"`
	Rank  int               `json:"rank"`
	Place string            `json:"place"`
	From  QualifierIdentity `json:"from"`
	To    QualifierIdentity `json:"to"`
	Tied  bool              `json:"tied,omitempty"`
}

// QualifierIdentity names a competitor on the wire the way every other
// payload does: the name to show, the participant id to address them by.
type QualifierIdentity struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

func (e *DownstreamKnockoutPlayedError) Error() string {
	// bc-cse item 14: Label is the operator-facing spelling of MatchID
	// (populated at construction; see the struct doc for why not here).
	// Every production constructor sets it, so there is no bare-id fallback
	// here; a hand-built test value must set it too.
	label := e.Label
	if len(e.QualifierChange) > 0 {
		labels := make([]string, 0, len(e.Blocking))
		for _, b := range e.Blocking {
			labels = append(labels, MatchLabel(b))
		}
		verb, them, who := "was", "it", "the competitor being replaced"
		if len(labels) > 1 {
			verb, them, who = "were", "them", "the competitors being replaced"
		}
		pool := e.QualifierChange[0].Pool
		change := fmt.Sprintf("correcting %s changes who qualified from %s", label, pool)
		if e.MatchID == "" {
			// A pool-rank override (OverridePoolRank) corrects no match.
			change = fmt.Sprintf("changing the ranking of %s changes who qualified from it", pool)
		}
		return fmt.Sprintf("%s, and %s %s already fought by %s. Retry with forceDownstreamReopen to apply the change and reopen %s to be fought again",
			change, strings.Join(labels, " and "), verb, who, them)
	}
	labels := make([]string, 0, len(e.Blocking))
	for _, b := range e.Blocking {
		labels = append(labels, MatchLabel(b))
	}
	blocked := e.BlockingMatchID
	if len(labels) > 0 {
		blocked = strings.Join(labels, " and ")
	}
	if len(e.Blocking) > 1 {
		// No Displaced clause: it names one competitor, and these matches do
		// not share one.
		return fmt.Sprintf("correcting %s would change the winner already propagated into %s, which have recorded their own results. Retry with forceDownstreamReopen to apply the correction and reopen both to be fought again",
			label, blocked)
	}
	return fmt.Sprintf("correcting %s would change the winner already propagated into %s, which has recorded its own result; this would displace %q without updating that result. Retry with forceDownstreamReopen to apply the correction and reopen %s to be fought again",
		label, blocked, e.Displaced, blocked)
}

func (e *DownstreamKnockoutPlayedError) Is(target error) bool {
	return target == ErrDownstreamKnockoutPlayed
}

// ErrDownstreamKnockoutRunning is the sentinel matched by errors.Is for
// DownstreamKnockoutRunningError. Handlers should return HTTP 409.
var ErrDownstreamKnockoutRunning = errors.New("downstream knockout match is being fought")

// DownstreamKnockoutRunningError refuses a write that would move a
// qualifier out of a knockout match somebody is fighting RIGHT NOW. Two
// producers construct it, distinguished by Reopening: a pool correction in
// a mixed competition (pool_requalify.go's requalifyAfterPoolWrite, the
// default Reopening:false), and the reopen / requeue-blocker-and-reopen
// doors (reopenBracketDownstreamCheck, kachinuki.go, Reopening:true). Unlike
// DownstreamKnockoutPlayedError it cannot be confirmed past: reopening a
// match mid-bout would wipe strikes being scored at the shiaijo, so the
// operator finishes the match or sends it back to the queue first, THEN
// retries -- which of those two doors "retries" means is Reopening's whole
// reason to exist (see Error()). Checked before the played case, so the
// operator is never asked to confirm something that would then be refused.
type DownstreamKnockoutRunningError struct {
	// MatchID is the pool match being corrected; "" for a pool-rank override
	// (OverridePoolRank), which corrects none.
	MatchID string
	// Running is every knockout match the move would reach that is being
	// fought, each with the number the operator knows it by.
	Running []ReopenedMatch
	// Reopening is true ONLY from the reopen/requeue-blocker-and-reopen
	// construction site. A reopen has no "save" step to retry -- bc-cse:
	// the shared sentence used to end "...then save again" regardless of
	// door, which is simply wrong advice on a reopen (there was never a
	// save to repeat); Reopening picks the correct remedy clause instead.
	Reopening bool
}

// Error is the operator-facing sentence (the SPA shows the same words, from
// write_result.jsx's downstreamKnockoutRunningMessage) for the SAVE-path
// wording (Reopening:false); the reopen path's own wording is Reopening's
// whole reason to exist, see the struct doc.
func (e *DownstreamKnockoutRunningError) Error() string {
	labels := make([]string, 0, len(e.Running))
	for _, r := range e.Running {
		labels = append(labels, MatchLabel(r))
	}
	subject := strings.Join(labels, " and ")
	if subject == "" {
		subject = "A knockout match"
	}
	verb := "is"
	them := "it"
	if len(labels) > 1 {
		verb, them = "are", "them"
	}
	retry := "then save again"
	if e.Reopening {
		retry = "then reopen this match again"
	}
	return fmt.Sprintf("%s %s being fought now. Finish %s or send %s back to the queue, %s.",
		SentenceCase(subject), verb, them, them, retry)
}

func (e *DownstreamKnockoutRunningError) Is(target error) bool {
	return target == ErrDownstreamKnockoutRunning
}

// ErrReopenDownstreamResolved is the sentinel matched by errors.Is for
// ReopenDownstreamResolvedError. Handlers should return HTTP 409.
//
// bc-cse.
var ErrReopenDownstreamResolved = errors.New("cannot reopen: a downstream knockout match was already resolved automatically")

// ReopenDownstreamResolvedError refuses a reopen (kachinuki.go,
// reopenBracketDownstreamCheck) because a downstream match this match feeds
// (the next round, or for a semifinal the bronze) auto-completed from a
// BYE -- it carries no result of its own (bracketMatchCarriesOwnResult is
// false: no ippons, sub-results, decision, or hansoku were ever recorded for
// it), so it is neither "someone is fighting it now"
// (DownstreamKnockoutRunningError, whose "finish it or requeue it" remedy
// does not apply here -- nobody is fighting a bye) nor "closed with a result
// of its own, which the operator may confirm past"
// (DownstreamKnockoutPlayedError). Terminal like the running case: retrying
// the SAME reopen is never the answer, since there is no bout to finish or
// court to free -- but a SAVE CORRECTION on this match is: propagateBracketWinner
// (scoring.go) reseats a bye-only downstream slot unconditionally, with no
// guard and no confirmation (guardDownstreamKnockoutCorrection's blocking
// check is firstDownstreamWithOwnResult, the SAME bracketMatchCarriesOwnResult
// predicate, so a bye-only slot never blocks it), exactly matching
// docs/user-guide/court-operators/scoring-a-match.md's documented contract
// ("A later slot that was only filled by a bye, and never fought, does not
// block a correction; it simply updates to follow the new winner"). So the
// remedy for THIS refusal is the OTHER door, not the draw/seeding.
type ReopenDownstreamResolvedError struct {
	// MatchID is the id of the match being reopened.
	MatchID string
	// Resolved is every downstream match auto-completed by a bye that this
	// reopen would strand (almost always one; a semifinal can feed both the
	// final's slot and the bronze match, so it can be two).
	Resolved []ReopenedMatch
}

// Error is the operator-facing sentence (mirrors
// DownstreamKnockoutRunningError.Error's shape and pluralisation, but names
// what actually happened -- a bye, not a fight -- and the different remedy:
// there is nothing here for the operator to finish or requeue, so the fix is
// the correction path, not this reopen; see the struct doc for why that door
// is known to work here).
func (e *ReopenDownstreamResolvedError) Error() string {
	labels := make([]string, 0, len(e.Resolved))
	for _, r := range e.Resolved {
		labels = append(labels, MatchLabel(r))
	}
	subject := strings.Join(labels, " and ")
	if subject == "" {
		subject = "A downstream match"
	}
	verb, them := "has", "it"
	if len(labels) > 1 {
		verb, them = "have", "them"
	}
	return fmt.Sprintf("%s already %s a result from a bye, not from being fought, so reopening this match cannot undo %s. Correct the result instead: Save correction on this match moves the new winner through the bye.",
		SentenceCase(subject), verb, them)
}

func (e *ReopenDownstreamResolvedError) Is(target error) bool {
	return target == ErrReopenDownstreamResolved
}

// MatchLabel names a KNOCKOUT match the way the OPERATOR sees it: its match
// number, the label on the score sheet, the bracket and the Excel tree sheet,
// qualified by its round, "Match 3 (Final)". The round is what tells it apart
// from a pool match: pool matches are numbered from 1 inside each pool, so a
// bare "Match 1" in a dialog raised while correcting "Pool A · Match 1" read as
// the match on screen. The round name is the one the bracket prints over the
// match's column (bracketRoundLabel in web-mobile/js/bracket.jsx, mirrored by
// roundLabelFromEnd below); a bracket saved before DisplayRound existed has no
// round to name, so it says "knockout Match 3" instead.
//
// The client shows this string as it is (the payloads carry it as `label`
// beside `number`), so the dialog and the server's own message say the same
// words and the format is decided here only.
//
// The 3rd-place match is the one match that has a NAME instead of a number:
// assignBracketMatchNumbers walks Bracket.Rounds, and the bronze hangs off the
// separate ThirdPlaceMatch field, so it is numbered neither here nor on the
// printed tree (helper.AssignMatchNumbers walks the same rounds). It is called
// the 3rd-place match on every surface an operator sees, so that is what this
// says; reaching the id fallback for it would show "m-bronze" to someone who
// has never seen an internal id.
//
// The id fallback remains for a match that carries no number for a reason we
// cannot name (a bye placeholder, or a bracket saved before numbering
// existed): a bare id still beats "Match 0", but it is a fallback, not a
// normal case.
// SentenceCase upper-cases s's first byte, leaving the rest untouched, and
// returns "" unchanged. Every operator match label in this codebase
// (MatchLabel above, OperatorMatchLabel, operatorMatchLabel) deliberately
// starts lowercase mid-sentence ("the 3rd-place match", "knockout Match 3",
// a bare pool name), which reads wrong the moment one is interpolated at
// the START of a sentence ("%s is not completed yet..."). Extracted from
// two hand-rolled copies (DownstreamKnockoutRunningError.Error,
// ReopenDownstreamResolvedError.Error below) so every OTHER sentence-initial
// use -- in this package and in mobileapp, which already imports engine --
// shares the one capitalisation rule instead of a third hand-rolled copy.
func SentenceCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func MatchLabel(m ReopenedMatch) string {
	if m.Number > 0 {
		if m.DisplayRound > 0 {
			return fmt.Sprintf("Match %d (%s)", m.Number, roundLabelFromEnd(m.DisplayRound-1))
		}
		return fmt.Sprintf("knockout Match %d", m.Number)
	}
	if m.ID == state.BronzeMatchID {
		return "the 3rd-place match"
	}
	return m.ID
}

// roundLabelFromEnd names a knockout round by how many rounds come after it:
// 0 = Final, 1 = Semifinals, 2 = Quarterfinals, then the bracket-size form
// "R16", "R32", .... It mirrors roundLabelFromEnd in web-mobile/js/bracket.jsx,
// which names the bracket's columns, and both are pinned by
// testdata/round_labels.json so the round a dialog names is the column the
// operator finds the match under.
func roundLabelFromEnd(fromEnd int) string {
	switch fromEnd {
	case 0:
		return "Final"
	case 1:
		return "Semifinals"
	case 2:
		return "Quarterfinals"
	}
	return fmt.Sprintf("R%d", 1<<(fromEnd+1))
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
