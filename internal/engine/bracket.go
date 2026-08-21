package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// winnerOfFormat / winnerOfPlaceholder are the ONE producer of the
// generation-time "not yet decided" slot value: depth is 1-based from the
// final (matching parseWinnerOf's contract, scoring.go), matchIdx is 0-based
// within that round. Before this, three independent fmt.Sprintf("Winner of
// r%d-m%d", ...) call sites (here x2, and retractPropagatedWinner in
// kachinuki.go) and one fmt.Sscanf parser (parseWinnerOf) each hard-coded the
// same literal — a wording change had to touch all four to keep
// propagateBracketWinner able to re-resolve a reopened match's downstream
// slot; missing even one would silently break re-propagation on that one
// path only (mp-gmcg review).
const winnerOfFormat = "Winner of r%d-m%d"

func winnerOfPlaceholder(depth, matchIdx int) string {
	return fmt.Sprintf(winnerOfFormat, depth, matchIdx)
}

// generatePlayoffs builds and saves an elimination bracket for a standalone
// (direct-elimination) playoffs competition. StandardSeeding → CreateBalancedTree
// → TreeToLeafArray mirrors the Excel create-playoffs path exactly (mp-5ng7);
// the unbalanced tree's structural byes are embedded as "" slots in the pow2
// array. (A mixed competition's pool-fed knockout is NOT built here, it is the
// preview bracket from generatePoolPreviewBracket, filled in by
// ResolveQualifiedPools as each pool finishes.)
func (e *Engine) generatePlayoffs(comp *state.Competition, players []domain.Player, seeds []domain.SeedAssignment) error {
	// helper.Player is a type alias for domain.Player (NFR-007); the
	// Excel-coupled helpers accept domain values directly.
	if len(seeds) > 0 {
		if err := helper.ApplySeeds(players, seeds); err != nil {
			return fmt.Errorf("applying seeds: %w", err)
		}
	}

	if comp.NumberPrefix != "" {
		helper.AssignPlayerNumbers(players, comp.NumberPrefix, 1)
	}

	seededPlayers := helper.StandardSeeding(players)
	names := make([]string, len(seededPlayers))
	for i, p := range seededPlayers {
		names[i] = p.Name
	}
	tree := helper.CreateBalancedTree(names)

	// R2-R7 leave a playoffs bracket alone, but its matches still need courts,
	// so the seeded tree is cut into one region per shiaijo exactly as the Excel
	// pagination cuts it (helper.NewPlayoffDraw).
	draw := helper.NewPlayoffDraw(tree, len(comp.Courts))
	bracket, err := e.buildBracketFromDraw(comp, draw)
	if err != nil {
		return err
	}

	return e.store.SaveBracket(comp.ID, bracket)
}

// bracketMatchLeafSlot is the LEFTMOST first-round slot that match (roundIdx,
// matchIdx) is rooted above, in a pow2-padded bracket.Rounds: match (r, m)
// covers leaves [m*2^(r+1), (m+1)*2^(r+1)).
//
// Two things derive from it and they MUST agree: a match's court (the region
// owning that leaf) and the tie-break inside a DisplayRound when numbering
// matches. They are the same quantity, so they are one function -- numbering a
// bout by one rule and placing it by another is precisely how the printed
// sheet's "Match 12" and the app's "Match 12" became different bouts.
func bracketMatchLeafSlot(roundIdx, matchIdx int) int {
	return matchIdx * (1 << (roundIdx + 1))
}

