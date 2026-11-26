package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// THE RESULT LOCK (bc-evts stage 2b, operator decision K1, 2026-10-16). Under
// I1 the first recorded result (by stamp) stands, so a finish stamped earlier
// than the stored result displaces it when it arrives late. Once the NEXT
// knockout match has been played or started, though, the stored result is
// LOCKED: such a write, which did not see the stored result, is HELD. It is
// kept in the match's history with its values and answered 200 superseded with
// the downstream reason (never the 409 an explicit correction gets, which
// would lose the write), and the stored result stands. An explicit correction
// (a reason, clearWithdrawal, a basis) keeps today's downstream guard and its
// force option. Before the next match is played or started I1 applies as it
// always did. The pool requalification guard is locked the same way once a
// seated knockout match was played or started.

// HoldReasonResultLocked is the history reason of a finish or decision held
// because the next knockout match had already been played or started.
const HoldReasonResultLocked = "the next round was already fought"

// HoldReasonResultLockedRunning is the same hold when the next round is only
// under way.
const HoldReasonResultLockedRunning = "the next round has already started"

// displacesUnseenResult reports whether the merge moved a finished stored result
// out for a write that did not see it: an older-stamped finish or decision (a
// rival under I1, never a correction) that arrived after a newer result, or a
// court result (bc-sseq FR8: the court's standing, ScoreBaseline.Court) over an
// ordinary result it never saw, whichever was stamped first. Either is held by
// the lock rather than refused, and never reopens a played knockout match. The
// stored result is the whole of it (verdict, scoreline, overtime, flags, bouts),
// so ANY displaced group counts, not the verdict alone; but a result must BE in
// force to be locked: a match reopened or sent back to the queue holds none, and
// a finish displacing only the scoring stamped after the reopen locks nothing.
// "Did not see it" and "in force" are the replay's own reading of the log as the
// write found it (ResultInForceAt, set on the report by the merge): a correction
// is informed, and so is a SENT basis that holds the line of the result in force.
func displacesUnseenResult(rep *state.MergeReport) bool {
	if rep == nil || rep.Baseline == nil || rep.Baseline.Informed || len(rep.DisplacedGroups()) == 0 {
		return false
	}
	return rep.StoredResultInForce && !rep.StoredResultSeen
}

// downstreamLockCode names the lock a played or started knockout match one hop
// down puts on the result of match (rIdx, mIdx): the held-reason code, or ""
// when the next match is untouched.
func downstreamLockCode(bracket *state.Bracket, rIdx, mIdx int) string {
	d := propagatedDownstreamOf(bracket, rIdx, mIdx)
	if len(d.running()) > 0 {
		return state.HeldReasonDownstreamRunning
	}
	if len(d.played()) > 0 {
		return state.HeldReasonDownstreamPlayed
	}
	return ""
}

// lockedByRequalification names the lock a pool write's requalification
// refusal stands for: the knockout match it would change was played or is
// being fought. "" for any other error.
func lockedByRequalification(err error) string {
	var played *DownstreamKnockoutPlayedError
	var running *DownstreamKnockoutRunningError
	switch {
	case errors.As(err, &played):
		return state.HeldReasonDownstreamPlayed
	case errors.As(err, &running):
		return state.HeldReasonDownstreamRunning
	}
	return ""
}

// holdAsLocked turns the merge report of a write that was about to displace the
// stored result into the report of one held whole: every group it applied is
// held with the value it carried (the history keeps it), nothing is displaced
// or logged as a score baseline (a logged older result would replay over the
// stored one), and the report names the lock. The caller answers
// ErrMatchSuperseded.
func holdAsLocked(result, from *state.MatchResult, code string) {
	if from == nil {
		from = result
	}
	rep := result.Merge
	if rep.HeldValues == nil {
		rep.HeldValues = map[string]json.RawMessage{}
	}
	for _, g := range rep.Applied {
		rep.Held = append(rep.Held, g)
		rep.HeldValues[g] = state.GroupValue(from, g)
	}
	rep.Applied, rep.Unchanged, rep.Displaced = nil, nil, nil
	rep.ResultChanged, rep.MatchClosed, rep.CourtResult, rep.Replayed = false, false, false, false
	rep.Baseline, rep.LogPrefix, rep.Prior, rep.Replay = nil, nil, nil, nil
	rep.HoldReason, rep.ResultLocked = lockReason(code), code
}

