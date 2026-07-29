package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Playoff elimination-skeleton leaf derivation, shared by both workbook builders
// so a pure-playoffs competition (no pools, so helper.GenerateFinals returns
// nothing) still renders a bracket. The results export (internal/export) overlays
// scores onto these leaves; the blank-template export (Engine.ExportCompetitionXlsx)
// prints them empty. Both MUST derive leaves the same way or the two exports of one
// competition would disagree (mp-ndfu). This lives in engine, the layer that owns
// bracket generation, because internal/export already imports engine (the reverse
// import would be a cycle).

// isPurePlayoffs reports whether comp runs a standalone elimination bracket with
// no pool phase -- the case where helper.GenerateFinals yields nothing and the
// leaves must come from the stored bracket / participant seeding instead. Both
// the bracket-load guard (export.go) and EliminationLeaves gate on this exact
// condition, so it lives in one predicate rather than two hand-copied literals.
func isPurePlayoffs(comp *state.Competition, pools []helper.Pool) bool {
	return len(pools) == 0 && comp.Format == state.CompFormatPlayoffs
}

// EliminationLeaves returns the elimination-tree leaf order for a competition's
// knockout phase. It is the single owner of the leaf derivation so the blank-
// template export (Engine.ExportCompetitionXlsx) and the results export
// (internal/export) of one competition always render the identical bracket
// (mp-ndfu) -- the invariant that prose-synchronized copies in the two callers
// used to risk drifting.
//
// Pooled formats (Mixed), and the League case the IsPlayoffEnabled gate later
// drops, come straight from helper.GenerateFinals on the pool winners.
// EffectivePoolWinners (not the raw field) is used so an unset (<=0) PoolWinners
// still yields the 2-winner knockout the tournament actually runs (mp-0yd8). A
// pure playoffs competition has no pools, so GenerateFinals is empty; its leaves
// come from the frozen bracket's own first-round order
// (PlayoffLeavesFromBracket, which cannot desync from the stored bracket the
// score overlay fills in), falling back to participant seeding only pre-start,
// when no bracket exists yet. bracket may be nil for any non-pure-playoffs
// caller: it is consulted only on the pure-playoffs branch, where both callers
// load it.
func EliminationLeaves(store *state.Store, comp *state.Competition, pools []helper.Pool, bracket *state.Bracket) []string {
	finals := helper.GenerateFinals(pools, comp.EffectivePoolWinners())
	if len(finals) == 0 && isPurePlayoffs(comp, pools) {
		if leaves := PlayoffLeavesFromBracket(bracket); len(leaves) > 0 {
			return leaves
		}
		return PlayoffFinalsFromParticipants(store, comp)
	}
	return finals
}

// PlayoffLeavesFromBracket reconstructs the pow2 leaf ordering the engine used to
// build a pure-playoffs bracket, read straight from the frozen bracket's first
// round: each round-1 match contributes SideA then SideB, in order, with "" for a
// bye. Feeding THIS order to the export skeleton guarantees its printed
// "Round N - Match N" numbering matches the stored bracket's MatchNumber (the two
// numbering walks are equal-by-contract, assignBracketMatchNumbers vs
// helper.AssignMatchNumbers), so overlayBracketScores writes each score into the
// right block even when seeds.csv has drifted. Returns nil for a nil/empty bracket
// (e.g. a playoffs competition not yet started).
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