// generatePoolPreviewBracket builds the in-place knockout bracket for a mixed
// (Pools + Knockout) competition at draw time. Its leaves start as pool-origin
// placeholders ("Pool A-1st", "Pool B-2nd", …) produced by the court-first draw
// (helper.BuildKnockoutDraw), the same labels the Excel Tree sheet uses, and the bracket is
// scheduled here so knockout matches have court/time slots from the start. As
// each pool finishes, ResolveQualifiedPools replaces that pool's placeholders
// with the real finishers IN PLACE (no separate playoffs competition, no manual
// start step); a knockout match becomes scoreable once both its sides resolve.
// The Preview flag is set here and cleared by ResolveQualifiedPools on the first
// seeding; scoring playability is per-match (bracketMatchPlayable), not gated on
// this flag.
//
// No-ops (returns nil without writing bracket.json) when there are no pools
// (nothing to seed a tree from) or when the draw comes back empty.
// PoolWinners <= 0 is coerced to 2 (matching the same default in
// ResolveQualifiedPools) rather than treated as "skip", a mixed source with the
// field unset still has a knockout to preview, and matching the resolver default
// ensures the preview shape equals the live knockout bracket.
//
// bc-qual LP-3c: this is the "generate-draw" boundary (runDrawPipeline ->
// generatePools -> generatePoolPreviewBracket) that actually PERSISTS
// bracket.json, so it is where an out-of-scope larger-pools shape must
// become a clean, operator-facing error rather than silently writing no
// bracket at all (a mixed competition would otherwise reach CompStatusPools
// with pools.csv on disk and no knockout to score into). Draw building goes
// through buildPoolFedDraw (playoff_skeleton.go), shared with the export
// path's poolDraw, so both agree on which builder a given competition uses.
func (e *Engine) generatePoolPreviewBracket(comp *state.Competition) error {
	pools, err := e.store.LoadPools(comp.ID)
	if err != nil {
		return fmt.Errorf("loading pools for preview bracket: %w", err)
	}
	if len(pools) == 0 {
		return nil
	}

	// Mirror the Excel create-pools path exactly: the SAME court-first draw
	// builds both, so the preview bracket has the same topology, the same
	// region-to-shiaijo mapping and the same byes as the printed Excel bracket
	// (mp-5ng7). Flattening to a pow2 leaf array is TreeToLeafArray's job, done
	// inside buildBracketFromDraw, and re-pads the regions' structural byes as
	// "" slots.
	draw, outOfScope, reason := buildPoolFedDraw(comp, pools, len(comp.Courts))
	if outOfScope {
		// bc-qual LP-3a review item (b), extended to fill-bracket in LP-4:
		// the mode's builder (BuildKnockoutDrawPerPool or
		// BuildKnockoutDrawFillBracket) returned nil / an error for a shape
		// its file comment marks out of scope (e.g. a court count with no
		// same-half neighbour to cross an oversized pool's extra qualifier
		// to, or drafts that do not split evenly across opposite halves).
		// NEVER fall back to the uniform builder here -- that would
		// silently seat the wrong number of qualifiers per pool and drop
		// the crossing the operator asked for. Surface it as a clean,
		// actionable *ValidationError instead -- naming the SPECIFIC cause
		// (third review) when buildPoolFedDraw supplied one, rather than
		// the generic "outside what extraQualifiers currently supports"
		// for every shape alike.
		if reason != "" {
			return validationErrorf("competition %s: the %s qualifier draw could not be built for %d pools across %d court(s): %s", comp.ID, comp.ExtraQualifiers, len(pools), len(comp.Courts), reason)
		}
		return validationErrorf("competition %s: the %s qualifier draw could not be built for %d pools across %d court(s); this shape is outside what extraQualifiers %q currently supports, adjust courts/pools or switch extraQualifiers back to standard", comp.ID, comp.ExtraQualifiers, len(pools), len(comp.Courts), comp.ExtraQualifiers)
	}
	if draw == nil {
		return nil
	}

	bracket, err := e.buildBracketFromDraw(comp, draw)
	if err != nil {
		return err
	}
	bracket.Preview = true

	return e.store.SaveBracket(comp.ID, bracket)
}