// lockReason is the history reason of a hold: what locked the result.
func lockReason(code string) string {
	switch code {
	case state.HeldReasonDownstreamRunning:
		return HoldReasonResultLockedRunning
	case state.HeldReasonBuiltOn:
		return HoldReasonResultBuiltOn
	}
	return HoldReasonResultLocked
}

// knockoutLockCode is downstreamLockCode for the knockout round match matchID
// of a stored bracket: "" for a pool, the bronze match or an unknown id.
func (e *Engine) knockoutLockCode(tx state.StoreTx, compID, matchID string) string {
	bracket, err := tx.LoadBracket(compID)
	if err != nil || bracket == nil {
		return ""
	}
	for rIdx := range bracket.Rounds {
		for mIdx := range bracket.Rounds[rIdx] {
			if bracket.Rounds[rIdx][mIdx].ID == matchID {
				return downstreamLockCode(bracket, rIdx, mIdx)
			}
		}
	}
	return ""
}

// requalifyMixedPoolWriteLocked runs a pool write's requalification under the
// result lock. A write that is a rival (displaced the stored result without
// having seen it) is judged WITHOUT force, whatever the caller sent, and when
// that refuses because a seated knockout match was played or started, the
// write is held (the refusal has rolled the match back), never forced through:
// force belongs to an explicit correction. values, when set, supplies the held
// groups' values instead of the result itself (the engi finish's probe). Every
// write's door passes through it (a non-mixed competition requalifies
// nothing), so the built-on hold (holdBuiltOnResult) is asked here first.
func (e *Engine) requalifyMixedPoolWriteLocked(tx state.StoreTx, compID string, comp *state.Competition, matchID string, written, prior, result, values *state.MatchResult, fo ForceOptions, rival bool) error {
	// The built-on hold first (a no-op for every write but a court result that
	// replaces one it never saw): a requalification that had run and repainted
	// the knockout could not be taken back by the row-only rollback.
	if err := e.holdBuiltOnResult(tx, compID, comp, matchID, prior, result, values); err != nil {
		return err
	}
	if rival {
		fo.Force = false
	}
	err := e.requalifyMixedPoolWrite(tx, compID, comp, matchID, written, prior, fo)
	if err != nil && rival {
		if code := lockedByRequalification(err); code != "" {
			holdAsLocked(result, values, code)
			e.recordSupersededWriteHistory(tx, compID, matchID, result)
			return supersededBy(result)
		}
	}
	return err
}

// recordLockedHoldHistory keeps a write the result lock held in the match's
// history, once: a resend of it (a queued write whose answer was lost, sent
// again under the same stamp) is held again and answered the same, but the
// history already has that line, so it adds none. The held write is no line of
// the score log (rep.Baseline is nil), so the log cannot say it was seen.
func (e *Engine) recordLockedHoldHistory(h state.StoreTx, compID, matchID string, result *state.MatchResult) {
	if rep := result.Merge; rep != nil {
		if past, err := h.LoadMatchHistory(compID, matchID); err == nil {
			for _, p := range past {
				if sameHeldLine(p, rep, doorOf(result), result.RevSession) {
					return
				}
			}
		}
	}
	e.recordWriteHistory(h, compID, matchID, result)
}

// sameHeldLine reports whether a history line is the line a locked hold would
// add: the same door, session, stamp, reason, groups and held values (two
// devices sending the same value are two writes; a device's resend is one).
// An anonymous self-run caller has no session (its RevSession is blanked
// before the write, enforceSelfRunPolicy), so two anonymous devices sending
// the same values under the same stamp read as one resend and keep one line:
// accepted, as the line kept says everything the second would.
func sameHeldLine(p state.MatchHistoryEntry, rep *state.MergeReport, door, session string) bool {
	if p.Door != door || p.Session != session || p.Stamp != rep.Stamp || p.Reason != rep.HoldReason ||
		!slices.Equal(p.Changed, rep.Changed) || len(p.Held) != len(rep.HeldValues) {
		return false
	}
	for g, v := range rep.HeldValues {
		if p.Outcomes[g] != state.HistoryOutcomeHeld || !bytes.Equal(p.Held[g], v) {
			return false
		}
	}
	return true
}

