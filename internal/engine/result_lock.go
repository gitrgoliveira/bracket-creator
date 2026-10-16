package engine

import (
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

// rivalDisplacesResult reports whether the merge moved the stored result out
// for a write that did not see it: an older-stamped finish or decision (a
// rival under I1, never a correction) that arrived after a newer result.
func rivalDisplacesResult(rep *state.MergeReport) bool {
	return rep != nil && rep.Baseline != nil && !rep.Baseline.Informed && !rep.Baseline.BasisSent &&
		slices.Contains(rep.DisplacedGroups(), state.GroupResult)
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
func holdAsLocked(result *state.MatchResult, code string) {
	rep := result.Merge
	if rep.HeldValues == nil {
		rep.HeldValues = map[string]json.RawMessage{}
	}
	for _, g := range rep.Applied {
		rep.Held = append(rep.Held, g)
		rep.HeldValues[g] = state.GroupValue(result, g)
	}
	rep.Applied, rep.Unchanged, rep.Displaced = nil, nil, nil
	rep.ResultChanged, rep.MatchClosed, rep.Replayed = false, false, false
	rep.Baseline, rep.LogPrefix, rep.Prior = nil, nil, nil
	rep.HoldReason, rep.ResultLocked = HoldReasonResultLocked, code
}
