package engine

import (
	"bytes"
	"encoding/json"
	"errors"
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

// rivalDisplacesResult reports whether the merge moved the stored result out
// for a write that did not see it: an older-stamped finish or decision (a
// rival under I1, never a correction) that arrived after a newer result. The
// stored result is the whole of it (verdict, scoreline, overtime, flags, bouts),
// so ANY displaced group counts, not the verdict alone. "Did not see it" is
// judged exactly as the replay's rivalResult does: a correction is informed,
// and so is a basis that holds the line of a recorded result (lines is the
// match's score log as the write found it); a basis that holds none (an unknown
// session, a count past the log) saw nothing.
func rivalDisplacesResult(rep *state.MergeReport, lines []state.ScoreLogLine) bool {
	if rep == nil || rep.Baseline == nil || rep.Baseline.Informed || len(rep.DisplacedGroups()) == 0 {
		return false
	}
	b := rep.Baseline
	return !b.BasisSent || !BasisSawResult(lines, &b.Basis)
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
	rep.ResultChanged, rep.MatchClosed, rep.Replayed = false, false, false
	rep.Baseline, rep.LogPrefix, rep.Prior = nil, nil, nil
	rep.HoldReason, rep.ResultLocked = lockReason(code), code
}

// lockReason is the history reason of a hold: what locked the result.
func lockReason(code string) string {
	if code == state.HeldReasonDownstreamRunning {
		return HoldReasonResultLockedRunning
	}
	return HoldReasonResultLocked
}

// rivalDisplacesNow is rivalDisplacesResult for the write just landed, reading
// the score log only when the merge displaced something at all.
func (e *Engine) rivalDisplacesNow(tx state.StoreTx, compID, matchID string, result *state.MatchResult) bool {
	rep := result.Merge
	if rep == nil || rep.Baseline == nil || len(rep.DisplacedGroups()) == 0 {
		return false
	}
	return rivalDisplacesResult(rep, e.snapshotScoreLog(tx, compID, matchID, result, matchWriteForward))
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
// groups' values instead of the result itself (the engi finish's probe).
func (e *Engine) requalifyMixedPoolWriteLocked(tx state.StoreTx, compID string, comp *state.Competition, matchID string, written, prior, result, values *state.MatchResult, fo ForceOptions, rival bool) error {
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
				if sameHeldLine(p, rep, doorOf(result)) {
					return
				}
			}
		}
	}
	e.recordWriteHistory(h, compID, matchID, result)
}

// sameHeldLine reports whether a history line is the line a locked hold would
// add: the same door, stamp, reason, groups and held values.
func sameHeldLine(p state.MatchHistoryEntry, rep *state.MergeReport, door string) bool {
	if p.Door != door || p.Stamp != rep.Stamp || p.Reason != rep.HoldReason ||
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