// THE BUILT-ON HOLD (bc-sseq FR8, operator answer of round bc-sseq-plan-2 and
// round bc-sseq-plan-3). A result with the court's standing (a finish or
// decision from the court console of the tablet that runs the shiaijo,
// ScoreBaseline.Court) replaces an ordinary result it never saw. When other
// matches were already built on that result, the court's result WAITS instead,
// exactly as any late result waits once the next knockout match was played
// (the knockout lock above): it is held whole, kept in the match's history once
// however often it is resent, logged as no score-log line, and answered 200
// superseded with heldReason downstream_built_on and ResultBuiltOnMessage, so
// nothing built on the stored result changes silently and the operator corrects
// from the note. Built on means one of:
//
//   - a Swiss match of round r, and the competition holds a match of a later
//     round: that round was paired from it;
//   - a regular pool or league match, and its pool holds a tie-break or a
//     representative row: it was added from the standings the result fed;
//   - the competition has finished.
//
// The wait is the COURT's alone: an ordinary device's late result (the Scores
// tab and page, Pools, Bracket, the public page) keeps today's rules exactly,
// the first recorded result standing and only the knockout lock holding it. An
// explicit correction (a reason, clearWithdrawal, a basis that saw the result)
// is informed and keeps today's rules too. It judges only the write that
// arrives: a result displaced before any of these existed is left as it is.

// BuiltOnKind names what was built on the result a court write would replace.
type BuiltOnKind string

// The cases of the built-on hold.
const (
	BuiltOnSwissRound BuiltOnKind = "swiss_round"
	BuiltOnStandings  BuiltOnKind = "standings"
	BuiltOnFinished   BuiltOnKind = "finished"
)

// BuiltOn is what the built-on hold found: Kind "" when nothing was built on
// the result. Round is the later Swiss round already paired (BuiltOnSwissRound);
// Reopens says a correction reopens the finished competition (a league,
// reopenLeagueTx), for BuiltOnFinished.
type BuiltOn struct {
	Kind    BuiltOnKind
	Round   int
	Reopens bool
}

// courtDisplacesUnseen reports whether a write is the one the built-on hold
// asks about: it has the court's standing (judged by the door on arrival, its
// baseline's Court) and displaces a finished result in force it never saw
// (displacesUnseenResult) that an ORDINARY device recorded
// (replacesOrdinaryResult). An ordinary write is never asked, and neither is a
// court result over another court result, which keeps I1 as two ordinary
// results do.
func courtDisplacesUnseen(rep *state.MergeReport) bool {
	return rep != nil && rep.Baseline != nil && rep.Baseline.Court && displacesUnseenResult(rep) && replacesOrdinaryResult(rep)
}

// replacesOrdinaryResult reports whether the replay that decided the write
// (MergeReport.Replay) took a result line without the court's standing out of
// force: a line of the log as the merge read it, or a migration or drift line
// the merge added before the write's own (which no court wrote), that the write
// left held with its result group, as replaced by the court's result (a court
// line stamped after it) or as a rival of the write (a court line stamped
// before it, I1). A held result line the log already held before this write
// counts too: the hold then errs toward waiting, which the operator undoes with
// Correct.
func replacesOrdinaryResult(rep *state.MergeReport) bool {
	mr, _ := rep.Replay.(*mergeReplay)
	if mr == nil {
		return false
	}
	for i, h := range mr.after.Held {
		if i >= mr.lines || (!h.MatchClosed && !h.CourtResult) || !slices.Contains(h.Groups, state.GroupResult) {
			continue
		}
		if i >= len(mr.found) || mr.found[i].Baseline == nil || !mr.found[i].Baseline.Court {
			return true
		}
	}
	return false
}

