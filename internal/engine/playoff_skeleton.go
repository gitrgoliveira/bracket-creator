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