// buildBracketFromDraw builds a balanced single-elimination bracket from a
// built draw. Labels may be resolved player names (live playoffs) or
// pool-origin placeholders (preview bracket); the tree shape, court
// assignment, bye resolution and scheduling are identical either way. The
// caller persists the result (and sets Preview when appropriate).
//
// It takes the DRAW rather than a leaf array plus a span slice because the two
// are one object's two faces and are only correct together: the leaves are
// helper.TreeToLeafArray(draw.Root) (which mirrors the Excel bracket topology)
// and the spans are draw.RegionSpans(). Passing them separately admitted
// leaves from one tree with spans from another, which nothing detects -- the
// bracket would carry correct sides and wrong courts, and helper.CourtForLeafSlot
// never errors because it always returns a real court.
//
// The spans are what make each match's COURT exact: a region is a contiguous,
// aligned span of the pow2 leaf array (R3), so a match's court is the region
// containing its first-round slot. The court used to be derived by dividing the
// round-1 slot count by the court count, which is only right when every court
// holds the same number of pools, and which silently clamped every overflow
// slot onto the last court. A draw with no regions falls back to court 0.
func (e *Engine) buildBracketFromDraw(comp *state.Competition, draw *helper.KnockoutDraw) (*state.Bracket, error) {
	if draw == nil || draw.Root == nil {
		return nil, fmt.Errorf("buildBracketFromDraw: no draw to build from")
	}
	// SlotArray, not TreeToLeafArray: the pow2 bracket must carry the draw's
	// SLOT geometry so its round indices equal the printed Excel columns. The
	// flat array tail-pads a vacancy block's bye pair into adjacency
	// ([H10,C6,"",""] for the true [H10,"",C6,""]), which would fight a bout
	// in round 1 that the sheet prints in round 2. Region widths are identical
	// under both readings, so the spans and every court derivation are
	// unaffected.
	leaves := helper.SlotArray(draw.Root)
	regionSpans := draw.RegionSpans()

	// NextPow2 ensures we have a balanced tree with enough slots
	pow2 := helper.NextPow2(len(leaves))
	leafValues := make([]string, pow2)
	for i := range pow2 {
		if i < len(leaves) {
			leafValues[i] = leaves[i]
		} else {
			leafValues[i] = "" // Bye
		}
	}

	tree := helper.CreateBalancedTree(leafValues)
	maxDepth := helper.CalculateDepth(tree)

	var rounds [][]state.BracketMatch
	// Round 1 is the first level of matches (just above leaves)
	// Depth starts at 1 (root). Leaves are at maxDepth.
	// We want rounds from maxDepth-1 down to 1.
	for d := maxDepth - 1; d >= 1; d-- {
		rIdx := (maxDepth - 1) - d // 0 = first round, increases toward final
		nodes := helper.TraverseRounds(tree, 1, d)
		var roundMatches []state.BracketMatch
		for i, n := range nodes {
			sideA := ""
			if n.Left != nil {
				if n.Left.LeafNode {
					sideA = n.Left.LeafVal
				} else {
					// Placeholder for winner of previous round match
					sideA = winnerOfPlaceholder(d+1, i*2)
				}
			}
			sideB := ""
			if n.Right != nil {
				if n.Right.LeafNode {
					sideB = n.Right.LeafVal
				} else {
					sideB = winnerOfPlaceholder(d+1, i*2+1)
				}
			}

			// If both sides are empty (byes), we might still want to show the match
			// but marked as completed/skipped.

			// Derive the court from the leaf slots this match covers. A match
			// inside one region takes that region's court; the half-finals and
			// the final take the centre-most court they span, which is where a
			// hall runs its closing bouts. helper.CourtForSpan owns both rules
			// and the Excel side asks it the same question (NodeCourts), so the
			// operator's screen and the printed handout cannot disagree.
			courtIdx := helper.CourtForSpan(regionSpans, bracketMatchLeafSlot(rIdx, i), 1<<(rIdx+1))
			court := ""
			if len(comp.Courts) > 0 {
				if courtIdx >= len(comp.Courts) {
					courtIdx = len(comp.Courts) - 1
				}
				court = comp.Courts[courtIdx]
			}

			match := state.BracketMatch{
				ID:     fmt.Sprintf("m-r%d-%d", maxDepth-d, i),
				SideA:  sideA,
				SideB:  sideB,
				Status: state.MatchStatusScheduled,
				Court:  court,
				// ScheduledAt is populated below by
				// assignBracketMatchSlots, uniform start times
				// were retired in T150.
			}

			// Auto-resolve byes
			if sideA == "" && sideB != "" {
				match.Winner = sideB
				match.Status = state.MatchStatusCompleted
			} else if sideA != "" && sideB == "" {
				match.Winner = sideA
				match.Status = state.MatchStatusCompleted
			} else if sideA == "" && sideB == "" {
				match.Status = state.MatchStatusCompleted
			}

			roundMatches = append(roundMatches, match)
		}
		rounds = append(rounds, roundMatches)
	}

	bracket := &state.Bracket{
		Rounds: rounds,
	}

	// Post-process: Propagate auto-resolved winners across all rounds
	for rIdx := 0; rIdx < len(bracket.Rounds)-1; rIdx++ {
		for mIdx := 0; mIdx < len(bracket.Rounds[rIdx]); mIdx++ {
			m := &bracket.Rounds[rIdx][mIdx]
			if m.Status == state.MatchStatusCompleted {
				e.propagateBracketWinner(bracket, rIdx, mIdx)
			}
		}
	}

	// Latent byes: when TreeToLeafArray clusters structural byes
	// (e.g. 5 players → ["A","B","","","C","","D","E"]), a "" vs ""
	// dead match propagates "" into a round where the other feeder is
	// a real "Winner of…" placeholder. That match will auto-resolve at
	// runtime but at generation time it looks Scheduled. Mark it
	// Completed so the real-match count stays N-1.
	for rIdx := range bracket.Rounds {
		for mIdx := range bracket.Rounds[rIdx] {
			m := &bracket.Rounds[rIdx][mIdx]
			if m.Status != state.MatchStatusScheduled {
				continue
			}
			aEmpty := m.SideA == ""
			bEmpty := m.SideB == ""
			aPlaceholder := strings.HasPrefix(m.SideA, "Winner of")
			bPlaceholder := strings.HasPrefix(m.SideB, "Winner of")
			if (aEmpty && bPlaceholder) || (bEmpty && aPlaceholder) {
				m.Status = state.MatchStatusCompleted
			}
		}
	}

	// Per-court slot assignment (T150) + ceremony-block skipping
	// (T151). See pools.go for the same wiring; tournament load
	// failures abort the start so the operator notices the missing
	// schedule data rather than silently shipping a uniform-start
	// bracket.
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}
	assignBracketMatchSlots(bracket.Rounds, comp, tournament)

	// Display metadata (mp-7f2w): label each match with its effective round and
	// real feeders so the viewer renders the same effective-round columns as the
	// Excel Tree sheet (structural byes skip a column). Computed once here, while
	// the "Winner of rX-mY" placeholders are still intact, it must NOT be
	// recomputed after results resolve those placeholders into player names.
	computeBracketDisplayMetadata(bracket)
	applySlotDisplayRounds(bracket, draw)

	// Assign sequential match numbers matching the Excel Tree sheet (AC8).
	// Must run AFTER computeBracketDisplayMetadata sets Hidden so the skipping
	// logic is identical to helper.AssignMatchNumbers (nil-node skip in Excel
	// = Hidden or both-sides-empty in the web bracket).
	assignBracketMatchNumbers(bracket)

	// Bronze (3rd-place) playoff: naginata only, and only when a real semifinal
	// round exists (len(Rounds) >= 2; a 2-player bracket has a single round and
	// no semifinal, so no bronze). Modelled as a sibling field rather than a row
	// in Rounds to preserve the power-of-two advancement geometry (see
	// state.Bracket.ThirdPlaceMatch). Sides start empty and are filled from the
	// two semifinal losers by propagateBracketWinner. DisplayRound -1 is a
	// sentinel telling renderers to label this "3rd Place".
	if helper.NeedsBronzeBlock(comp.Naginata, len(bracket.Rounds)) {
		// Default the bronze to the FINAL's court: the final and the 3rd-place
		// playoff are conventionally run on the same shiaijo, so the bronze
		// shows up in that court's queue out of the box. The final is the sole
		// match in the last round. Operators can still reassign it via
		// UpdateMatchCourt like any other bracket match.
		finalCourt := ""
		if last := bracket.Rounds[len(bracket.Rounds)-1]; len(last) > 0 {
			finalCourt = last[0].Court
		}
		bracket.ThirdPlaceMatch = &state.BracketMatch{
			ID:           "m-bronze",
			Status:       state.MatchStatusScheduled,
			DisplayRound: -1,
			Court:        bronzeDefaultCourt(finalCourt, comp.Courts),
		}
		// Give the bronze a real time slot just before the final on its court:
		// assignBracketMatchSlots above only walked Rounds, so the bronze would
		// otherwise stay blank and sort AFTER the final everywhere. Must run
		// after the bronze's court is set above.
		scheduleBronze(bracket, comp, tournament)
	}

	// Freeze the draw-time slot labels (bc-draw). Runs LAST, after byes have
	// resolved, winners have propagated and the bronze exists, so what is
	// recorded is exactly the sides this draw produced.
	recordDrawPlaceholders(bracket, leaves)

	return bracket, nil
}

