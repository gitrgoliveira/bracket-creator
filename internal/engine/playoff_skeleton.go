package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Playoff elimination-skeleton derivation, shared by both workbook builders so a
// pure-playoffs competition (no pools, so the pool-fed draw returns nothing)
// still renders a bracket. The results export (internal/export) overlays scores
// onto it; the blank-template export (Engine.ExportCompetitionXlsx) prints it
// empty. Both MUST derive it the same way or the two exports of one competition
// would disagree (mp-ndfu). This lives in engine, the layer that owns bracket
// generation, because internal/export already imports engine (the reverse import
// would be a cycle).
//
// EliminationDraw is the entry point both builders call. EliminationLeaves was
// that entry point until the court regions were needed as well as the leaf
// order; it is now a step inside it.

// isPurePlayoffs reports whether comp runs a standalone elimination bracket with
// no pool phase -- the case where the pool-fed draw yields nothing and the
// leaves must come from the stored bracket / participant seeding instead. Both
// the bracket-load guard (export.go) and EliminationLeaves gate on this exact
// condition, so it lives in one predicate rather than two hand-copied literals.
func isPurePlayoffs(comp *state.Competition, pools []helper.Pool) bool {
	return len(pools) == 0 && comp.Format == state.CompFormatPlayoffs
}

// EliminationLeaves returns the elimination-tree leaf order for a competition's
// knockout phase.
//
// Its only caller today is EliminationDraw, which owns the "both exports render
// the identical bracket" invariant (mp-ndfu) that this function used to own. It
// stays exported and keeps its own pool-fed branch rather than being folded in,
// because that branch is what makes the function total for any caller: reached
// through EliminationDraw the branch cannot fire, since EliminationDraw only
// calls this after its own poolDraw returned nil and poolDraw's result does not
// depend on the court count either call passes. Called directly it still answers
// correctly for a pooled competition. Do not "simplify" it away on the strength
// of the current single call site.
//
// Pooled formats (Mixed), and the League case the IsPlayoffEnabled gate later
// drops, come straight from the court-first draw over the pool winners.
// EffectivePoolWinners (not the raw field) is used so an unset (<=0) PoolWinners
// still yields the 2-winner knockout the tournament actually runs (mp-0yd8). A
// pure playoffs competition has no pools, so the draw is empty; its leaves
// come from the frozen bracket's own first-round order
// (PlayoffLeavesFromBracket, which cannot desync from the stored bracket the
// score overlay fills in), falling back to participant seeding only pre-start,
// when no bracket exists yet. bracket may be nil for any non-pure-playoffs
// caller: it is consulted only on the pure-playoffs branch, where both callers
// load it.
//
// Prefer EliminationDraw where the TREE is needed: the leaf list alone cannot
// reproduce the court regions, and a rebuild has to go through
// helper.BuildSlotTree to collapse the array's bye slots (see EliminationDraw).
// CreateBalancedTree over this list would print a different bracket from the one
// the engine persisted.
func EliminationLeaves(store *state.Store, comp *state.Competition, pools []helper.Pool, bracket *state.Bracket) []string {
	if draw := poolDraw(comp, pools, len(comp.Courts)); draw != nil {
		return helper.TreeLeafLabels(draw.Root)
	}
	if isPurePlayoffs(comp, pools) {
		if leaves := PlayoffLeavesFromBracket(bracket); len(leaves) > 0 {
			return leaves
		}
		return PlayoffFinalsFromParticipants(store, comp)
	}
	return nil
}

// poolDraw builds the pool-fed court-first draw, or nil when the competition
// has no pool phase to draw from.
func poolDraw(comp *state.Competition, pools []helper.Pool, numCourts int) *helper.KnockoutDraw {
	if len(pools) == 0 {
		return nil
	}
	return helper.BuildKnockoutDraw(pools, comp.EffectivePoolWinners(), numCourts)
}

