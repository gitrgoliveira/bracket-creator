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
//
// Re-derivation at EXPORT time (this function's only caller, EliminationDraw)
// is best-effort by contract (see EliminationDraw's doc comment: it "equals
// the persisted bracket only while [pools, poolWinners, courts] are
// unchanged since the draw"), so a larger-pools shape that buildPoolFedDraw
// cannot build (outOfScope) degrades to nil here -- "nothing to render",
// the same contract every other empty/undrawable case on this path already
// has -- rather than an error this function has no way to report. The one
// invariant that MUST hold is bc-qual LP-3a review item (b)'s: never
// silently substitute the UNIFORM builder for a failed per-pool one, which
// would render an Excel sheet that seats the wrong number of qualifiers per
// pool and drops the crossing without telling anyone. buildPoolFedDraw
// enforces that by construction: its larger-pools branch only ever calls
// BuildKnockoutDrawPerPool, never BuildKnockoutDraw as a fallback.
func poolDraw(comp *state.Competition, pools []helper.Pool, numCourts int) *helper.KnockoutDraw {
	if len(pools) == 0 {
		return nil
	}
	draw, _, _ := buildPoolFedDraw(comp, pools, numCourts)
	return draw
}

// buildPoolFedDraw is the SINGLE place a pool-fed knockout draw is built from
// a state.Competition plus its currently loaded pools, honoring
// comp.ExtraQualifiers (bc-qual LP-3c, extended to fill-bracket in LP-4).
// Both callers (poolDraw above, for export re-derivation, and
// generatePoolPreviewBracket in bracket.go, for the generate-draw /
// preview-bracket persist path) go through this so they cannot
// independently drift on which builder a given competition uses -- the same
// invariant mp-ndfu already enforces for the plain uniform builder.
//
// Standard mode ("" / state.ExtraQualifiersNone) is exactly the pre-bc-qual
// call: helper.BuildKnockoutDraw with one poolWinners for every pool. This
// branch is untouched by bc-qual (same function, same arguments), which is
// what keeps every existing uniform-mode draw byte-identical to before.
//
// state.ExtraQualifiersLargerPools instead builds a pool-index ->
// qualifier-count override map from comp.QualifiersForPool (state's single
// owner of the oversized-pool arithmetic, bc-qual LP-3b) and calls
// helper.BuildKnockoutDrawPerPool. A pool whose QualifiersForPool differs
// from the uniform poolWinners is included in the map; every other pool is
// omitted, which BuildKnockoutDrawPerPool's own perPoolWinners already
// treats identically to an explicit entry equal to the default
// (draw_perpool.go).
//
// state.ExtraQualifiersFillBracket (bc-qual LP-4) resolves the draft
// selection via fillBracketDraftIndices below and calls
// helper.BuildKnockoutDrawFillBracket.
//
// outOfScope is true exactly when a non-standard mode was requested and its
// builder returned nil for a shape it marks out of scope (draw_perpool.go's
// or fill_bracket.go's file comments). Callers MUST NOT treat that nil as
// "nothing to draw" and fall back to the uniform builder -- see poolDraw and
// generatePoolPreviewBracket's own handling of it.
//
// reason names the SPECIFIC cause in operator terms when one is available
// (bc-qual LP-4, third review): fill-bracket's own SelectFillBracketDrafts
// error already names the shortfall and its remedy ("the draw needs N
// drafted 2nd(s)... seed more pools...") rather than a generic "out of
// scope", so this is threaded up rather than discarded -- an operator
// hitting the
// data-dependent residual case (see helper.FillBracketDraftCapacity's and
// TestFillBracketFormationAndBuilderAgree's doc comments for how rare that
// now is) gets told WHY, not just that it failed. reason is empty when no
// finer-grained cause exists (larger-pools' BuildKnockoutDrawPerPool has no
// error channel of its own, only nil), in which case
// generatePoolPreviewBracket's caller falls back to its existing generic
// message.
func buildPoolFedDraw(comp *state.Competition, pools []helper.Pool, numCourts int) (draw *helper.KnockoutDraw, outOfScope bool, reason string) {
	poolWinners := comp.EffectivePoolWinners()
	switch comp.ExtraQualifiers {
	case state.ExtraQualifiersLargerPools:
		overrides := extraQualifierOverrides(comp, pools, poolWinners)
		d := helper.BuildKnockoutDrawPerPool(pools, poolWinners, overrides, numCourts)
		return d, d == nil, ""
	case state.ExtraQualifiersFillBracket:
		draftIdx, err := fillBracketDraftIndices(comp, pools, numCourts)
		if err != nil {
			return nil, true, err.Error()
		}
		d := helper.BuildKnockoutDrawFillBracket(pools, draftIdx, numCourts)
		if d == nil {
			return nil, true, "the per-court placement could not be built despite draft selection succeeding"
		}
		return d, false, ""
	default:
		return helper.BuildKnockoutDraw(pools, poolWinners, numCourts), false, ""
	}
}