// recordDrawPlaceholders copies each match's draw-time SideA/SideB/Winner into
// its PlaceholderA/B/Winner fields, so ResolveQualifiedPools can later tell which
// pool finisher owns a slot WITHOUT recomputing the draw (see the field comments
// on state.BracketMatch, and legacy_template_v1.go for why that recompute was a
// live-event hazard).
//
// Only for a pool-fed knockout: a standalone playoffs bracket's leaves are real
// competitors, nothing ever "resolves" into it, and recording player names under
// a field called "placeholder" would both mislead and double the size of every
// bracket.json for no reader. Sniffing the leaves rather than taking a flag keeps
// the single-signature builder its three call sites already share; a leaf array
// either came from the pool-fed draw or it did not.
//
// It is NOT a general "original sides" snapshot: it is written once at draw and
// never updated, which is precisely what makes it a stable resolution key.
func recordDrawPlaceholders(bracket *state.Bracket, leaves []string) {
	if bracket == nil || !leavesCarryPoolPlaceholders(leaves) {
		return
	}
	stamp := func(m *state.BracketMatch) {
		m.PlaceholderA = m.SideA
		m.PlaceholderB = m.SideB
		m.PlaceholderWinner = m.Winner
	}
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			stamp(&bracket.Rounds[ri][mi])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		// Empty in every current draw (the bronze is fed by semifinal losers
		// long after this runs); stamped anyway so the bronze can never become
		// the one match whose placeholder silently went missing.
		stamp(bracket.ThirdPlaceMatch)
	}
}