// EliminationDraw returns the knockout tree AND its per-shiaijo regions for a
// competition's workbook export. It is the single owner of that derivation so
// the blank-template export (Engine.ExportCompetitionXlsx) and the results
// export (internal/export) of one competition always render the identical
// bracket (mp-ndfu), and so both render the SAME bracket the engine persisted
// in bracket.json rather than a re-derivation of it.
//
// The playoffs rebuild goes through helper.BuildSlotTree, NOT CreateBalancedTree.
// PlayoffLeavesFromBracket hands back the frozen bracket's pow2 first round, so
// a ragged roster's leaf array carries "" bye slots, and only BuildSlotTree
// collapses an all-empty half instead of giving it a node. CreateBalancedTree
// gave every "" a leaf, so the sheet drew and numbered a junction for each
// phantom pair: at 5 entrants, 7 printed junctions for a 4-bout bracket, with
// Match 2 between two empty slots and every number after it off the bracket's
// own (bc-cse). That is the same collapse the pool-fed draw applies in
// buildRegion, so both formats now rebuild a leaf array the one way, and the
// tree this yields is the tree generatePlayoffs itself cut into regions
// (CreateBalancedTree over the unpadded entrant list) -- BuildSlotTree is
// TreeToLeafArray's inverse -- so the printed pages, the printed numbers and the
// stored bracket all describe one draw. It is also what cmd/create-playoffs
// prints, since it builds from the entrant list and never pads.
//
// Returns nil when there is nothing to render.
func EliminationDraw(store *state.Store, comp *state.Competition, pools []helper.Pool, bracket *state.Bracket, numCourts int) *helper.KnockoutDraw {
	if draw := poolDraw(comp, pools, numCourts); draw != nil {
		return draw
	}
	leaves := EliminationLeaves(store, comp, pools, bracket)
	if len(leaves) == 0 {
		return nil
	}
	// nil (every slot a bye) falls through NewPlayoffDraw as a nil draw, which
	// the callers already treat as "nothing to render".
	return helper.NewPlayoffDraw(helper.BuildSlotTree(leaves), numCourts)
}

// PlayoffLeavesFromBracket reconstructs the pow2 leaf ordering the engine used to
// build a pure-playoffs bracket, read straight from the frozen bracket's first
// round: each round-1 match contributes SideA then SideB, in order, with "" for a
// bye. Feeding THIS order to the export skeleton is what keeps the printed
// "Round N - Match N" numbering equal to the stored bracket's MatchNumber even
// when seeds.csv has drifted, so overlayBracketScores writes each score into the
// right block. The two numbering walks are equal-by-contract
// (assignBracketMatchNumbers vs helper.AssignMatchNumbers), but only over the
// same SHAPE: the leaf order alone is not enough, the rebuild must also collapse
// the "" slots below, or the numbering walks over a tree with extra nodes in it
// (see EliminationDraw). Returns nil for a nil/empty bracket (e.g. a playoffs
// competition not yet started).
func PlayoffLeavesFromBracket(bracket *state.Bracket) []string {
	if bracket == nil || len(bracket.Rounds) == 0 {
		return nil
	}
	first := bracket.Rounds[0]
	leaves := make([]string, 0, len(first)*2)
	for _, m := range first {
		leaves = append(leaves, m.SideA, m.SideB)
	}
	return leaves
}

// PlayoffFinalsFromParticipants seeds the competition's participants exactly as
// generatePlayoffs does (ApplySeeds → optional numbering → StandardSeeding),
// returning the seeded names to feed the elimination-tree skeleton. This is the
// PRE-START fallback only: once a bracket exists, PlayoffLeavesFromBracket is used
// instead because it cannot desync from the frozen bracket. Since there is no
// bracket to overlay when this runs, a best-effort (possibly unseeded) order is
// acceptable. Returns nil when participants can't be loaded, in which case no
// elimination sheet is rendered.
func PlayoffFinalsFromParticipants(store *state.Store, comp *state.Competition) []string {
	players, err := store.LoadParticipants(comp.ID, comp.EffectiveWithZekkenName())
	if err != nil || len(players) == 0 {
		return nil
	}
	if seeds, serr := store.LoadSeeds(comp.ID); serr == nil && len(seeds) > 0 {
		if aerr := helper.ApplySeeds(players, seeds); aerr != nil {
			// An unmatched seed name is non-fatal for a read-only export; the
			// bracket still renders, just unseeded. Mirror the file's warn pattern.
			fmt.Printf("export: warning: apply seeds for playoffs skeleton: %v\n", aerr)
		}
	}
	if comp.NumberPrefix != "" {
		helper.AssignPlayerNumbers(players, comp.NumberPrefix, 1)
	}
	seeded := helper.StandardSeeding(players)
	names := make([]string, len(seeded))
	for i, p := range seeded {
		names[i] = p.Name
	}
	return names
}
