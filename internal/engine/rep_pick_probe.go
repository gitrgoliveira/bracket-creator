package engine

import (
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RepPicksApplied says, for each side, whether the merge would APPLY the
// representative pick (state.GroupRepPickA / state.GroupRepPickB) that incoming
// carries onto stored. The pick judges (mobileapp: a participant's score write
// and the organiser's) judge a pick for team membership exactly when it lands,
// and ask this rather than re-deriving the merge's rule: whether the write names
// the side's pick, whether its stamp is older than the stored pick's, whether a
// hold takes the whole write (a stale finish naming another winner is held
// whole, HoldReasonFinishAtomic), and what becomes of a pick on a row that is
// gone or never was (settleOrphanPicks) all live in mergeMatchWrite, and run
// here. A pick that is held, displaced or sits on no row is kept in the match's
// history by the real write and is not the judge's to refuse.
//
// It is the probe shape decisionHeldByMerge and engiFinishProbe have: the merge
// runs on COPIES of both matches (it rewrites the write it is given), so neither
// argument is changed by the call. knockout says which branch the write will
// land on (a bracket match, true, or a pool/league match, false): the two pass
// the merge different contexts (mergeCtx.knockout; mergeCtx.nilSubsClear, which
// is what an omitted bout list means on the pool branch). stored is the match
// as the store holds it (a pool row, or BracketMatchAsResult of a bracket
// match); comp is the competition (nil reads as an individual one).
//
// The write is folded onto the stored pairing first (reconcileSides, as both
// branches do before they merge), because the merge works the verdict out from
// the sides. A write that names other competitors is refused by the engine
// (ErrMatchSideMismatch) and lands nothing, so it applies no pick. The bracket
// branch skips that fold for a match not playable yet (sidesBeforeMerge) and
// refuses the write itself; the probe folds unconditionally, which cannot
// disagree, since an empty stored side never mismatches.
func RepPicksApplied(stored, incoming *state.MatchResult, comp *state.Competition, knockout bool) (sideA, sideB bool) {
	if stored == nil {
		stored = &state.MatchResult{} // a match the store does not hold has no row and no stamps
	}
	prior, probe := cloneForProbe(stored), cloneForProbe(incoming)
	if reconcileSides(&probe, storedSides{A: prior.SideA, B: prior.SideB, AID: prior.SideAID, BID: prior.SideBID}) {
		return false, false
	}
	rep := mergeMatchWrite(&prior, &probe, matchWriteForward, mergeCtx{comp: comp, knockout: knockout, nilSubsClear: !knockout})
	return slices.Contains(rep.Applied, state.GroupRepPickA), slices.Contains(rep.Applied, state.GroupRepPickB)
}

// cloneForProbe is a copy of m the merge can rewrite without reaching m: its
// bout rows, ippons, overtime, stamps and group names are cloned (nil stays
// nil, which the merge reads differently from empty), and no merge report rides
// along.
func cloneForProbe(m *state.MatchResult) state.MatchResult {
	c := *m
	c.IpponsA, c.IpponsB = cloneIppons(m.IpponsA), cloneIppons(m.IpponsB)
	c.SubResults = state.CloneSubResults(m.SubResults)
	c.GroupStamps = state.CloneGroupStamps(m.GroupStamps)
	c.Changed = slices.Clone(m.Changed)
	c.Encho = m.Encho.Clone()
	c.Merge = nil
	return c
}