// fillBracketDraftIndices resolves WHICH pools' 2nds are drafted for a
// fill-bracket competition (bc-qual LP-4, rule 2): D = NextPow2(numPools) -
// numPools drafts, from the seeded pools in seed order with oversized pools
// as fallback (WKC's own rule), via CAPACITY-AWARE selection
// (second review rework) -- helper.FillBracketDraftCapacity computes the
// per-pool home half and per-half draft capacity from the pool/draft
// counts alone (before any pool is chosen), and
// helper.SelectFillBracketDrafts skips a candidate whose destination half
// has no remaining capacity rather than taking it and stranding the build.
// state has no per-pool equivalent of QualifiersForPool for this mode --
// see that method's doc comment -- so this is computed fresh from the
// whole pool set every time, exactly as extraQualifierOverrides re-derives
// larger-pools' map every time.
//
// outOfScope reasons collapse to a single error return here (buildPoolFedDraw's
// caller only distinguishes "outOfScope" from "built"), covering both: the
// per-court target arithmetic FillBracketDraftCapacity checks is not
// achievable for this (pools, drafts, numCourts) triple (ok=false, expected
// to be effectively unreachable given a power-of-two numCourts -- see
// BuildKnockoutDrawFillBracket's doc comment), and the genuinely
// data-dependent case SelectFillBracketDrafts itself now names in its own
// error message (the "seed more pools" shortfall, threaded to the operator
// unmodified -- the engine tests grep it end to end).
func fillBracketDraftIndices(comp *state.Competition, pools []helper.Pool, numCourts int) ([]int, error) {
	drafts := helper.NextPow2(len(pools)) - len(pools)
	poolHalf, capacityByHalf, ok := helper.FillBracketDraftCapacity(pools, drafts, numCourts)
	if !ok {
		return nil, fmt.Errorf("fill-bracket: the per-court target arithmetic is not achievable for %d pools across %d shiaijo", len(pools), numCourts)
	}
	return helper.SelectFillBracketDrafts(pools, comp.PoolSize, poolHalf, capacityByHalf)
}

