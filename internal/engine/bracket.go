package engine

import (
	"fmt"
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

// generateKnockout builds and saves an elimination bracket for a standalone
// (direct-elimination) knockout competition. StandardSeeding → CreateBalancedTree
// → TreeToLeafArray mirrors the Excel create-knockout path exactly (mp-5ng7);
// the unbalanced tree's structural byes are embedded as "" slots in the pow2
// array. (A mixed competition's pool-fed knockout is NOT built here, it is the
// preview bracket from generatePoolPreviewBracket, filled in by
// ResolveQualifiedPools as each pool finishes.)
func (e *Engine) generateKnockout(comp *state.Competition, players []domain.Player, seeds []domain.SeedAssignment) error {
	// helper.Player is a type alias for domain.Player (NFR-007); the
	// Excel-coupled helpers accept domain values directly.
	if len(seeds) > 0 {
		if err := helper.ApplySeeds(players, seeds); err != nil {
			return fmt.Errorf("applying seeds: %w", err)
		}
	}

	// No AssignPlayerNumbers call here (bc-pnum review, F11): traced end to
	// end and confirmed dead. Only p.Name is read below to build the tree's
	// leaves; buildBracketFromDraw works entirely off those leaf STRINGS
	// (helper.CreateBalancedTree(names), SlotArray, ...), never off the
	// players slice or its Number field, and the bracket it saves stores
	// competitor NAMES on each side (state.BracketMatch.SideA/SideB), not a
	// Number. Because players is a slice parameter, mutating players[i].Number
	// here would also alias back into the caller's own slice (runDrawPipeline
	// in competition.go), but that caller never reads it again either -- the
	// generation-relevant re-validation after this call checks comp fields
	// only. A knockout-only competitor's Number is composed at READ time by
	// engine.NumberKnockoutParticipants (bc-pnum ruling 2), from the
	// DrawOrder stamped below, so nothing here needs to write Number.
	seededPlayers := helper.StandardSeeding(players)
	names := make([]string, len(seededPlayers))
	drawOrder := make([]string, len(seededPlayers))
	for i, p := range seededPlayers {
		names[i] = p.Name
		drawOrder[i] = p.ID
	}
	tree := helper.CreateBalancedTree(names)

	// R2-R7 leave a knockout bracket alone, but its matches still need courts,
	// so the seeded tree is cut into one region per shiaijo exactly as the Excel
	// pagination cuts it (helper.NewKnockoutDraw).
	draw := helper.NewKnockoutDraw(tree, len(comp.Courts))
	// drawOrder is StandardSeeding's own placement, participant ids in
	// bracket-position order top to bottom (bc-pnum ruling 2): "a number
	// belongs to a position in the draw". Passed straight into
	// buildBracketFromDraw, which stamps it onto bracket.DrawOrder AND uses
	// it to stamp round-0 SideAID/SideBID (bc-brid) before its own
	// bye-propagation pass runs, so a walkover's WinnerID cascades too. This
	// is the ONE place that produces it -- a mixed (Pools + Knockout)
	// bracket never carries it, its competitors are numbered pool by pool
	// instead, and generatePoolPreviewBracket passes nil for exactly that
	// reason.
	bracket, err := e.buildBracketFromDraw(comp, draw, drawOrder)
	if err != nil {
		return err
	}

	return e.store.SaveBracket(comp.ID, bracket)
}

// generatePoolPreviewBracket builds the in-place knockout bracket for a mixed
// (Pools + Knockout) competition at draw time. Its leaves start as pool-origin
// placeholders ("Pool A-1st", "Pool B-2nd", …) produced by the court-first draw
// (helper.BuildKnockoutDraw), the same labels the Excel Tree sheet uses, and the bracket is
// scheduled here so knockout matches have court/time slots from the start. As
// each pool finishes, ResolveQualifiedPools replaces that pool's placeholders
// with the real finishers IN PLACE (no separate knockout competition, no manual
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
// through buildPoolFedDraw (knockout_skeleton.go), shared with the export
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

	// nil drawOrder: a pool-fed bracket's leaves are pool-origin placeholders,
	// never resolved competitors at draw time, so there is nothing to stamp
	// (see buildBracketFromDraw's own doc comment). ResolveQualifiedPools
	// stamps ids as each placeholder resolves to a real pool finisher.
	bracket, err := e.buildBracketFromDraw(comp, draw, nil)
	if err != nil {
		return err
	}
	bracket.Preview = true

	return e.store.SaveBracket(comp.ID, bracket)
}