// builtOnOf judges what was built on the stored result of matchID, read in the
// write's transaction. comp is the competition the write loaded (read here
// when nil). The cases are asked in the order of the most concrete first: a
// paired Swiss round, a finished competition, a row added from the standings.
func builtOnOf(tx state.StoreTx, compID string, comp *state.Competition, matchID string) (BuiltOn, error) {
	if comp == nil {
		c, err := tx.LoadCompetition(compID)
		if err != nil {
			return BuiltOn{}, err
		}
		comp = c
	}
	swissRound, isSwiss := parseSwissMatchRound(matchID)
	pool, inPool := "", false
	if IsPoolMatchID(matchID) && !state.IsPoolSupplementaryMatchID(matchID) {
		pool, inPool = poolNameFromMatchID(matchID)
	}
	var matches []state.MatchResult
	if isSwiss || inPool {
		ms, err := tx.LoadPoolMatches(compID)
		if err != nil {
			return BuiltOn{}, err
		}
		matches = ms
	}
	if isSwiss {
		later := 0
		for _, m := range matches {
			if r, ok := parseSwissMatchRound(m.ID); ok && r > swissRound && (later == 0 || r < later) {
				later = r
			}
		}
		if later > 0 {
			return BuiltOn{Kind: BuiltOnSwissRound, Round: later}, nil
		}
	}
	if comp.Status == state.CompStatusComplete {
		return BuiltOn{Kind: BuiltOnFinished, Reopens: comp.Format == state.CompFormatLeague}, nil
	}
	if inPool {
		for _, m := range matches {
			if !state.IsPoolSupplementaryMatchID(m.ID) {
				continue
			}
			if p, ok := poolNameFromMatchID(m.ID); ok && p == pool {
				return BuiltOn{Kind: BuiltOnStandings}, nil
			}
		}
	}
	return BuiltOn{}, nil
}

// holdBuiltOnResult is the built-on hold, run once the write has landed in the
// transaction (the requalification's place, so it serves the score, decision
// and engi doors of a pool, league, Swiss and knockout match alike, after the
// knockout arms' own lock). A court write that replaces a result it never saw,
// on which something was built, is rolled back to prior (the match as stored
// before it; the merge's own copy when the door kept none, as the engi seam
// outside a mixed pool does) and held whole, like holdAsLocked's knockout lock:
// the answer is ErrMatchSuperseded with heldReason downstream_built_on. Every
// other write returns nil and goes on.
func (e *Engine) holdBuiltOnResult(tx state.StoreTx, compID string, comp *state.Competition, matchID string, prior, result, values *state.MatchResult) error {
	if !courtDisplacesUnseen(result.Merge) {
		return nil
	}
	b, err := builtOnOf(tx, compID, comp, matchID)
	if err != nil {
		return err
	}
	if b.Kind == "" {
		return nil
	}
	undo := prior
	if undo == nil && result.Merge.Prior != nil {
		p := *result.Merge.Prior
		undo = &p
	}
	if undo == nil {
		// A result in force was displaced, so the log held its line and the
		// merge kept its copy of the stored match (MergeReport.Prior).
		log.Printf("engine: BUG: %s/%s: the court's result would replace a result %s was built on, with no stored copy to roll back to; it is applied", compID, matchID, b.Kind)
		return nil
	}
	e.rollbackMatchResultTx(tx, compID, matchID, undo)
	holdAsLocked(result, values, state.HeldReasonBuiltOn)
	e.recordSupersededWriteHistory(tx, compID, matchID, result)
	return supersededBy(result)
}

// ResultBuiltOnMessageTx is the sentence a court write held by the built-on
// hold is answered with (ResultBuiltOnMessage), judged in the answering
// transaction by the same owner the hold asked (builtOnOf), so the two never
// disagree. "" when nothing is built on matchID's result (or it cannot be read).
func ResultBuiltOnMessageTx(tx state.StoreTx, compID, matchID string) string {
	b, err := builtOnOf(tx, compID, nil, matchID)
	if err != nil {
		log.Printf("engine: %s/%s: read what was built on the result for the answer: %v", compID, matchID, err)
		return ""
	}
	return ResultBuiltOnMessage(b)
}