// leavesCarryPoolPlaceholders reports whether a leaf array is a pool-fed draw,
// i.e. holds at least one "Pool X-Nth" finalist placeholder. Byes ("") and
// resolved player names are not placeholders.
func leavesCarryPoolPlaceholders(leaves []string) bool {
	for _, l := range leaves {
		if helper.IsPoolFinalistPlaceholder(l) {
			return true
		}
	}
	return false
}

// bronzeDefaultCourt chooses the 3rd-place (bronze) match's default court. The
// bronze conventionally shares the FINAL's court, so that wins when set. If the
// final's court is unset (a court-assignment gap) but the competition has
// courts, fall back to the first court so the bronze still lands on a shiaijo
// queue and doesn't render an empty "Shiaijo" label. A genuinely court-less
// competition keeps "" — consistent with every other match; don't invent a
// court. Operators can reassign via UpdateMatchCourt.
func bronzeDefaultCourt(finalCourt string, courts []string) string {
	if finalCourt != "" {
		return finalCourt
	}
	if len(courts) > 0 {
		return courts[0]
	}
	return ""
}

// assignBracketMatchNumbers sets MatchNumber on every real (non-Hidden,
// non-empty) bracket match. This is the web API's numbering implementation; the
// Excel renderer has a SEPARATE one, helper.AssignMatchNumbers, which operates on
// []*Node instead of *state.Bracket. The two are NOT a literally-shared function
// (the types differ), they are kept equal-by-contract so the on-screen "Match N"
// always equals the printed Excel "Match N".
//
// Ordering, CRITICAL for byes: the Excel sheet numbers via eliminationMatchRounds,
// which groups matches by DEPTH-FROM-ROOT (the unbalanced tree's deepest matches
// come first), NOT by raw bracket.Rounds index. With a non-power-of-two roster the
// pow2-padded bracket.Rounds order diverges from that depth grouping, so numbering
// in raw Rounds order drifts (e.g. 5 entrants: the lone deep first-round bout must
// be Match 1, not the shallow slot-0 bout). DisplayRound already encodes the Excel
// depth grouping (verified by TestBracketDisplayMetadata_MatchesExcelRounds), so we
// number by descending DisplayRound (deepest/earliest round first).
//
// The tie-break inside a DisplayRound is the match's LEFTMOST FIRST-ROUND SLOT,
// bracketMatchLeafSlot — the same function buildBracketFromDraw uses to find a
// match's court. It has to be, because Excel's TraverseRounds walks each depth level
// LEFT TO RIGHT across the whole tree, and one effective round can draw its
// matches from several pow2 rounds at once: a shallow region's first bout and a
// deep region's second bout share a DisplayRound while sitting in bracket.Rounds
// 0 and 1. Tie-breaking on the within-round position alone (the old rule) then
// interleaves them by an index that means different things in the two rounds, and
// the printed "Match 12" and the app's "Match 12" become different bouts. Measured
// on a pool-fed draw of 8 pools x 3 qualifiers: the sheet numbered Pool G-3rd v
// Pool H-3rd 12 while the app numbered the E-1st/A-2nd v F-1st/B-2nd bout 12
// (bc-draw Phase 5). The leaf slot orders them the way the sheet prints them,
// because a node's slot range is contiguous and left-to-right IS increasing slot.
//
// Skip rule (matches the Excel nil-node skip): Hidden (structural-bye) matches and
// both-sides-empty dead matches are excluded and do not consume a number.
//
// The printed Excel sheet is authoritative. The contract is enforced by
// TestMatchNumberingParity_ExcelVsWeb (match_numbering_parity_test.go) for
// playoffs brackets and by TestExcelWorkbookMatchesEngineBracket_Mixed
// (excel_draw_parity_test.go) for pool-fed ones, the latter by reading the numbers
// back out of a rendered workbook. If they ever diverge, fix THIS path to match
// the Excel one — and the JS buildDisplayModel matchNumById ordering with it
// (web-mobile/js/bracket.jsx), which is the third implementation of this walk.
//
// Must run AFTER computeBracketDisplayMetadata, which sets Hidden / DisplayRound.
func assignBracketMatchNumbers(b *state.Bracket) {
	type ref struct {
		m *state.BracketMatch
		// leafSlot is the first-round slot this match's subtree starts at.
		leafSlot int
	}
	var real []ref
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if m.Hidden {
				continue
			}
			if m.SideA == "" && m.SideB == "" {
				continue
			}
			real = append(real, ref{m: m, leafSlot: bracketMatchLeafSlot(ri, mi)})
		}
	}
	// Descending DisplayRound (deepest/earliest round first), then left to right
	// across the whole tree, mirrors the Excel eliminationMatchRounds walk.
	sort.SliceStable(real, func(i, j int) bool {
		if real[i].m.DisplayRound != real[j].m.DisplayRound {
			return real[i].m.DisplayRound > real[j].m.DisplayRound
		}
		return real[i].leafSlot < real[j].leafSlot
	})
	for i, r := range real {
		r.m.MatchNumber = i + 1
	}
}