// buildBracketFromDraw builds a balanced single-elimination bracket from a
// built draw. Labels may be resolved player names (live knockout) or
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
//
// drawOrder is the participant-id twin of the leaves this draw was built
// from, in the SAME order (bc-brid): generateKnockout passes StandardSeeding's
// own id ordering so round-0 SideAID/SideBID (and a walkover's WinnerID) can
// be stamped from it (see state.Bracket.StampRoundZeroSideIDsFromDrawOrder);
// generatePoolPreviewBracket passes nil, since a pool-fed bracket's leaves are
// unresolved pool placeholders, not competitors, at draw time.
func (e *Engine) buildBracketFromDraw(comp *state.Competition, draw *helper.KnockoutDraw, drawOrder []string) (*state.Bracket, error) {
	if draw == nil || draw.Root == nil {
		return nil, fmt.Errorf("buildBracketFromDraw: no draw to build from")
	}
	// SlotArray, not TreeToLeafArray: the pow2 bracket must hold every bout at
	// the slots the draw tree gives it, or applySlotDisplayRounds cannot find
	// the bout to stamp its printed round. The flat array tail-pads a vacancy
	// block's bye pair into adjacency ([H10,C6,"",""] for the true
	// [H10,"",C6,""]), which would fight a bout in round 1 that the sheet
	// prints in round 2. Region widths are identical under both readings, so
	// the spans and every court derivation are unaffected.
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
			courtIdx := helper.CourtForSpan(regionSpans, state.BracketMatchLeafSlot(rIdx, i), 1<<(rIdx+1))
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
		Rounds:    rounds,
		DrawOrder: drawOrder,
	}
	// Stamp round-0 SideAID/SideBID (and a resolved bye's WinnerID) from
	// DrawOrder BEFORE the propagation pass below runs, so a walkover's
	// winner id cascades into later rounds along with its name (bc-brid).
	// No-op when drawOrder is nil (the pool-fed preview bracket).
	bracket.StampRoundZeroSideIDsFromDrawOrder()

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
	// (The load-time restamp, state.Bracket.RestampRoundsFromFeeders, does not
	// break this rule: it reads only the Feeders stamped here, never the sides.)
	computeBracketDisplayMetadata(bracket)
	applySlotDisplayRounds(bracket, draw)

	// Assign sequential match numbers matching the Excel Tree sheet (AC8).
	// Must run AFTER computeBracketDisplayMetadata sets Hidden so the skipping
	// logic is identical to helper.AssignMatchNumbers (nil-node skip in Excel
	// = Hidden or both-sides-empty in the web bracket). The rule lives on
	// state.Bracket so the load-time restamp (RestampRoundsFromFeeders) numbers
	// a stored bracket with this same body.
	bracket.NumberMatches()

	// Bronze (3rd-place) knockout: only when this competition's format
	// requires a single 3rd place (comp.RequiresSingleThirdPlace, the
	// generalised per-format rule -- see its and EffectiveTwoThirdPlaces's
	// doc comments, bc-3rdp), and only when a real semifinal round exists
	// (len(Rounds) >= 2; a 2-player bracket has a single round and no
	// semifinal, so no bronze). Modelled
	// as a sibling field rather than a row in Rounds to preserve the
	// power-of-two advancement geometry (see state.Bracket.ThirdPlaceMatch).
	// Sides start empty and are filled from the two semifinal losers by
	// propagateBracketWinner. DisplayRound -1 is a sentinel telling
	// renderers to label this "3rd Place".
	if helper.NeedsBronzeBlock(comp.RequiresSingleThirdPlace(), len(bracket.Rounds)) {
		// Default the bronze to the FINAL's court: the final and the 3rd-place
		// knockout are conventionally run on the same shiaijo, so the bronze
		// shows up in that court's queue out of the box. The final is the sole
		// match in the last round. Operators can still reassign it via
		// UpdateMatchCourt like any other bracket match.
		finalCourt := ""
		if last := bracket.Rounds[len(bracket.Rounds)-1]; len(last) > 0 {
			finalCourt = last[0].Court
		}
		bracket.ThirdPlaceMatch = &state.BracketMatch{
			ID:           state.BronzeMatchID,
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
// Only for a pool-fed knockout: a standalone knockout bracket's leaves are real
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
	// the draw tree knows with its distance from the final in that tree, the
	// column the workbook prints it in. The two differ wherever an empty half
	// was collapsed on a bout's path to the final: a pair beside a phantom
	// pair sits in the first pow2 row but prints a column later (34th EKC
	// Junior Individual Male, P4 v P5), and the pow2 padding parks an
	// assembly-level late bout in round-1 adjacency. The real feeder graph
	// gives the same rounds (TestBracketDisplayMetadata_Feeders pins one
	// round per feeder step); the tree walk stays the source because it is
	// what the workbook prints. The load-time restamp of an older bracket
	// (state.Bracket.RestampRoundsFromFeeders) walks that feeder graph, so the
	// two routes must keep agreeing:
	// TestRestampRoundsFromFeeders_LeavesFreshBracketsUnchanged pins it.
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
// draw tree fights it in, its distance from the final (helper.SlotRoundMatches).
// A bout is located by its first-round window: entrant width 2^(r+1) starting
// at slot offset i*2^(r+1) identifies pow2 match (r, i) exactly, because the
// bracket was built from helper.SlotArray of the same tree.
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
