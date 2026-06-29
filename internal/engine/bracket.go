package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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
	leaves := helper.TreeToLeafArray(tree)

	bracket, err := e.buildBracketFromLeaves(comp, leaves)
	if err != nil {
		return err
	}

	return e.store.SaveBracket(comp.ID, bracket)
}

// generatePoolPreviewBracket builds the in-place knockout bracket for a mixed
// (Pools + Knockout) competition at draw time. Its leaves start as pool-origin
// placeholders ("Pool A-1st", "Pool B-2nd", …) produced by helper.GenerateFinals,
// the same hyphenated labels the Excel Tree sheet uses, and the bracket is
// scheduled here so knockout matches have court/time slots from the start. As
// each pool finishes, ResolveQualifiedPools replaces that pool's placeholders
// with the real finishers IN PLACE (no separate playoffs competition, no manual
// start step); a knockout match becomes scoreable once both its sides resolve.
// The Preview flag is set here and cleared by ResolveQualifiedPools on the first
// seeding; scoring playability is per-match (bracketMatchPlayable), not gated on
// this flag.
//
// No-ops (returns nil without writing bracket.json) when there are no pools
// (nothing to seed a tree from) or when helper.GenerateFinals returns an empty
// list. PoolWinners <= 0 is coerced to 2 (matching the same default in
// ResolveQualifiedPools) rather than treated as "skip", a mixed source with the
// field unset still has a knockout to preview, and matching the resolver default
// ensures the preview shape equals the live knockout bracket.
func (e *Engine) generatePoolPreviewBracket(comp *state.Competition) error {
	pools, err := e.store.LoadPools(comp.ID)
	if err != nil {
		return fmt.Errorf("loading pools for preview bracket: %w", err)
	}
	if len(pools) == 0 {
		return nil
	}

	poolWinners := comp.EffectivePoolWinners()

	finals := helper.GenerateFinals(pools, poolWinners)
	if len(finals) == 0 {
		return nil
	}

	// Mirror the Excel create-pools path: build tree, apply pool adjustments
	// so 1st-place finishers get byes, then flatten to a pow2 leaf array
	// (mp-5ng7). This gives the preview bracket the same topology as the
	// printed Excel bracket.
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	previewLeaves := helper.TreeToLeafArray(tree)

	bracket, err := e.buildBracketFromLeaves(comp, previewLeaves)
	if err != nil {
		return err
	}
	bracket.Preview = true

	return e.store.SaveBracket(comp.ID, bracket)
}