// computeBracketDisplayMetadata fills DisplayRound / Hidden / Feeders on every
// match so the viewer can render effective-round columns identical to the Excel
// Tree sheet (matches grouped by depth-from-root; structural byes skip a column
// rather than appearing as empty cards). It is purely additive, the positional
// ID + "Winner of rX-mY" resolution scheme used by scoring/scheduling/the pool
// resolver is untouched.
//
// A match is REAL iff both sides are non-empty (a structural bye always leaves
// one side ""). Phantom matches (empty-vs-empty dead matches and one-sided latent
// byes) are marked Hidden. For each real match, Feeders holds the IDs of the two
// REAL feeder matches whose winners meet here ([A, B] order); a side fed by a
// seeded entrant / pool placeholder / bye carries "" (no connector). DisplayRound
// counts from the final (1 = Final), assigned by walking the real feeder graph
// outward from the lone real match in the last round.
//
// Must run after bye winners have been auto-resolved and propagated (so resolved
// names already sit in their feeder slots), i.e. at the end of bracket build.
func computeBracketDisplayMetadata(bracket *state.Bracket) {
	rounds := bracket.Rounds
	numRounds := len(rounds)
	if numRounds == 0 {
		return
	}

	at := func(r, m int) *state.BracketMatch {
		if r < 0 || r >= numRounds || m < 0 || m >= len(rounds[r]) {
			return nil
		}
		return &rounds[r][m]
	}
	isReal := func(m *state.BracketMatch) bool {
		return m != nil && m.SideA != "" && m.SideB != ""
	}

	// realFeederID follows a "Winner of rX-mY" side through any phantom (bye)
	// matches to the underlying REAL feeder match's ID, or "" when the side is a
	// seeded entrant / resolved name / dead end (no connector line).
	var realFeederID func(side string) string
	realFeederID = func(side string) string {
		if !strings.HasPrefix(side, "Winner of") {
			return "" // resolved name, pool placeholder, or empty → no feeder
		}
		r, m := parseWinnerOf(side, numRounds)
		f := at(r, m)
		if f == nil {
			return ""
		}
		if isReal(f) {
			return f.ID
		}
		// Phantom: descend through whichever side carries a competitor.
		if f.SideA != "" {
			return realFeederID(f.SideA)
		}
		if f.SideB != "" {
			return realFeederID(f.SideB)
		}
		return "" // dead match (both empty)
	}

	byID := make(map[string]*state.BracketMatch)
	for r := range rounds {
		for i := range rounds[r] {
			mm := &rounds[r][i]
			byID[mm.ID] = mm
			if isReal(mm) {
				mm.Hidden = false
				mm.DisplayRound = 0 // assigned by the walk below
				mm.Feeders = []string{realFeederID(mm.SideA), realFeederID(mm.SideB)}
			} else {
				mm.Hidden = true
				mm.DisplayRound = 0
				mm.Feeders = nil
			}
		}
	}

	// DisplayRound's provisional value is the match's POW2 round counted from
	// the final (1 = Final); applySlotDisplayRounds then overrides every bout
	// the draw tree knows with the round the risen-aware Excel walk fights it
	// in, which is the printed column. The pow2 row alone is wrong for an
	// assembly-level late bout that the pow2 padding parks in round-1
	// adjacency, and the old feeder-graph walk was wrong the other way for a
	// phantom-risen pair the sheets fight in round 1 (34th EKC Junior Male
	// F2) -- the bracket graph cannot tell those two shapes apart, so neither
	// recomputation can be the source. The draw tree can (Node.risen), and
	// its walk is what the workbook prints.
	if !isReal(at(numRounds-1, 0)) {
		return // degenerate bracket (e.g. < 2 competitors)
	}
	for r := range rounds {
		for i := range rounds[r] {
			if mm := &rounds[r][i]; !mm.Hidden {
				mm.DisplayRound = numRounds - r
			}
		}
	}
}

// applySlotDisplayRounds stamps each real bracket match with the round the
// draw's risen-aware walk (helper.SlotRoundMatches) fights it in. A bout is
// located by its first-round window: entrant width 2^(r+1) starting at slot
// offset i*2^(r+1) identifies pow2 match (r, i) exactly, because the bracket
// was built from helper.SlotArray of the same tree.
func applySlotDisplayRounds(bracket *state.Bracket, draw *helper.KnockoutDraw) {
	if bracket == nil || draw == nil || draw.Root == nil {
		return
	}
	numRounds := len(bracket.Rounds)
	for _, sm := range helper.SlotRoundMatches(draw.Root) {
		w := sm.EntrantWidth
		r := -1
		for ww := w; ww > 1; ww >>= 1 {
			r++
		}
		if r < 0 || r >= numRounds {
			continue
		}
		i := sm.Offset / w
		if i < 0 || i >= len(bracket.Rounds[r]) {
			continue
		}
		if mm := &bracket.Rounds[r][i]; !mm.Hidden {
			mm.DisplayRound = numRounds - sm.Round
		}
	}
}