// extraQualifierOverrides builds the pool-index -> qualifier-count map
// helper.BuildKnockoutDrawPerPool expects, from comp.QualifiersForPool
// (state's single owner of the oversized-pool arithmetic; see its doc
// comment for the exact oversized test). A pool is included only when its
// count differs from the uniform poolWinners -- an entry that repeats the
// default would be redundant, not wrong (BuildKnockoutDrawPerPool's
// perPoolWinners falls back to the default for anything absent from the
// map), but omitting it keeps the map as small as the "overrides" name implies.
func extraQualifierOverrides(comp *state.Competition, pools []helper.Pool, poolWinners int) map[int]int {
	var overrides map[int]int
	for i, p := range pools {
		if w := comp.QualifiersForPool(p); w != poolWinners {
			if overrides == nil {
				overrides = make(map[int]int, len(pools))
			}
			overrides[i] = w
		}
	}
	return overrides
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

// CompetitionCourts is the shiaijo a competition runs on, by NAME.
//
// A competition's courts need not start at A: running one competition on A+B and
// another on C+D is how a 4-shiaijo venue is shared. Naming the bands from their
// POSITION printed "Shiaijo A"/"Shiaijo B" on sheets for courts that competition
// never touches, so the names travel into the workbook rather than a count.
//
// It loads the tournament ITSELF rather than taking one. Resolution goes through
// InheritedDrawCourts, where an empty list means "inherit the tournament's", so
// a caller that forgot the load, or handed nil, would silently get the
// positional answer back for exactly the legacy records that need the venue's.
// A tournament that will not load is not fatal to an export: the resolution then
// degrades to the competition's own list, which is what it was before.
func CompetitionCourts(store *state.Store, comp *state.Competition) []string {
	if comp == nil {
		return helper.CourtLabels(1)
	}
	var tourn *state.Tournament
	if store != nil {
		if t, err := store.LoadTournament(); err == nil {
			tourn = t
		}
	}
	return InheritedDrawCourts(comp.Courts, tourn)
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
		// poolNameFromMatchID, not a first-hyphen split: it is this package's
		// owner of that parse and it handles the -TB-/-DH- suffixes and a pool
		// name that itself contains a hyphen, where a naive cut silently folds
		// two pools into one key and reports the pair as split.
		pool, ok := poolNameFromMatchID(m.ID)
		if !ok || pool == "" {
			continue
		}
		// An unrecorded court is UNKNOWN, not agreement. Skipping those rows
		// instead let ONE bout with a court speak for a pool whose others had
		// none, filing the entire block -- header, matches, standings table and
		// the pool's roster sheet -- under a shiaijo the rest of it is not on.
		// The contract above says a pool is reported only when EVERY one of its
		// matches agrees, and this is the half that was missing; it is the same
		// rule helper.CourtPlan.PageCourt applies to a tree page, for the same
		// reason. Pool matches are created with a court (engine/pools.go), so in
		// practice this only catches legacy or hand-edited rows -- and for those
		// the answer it now gives is the drawn band, which is where they were.
		if m.Court == "" {
			split[pool] = true
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

// BronzeCourt is the shiaijo the 3rd-place bout is on, or "" when there is none.
//
// The bronze is a SIBLING of bracket.Rounds rather than a row in it, so every
// rounds-only walk misses it -- the recurring mistake this repo already guards
// against in state.findBracketMatchByID. It also carries no match number, so it
// cannot ride BracketCourtByMatchNumber and needs its court passed on its own.
func BronzeCourt(bracket *state.Bracket) string {
	if bracket == nil || bracket.ThirdPlaceMatch == nil {
		return ""
	}
	return bracket.ThirdPlaceMatch.Court
}

// LiveCourtPlan is the court plan for a competition with a stored bracket: where
// every bout is ACTUALLY being fought.
//
// It exists so no caller assembles the plan itself. ByMatch and Bronze must come
// from the SAME bracket and must always be paired -- the 3rd-place bout carries
// no match number, so it cannot ride ByMatch, and an exporter that filled the
// other three fields and forgot it would compile clean and silently print the
// bronze under whichever shiaijo came first. That bug has already been written
// twice; this is the assembly that cannot omit it.
//
// The CLI builds its own two-field plan instead: it renders a blank workbook
// with no stored bracket, so the draw's regions are the only assignment there is.
func LiveCourtPlan(draw *helper.KnockoutDraw, courts []string, bracket *state.Bracket) helper.CourtPlan {
	return helper.CourtPlan{
		Draw:    draw,
		Courts:  courts,
		ByMatch: BracketCourtByMatchNumber(bracket),
		Bronze:  BronzeCourt(bracket),
	}
}