// buildBracketFromLeaves builds a balanced single-elimination bracket from an
// ordered pow2 leaf array. Callers must provide a pow2-length slice produced
// by helper.TreeToLeafArray (which mirrors the Excel bracket topology). Labels
// may be resolved player names (live playoffs) or pool-origin placeholders
// (preview bracket), the tree shape, court assignment, bye resolution, and
// scheduling are identical either way. The caller persists the result (and
// sets Preview when appropriate).
func (e *Engine) buildBracketFromLeaves(comp *state.Competition, leaves []string) (*state.Bracket, error) {
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

	numCourts := len(comp.Courts)
	if numCourts == 0 {
		numCourts = 1
	}
	numRound1Matches := pow2 / 2

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
					sideA = fmt.Sprintf("Winner of r%d-m%d", d+1, i*2)
				}
			}
			sideB := ""
			if n.Right != nil {
				if n.Right.LeafNode {
					sideB = n.Right.LeafVal
				} else {
					sideB = fmt.Sprintf("Winner of r%d-m%d", d+1, i*2+1)
				}
			}

			// If both sides are empty (byes), we might still want to show the match
			// but marked as completed/skipped.

			// Derive court from the first-round slot this match covers.
			// Match (rIdx, i) is rooted above first-round slot i * 2^rIdx.
			firstRoundSlot := i * (1 << rIdx)
			courtIdx := helper.SubtreeCourtIndex(numRound1Matches, numCourts, firstRoundSlot)
			court := ""
			if len(comp.Courts) > 0 {
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
	state.ApplyTournamentDefaults(tournament)
	state.ApplyCompetitionDefaults(comp)
	assignBracketMatchSlots(bracket.Rounds, comp, tournament)

	// Display metadata (mp-7f2w): label each match with its effective round and
	// real feeders so the viewer renders the same effective-round columns as the
	// Excel Tree sheet (structural byes skip a column). Computed once here, while
	// the "Winner of rX-mY" placeholders are still intact, it must NOT be
	// recomputed after results resolve those placeholders into player names.
	computeBracketDisplayMetadata(bracket)

	// Assign sequential match numbers matching the Excel Tree sheet (AC8).
	// Must run AFTER computeBracketDisplayMetadata sets Hidden so the skipping
	// logic is identical to helper.AssignMatchNumbers (nil-node skip in Excel
	// = Hidden or both-sides-empty in the web bracket).
	assignBracketMatchNumbers(bracket)

	return bracket, nil
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
// number by descending DisplayRound (deepest/earliest round first) then by the
// 0-based position parsed from the match ID, identical to both the Excel
// AssignMatchNumbers walk and the JS buildDisplayModel matchNumById ordering.
//
// Skip rule (matches the Excel nil-node skip): Hidden (structural-bye) matches and
// both-sides-empty dead matches are excluded and do not consume a number.
//
// The printed Excel sheet is authoritative. The contract is enforced by
// TestMatchNumberingParity_ExcelVsWeb (match_numbering_parity_test.go), which builds
// both numberings from identical entrant sets, including bye-producing, non-power-
// of-two sizes, and asserts the real-match numbers are identical bout-for-bout.
// If they ever diverge, fix THIS path to match the Excel one.
//
// Must run AFTER computeBracketDisplayMetadata, which sets Hidden / DisplayRound.
func assignBracketMatchNumbers(b *state.Bracket) {
	type ref struct {
		m   *state.BracketMatch
		pos int
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
			real = append(real, ref{m: m, pos: bracketMatchPosFromID(m.ID)})
		}
	}
	// Descending DisplayRound (deepest/earliest round first), then ascending
	// position, mirrors the Excel eliminationMatchRounds walk.
	sort.SliceStable(real, func(i, j int) bool {
		if real[i].m.DisplayRound != real[j].m.DisplayRound {
			return real[i].m.DisplayRound > real[j].m.DisplayRound
		}
		return real[i].pos < real[j].pos
	})
	for i, r := range real {
		r.m.MatchNumber = i + 1
	}
}

// bracketMatchPosFromID extracts the 0-based within-round position from a bracket
// match ID of the form "m-r{ROUND}-{POS}" (e.g. "m-r2-1" → 1). It is the same
// position the JS buildDisplayModel reads from the id suffix when ordering match
// numbers, keeping the Go and JS numbering tie-breaks identical. Returns 0 for any
// unparseable ID (defensive; real IDs always carry a trailing integer).
func bracketMatchPosFromID(id string) int {
	idx := strings.LastIndex(id, "-")
	if idx < 0 || idx == len(id)-1 {
		return 0
	}
	pos, err := strconv.Atoi(id[idx+1:])
	if err != nil {
		return 0
	}
	return pos
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

	// Walk the real feeder graph outward from the final (the lone real match in
	// the last round). It is a tree, so each match is reached exactly once; the
	// DisplayRound != 0 guard also bounds against any unexpected cycle.
	final := at(numRounds-1, 0)
	if !isReal(final) {
		return // degenerate bracket (e.g. < 2 competitors)
	}
	type qItem struct {
		m  *state.BracketMatch
		dr int
	}
	queue := []qItem{{final, 1}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if it.m == nil || it.m.DisplayRound != 0 {
			continue
		}
		it.m.DisplayRound = it.dr
		for _, fid := range it.m.Feeders {
			if fid == "" {
				continue
			}
			if f := byID[fid]; f != nil && f.DisplayRound == 0 {
				queue = append(queue, qItem{f, it.dr + 1})
			}
		}
	}
}
