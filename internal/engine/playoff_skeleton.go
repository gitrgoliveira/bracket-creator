package engine

import (
	"fmt"
	"strings"

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
// EliminationDraw is the single entry point both builders call.

// isPurePlayoffs reports whether comp runs a standalone elimination bracket with
// no pool phase -- the case where the pool-fed draw yields nothing and the
// leaves must come from the stored bracket / participant seeding instead. Both
// the bracket-load guard (export.go) and playoffLeaves gate on this exact
// condition, so it lives in one predicate rather than two hand-copied literals.
func isPurePlayoffs(comp *state.Competition, pools []helper.Pool) bool {
	return len(pools) == 0 && comp.Format == state.CompFormatPlayoffs
}

// playoffLeaves returns the first-round leaf order for a competition with NO
// pool phase to draw from. Its only caller is EliminationDraw, and only after
// that function's own poolDraw returned nil.
//
// A pure playoffs competition has no pools, so its leaves come from the frozen
// bracket's own first-round order (PlayoffLeavesFromBracket, which cannot
// desync from the stored bracket the score overlay fills in), falling back to
// participant seeding only pre-start, when no bracket exists yet. bracket may
// be nil for a non-pure-playoffs caller: it is consulted only on the
// pure-playoffs branch, where both callers load it.
//
// It used to be exported, and to re-run poolDraw itself so that it stayed
// "total for any caller". There were never any such callers, and the branch was
// unreachable through the one that exists, so what it actually bought was a
// second exported entry into this derivation -- the thing EliminationDraw is
// the single owner of (mp-ndfu) -- plus a redundant draw build.
func playoffLeaves(store *state.Store, comp *state.Competition, pools []helper.Pool, bracket *state.Bracket) []string {
	if !isPurePlayoffs(comp, pools) {
		return nil
	}
	if leaves := PlayoffLeavesFromBracket(bracket); len(leaves) > 0 {
		return leaves
	}
	return PlayoffFinalsFromParticipants(store, comp)
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
// competition's workbook export. It is the single owner of that derivation, so
// the blank-template export (Engine.ExportCompetitionXlsx) and the results
// export (internal/export) of one competition always render the identical
// bracket (mp-ndfu).
//
// The two paths are NOT the same in where that bracket comes from, and the
// difference matters:
//
//   - PURE PLAYOFFS reads the frozen bracket (PlayoffLeavesFromBracket), so the
//     printed sheet is the persisted bracket even if seeds.csv has since drifted.
//   - POOL-FED (mixed) RE-DERIVES it, from the CURRENT pools, pool-winner count
//     and comp.Courts. It equals the persisted bracket only while those three
//     inputs are unchanged since the draw.
//
// So the re-derived draw is the shape of the knockout, and it is the right
// source for the CLI, which has no stored bracket and no named shiaijo.
//
// It is NOT the authority on which shiaijo a bout runs on. A match's court is
// DATA, not geometry: the operator reassigns matches between the tournament's
// courts at will (Engine.UpdateMatchCourt), and comp.Courts itself is editable
// while the competition runs. Anything that derives a court from this draw is
// therefore stale the moment the operator moves a bout, and is wrong from the
// start for a competition whose shiaijo are not the first N (a competition
// assigned C and D is the recommended way to share a 4-shiaijo venue).
//
// Callers rendering a LIVE competition must read the court off the stored match
// and only fall back to the draw's regions when there is no stored bracket.
// This predates the court-first draw: the pre-Phase-4 export derived courts from
// comp.Courts the same way.
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
	leaves := playoffLeaves(store, comp, pools, bracket)
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

// ExportCourts is the shiaijo a competition's workbook is laid out for, by NAME.
//
// A competition's courts need not start at A: running one competition on A+B
// and another on C+D is how a 4-shiaijo venue is shared, and it is the split the
// app's own shiaijo hint recommends. Naming the second one's bands from their
// POSITION would print "Shiaijo A" and "Shiaijo B" on sheets for courts that
// competition never touches, so the names travel into the workbook rather than a
// count. The single-court fallback matches the count fallback it replaced: a
// competition saved without courts still lays out as one band.
func ExportCourts(comp *state.Competition) []string {
	if comp == nil || len(comp.Courts) == 0 {
		return helper.CourtLabels(1)
	}
	return comp.Courts
}

// BracketCourtByMatchNumber maps each numbered bout in a stored bracket to the
// shiaijo it is CURRENTLY on, for the workbook writers to band by.
//
// This is the whole reason the export cannot derive a bout's court from the
// draw: the operator reassigns matches between the tournament's courts while the
// competition runs (Engine.UpdateMatchCourt), so the draw's geometry is only the
// INITIAL answer. Keyed by match number because that is the identity the printed
// sheet and the stored bracket already share by contract (see
// PlayoffLeavesFromBracket). Unnumbered entries are byes and are never printed.
//
// Returns nil for a nil/empty bracket, which the writers read as "no live
// assignment, use the draw" -- the CLI's case, and a competition not yet drawn.
func BracketCourtByMatchNumber(bracket *state.Bracket) map[int64]string {
	if bracket == nil {
		return nil
	}
	out := make(map[int64]string)
	add := func(m *state.BracketMatch) {
		if m == nil || m.MatchNumber == 0 || m.Court == "" {
			return
		}
		out[int64(m.MatchNumber)] = m.Court
	}
	for _, round := range bracket.Rounds {
		for i := range round {
			add(&round[i])
		}
	}
	add(bracket.ThirdPlaceMatch)
	if len(out) == 0 {
		return nil
	}
	return out
}

// PoolCourtByName maps each pool to the shiaijo its matches are ACTUALLY being
// fought on, for the workbook writers to band by.
//
// Same reason as BracketCourtByMatchNumber: the operator moves matches between
// courts while the competition runs, so the drawn allocation is only the initial
// answer, and the Pool Matches sheet is what a shiaijo scores off.
//
// A pool is reported ONLY when every one of its matches agrees on a court. A
// pool split across shiaijo -- one bout moved to catch up, say -- has no single
// band it belongs in, and filing the whole block somewhere half its bouts are
// not would be worse than leaving it on the shiaijo it was drawn for. Those keep
// their drawn band, and the app's schedule stays authoritative for the
// individual bout.
//
// Returns nil when nothing is known, which the writers read as "use the draw".
func PoolCourtByName(matches []state.MatchResult) map[string]string {
	courts := make(map[string]string)
	split := make(map[string]bool)
	for _, m := range matches {
		pool, _, ok := strings.Cut(m.ID, "-")
		if !ok || pool == "" || m.Court == "" {
			continue
		}
		if seen, ok := courts[pool]; ok && seen != m.Court {
			split[pool] = true
			continue
		}
		courts[pool] = m.Court
	}
	for pool := range split {
		delete(courts, pool)
	}
	if len(courts) == 0 {
		return nil
	}
	return courts
}
