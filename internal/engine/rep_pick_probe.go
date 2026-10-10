package engine

import (
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// AppliedGroups says which of the groups incoming changes the merge would APPLY
// onto stored (an echo of the stored value counts as applied, as in
// MergeReport.Applied). The judges of a score write (mobileapp: the
// representative picks, and a participant's numbered bout rows) judge a group's
// content exactly when it lands, and ask this rather than re-deriving the
// merge's rule: whether the write names the group, whether its stamp is older
// than the stored group's, whether a hold takes the whole write (a stale finish
// naming another winner is held whole, HoldReasonFinishAtomic), and what
// becomes of a pick on a row that is gone or never was (settleOrphanPicks) all
// live in mergeMatchWrite, and run here. A group that is held, displaced or
// sits on no row is kept in the match's history by the real write and is not
// the judge's to refuse.
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
// (ErrMatchSideMismatch) and lands nothing, so it applies no group. The bracket
// branch skips that fold for a match not playable yet (sidesBeforeMerge) and
// refuses the write itself; the probe folds unconditionally, which cannot
// disagree, since an empty stored side never mismatches.
func AppliedGroups(stored, incoming *state.MatchResult, comp *state.Competition, knockout bool) []string {
	if stored == nil {
		stored = &state.MatchResult{} // a match the store does not hold has no row and no stamps
	}
	prior, probe := cloneForProbe(stored), cloneForProbe(incoming)
	if reconcileSides(&probe, storedSides{A: prior.SideA, B: prior.SideB, AID: prior.SideAID, BID: prior.SideBID}) {
		return nil
	}
	return mergeMatchWrite(&prior, &probe, matchWriteForward, mergeCtx{comp: comp, knockout: knockout, nilSubsClear: !knockout}).Applied
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
