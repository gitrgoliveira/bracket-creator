package engine

import (
	"fmt"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// generatePlayoffs builds and saves an elimination bracket. sourceLinked must
// be true when players were resolved from a source competition via
// resolvePoolWinners; false for manually-populated or standalone rosters.
// When sourceLinked the bracket topology mirrors the pool preview bracket
// (GenerateFinals ordering + pool adjustments); standalone uses
// StandardSeeding + CreateBalancedTree, matching the Excel create-playoffs
// path (mp-5ng7).
func (e *Engine) generatePlayoffs(comp *state.Competition, players []domain.Player, seeds []domain.SeedAssignment, sourceLinked bool) error {
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

	var leaves []string
	if sourceLinked {
		var err error
		leaves, err = e.buildSourceLinkedLeaves(comp)
		if err != nil {
			return err
		}
	} else {
		// StandardSeeding → CreateBalancedTree → TreeToLeafArray mirrors the
		// Excel create-playoffs path exactly (mp-5ng7). The unbalanced tree's
		// structural byes are embedded as "" slots in the pow2 array.
		seededPlayers := helper.StandardSeeding(players)
		names := make([]string, len(seededPlayers))
		for i, p := range seededPlayers {
			names[i] = p.Name
		}
		tree := helper.CreateBalancedTree(names)
		leaves = helper.TreeToLeafArray(tree)
	}

	bracket, err := e.buildBracketFromLeaves(comp, leaves)
	if err != nil {
		return err
	}

	return e.store.SaveBracket(comp.ID, bracket)
}

// generatePoolPreviewBracket builds a PREVIEW elimination bracket for a mixed
// (Pools + Knockout) competition at draw time. Its leaves are pool-origin
// placeholders ("Pool A 1st", "Pool B 2nd", …) produced by
// helper.GenerateFinals — the same labels the Excel Tree sheet uses — so the
// operator can see, on the source competition, the knockout structure that the
// pools feed (mp-9dz). The bracket is flagged Preview so the UI renders it
// read-only: the live knockout is played in the separate playoffs competition
// created via POST /competitions/:id/playoffs.
//
// No-ops (returns nil without writing bracket.json) when there are no pools
// (nothing to seed a tree from) or when helper.GenerateFinals returns an empty
// list. PoolWinners <= 0 is coerced to 2 (mirroring resolvePoolWinners' default
// used when the playoffs competition is started) rather than treated as
// "skip" — a mixed source with the field unset still has a knockout to
// preview, and matching resolvePoolWinners ensures the preview shape equals
// what the operator will get when they click "Create playoff bracket".
func (e *Engine) generatePoolPreviewBracket(comp *state.Competition) error {
	pools, err := e.store.LoadPools(comp.ID)
	if err != nil {
		return fmt.Errorf("loading pools for preview bracket: %w", err)
	}
	if len(pools) == 0 {
		return nil
	}

	poolWinners := comp.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2 // mirror resolvePoolWinners' default — see doc comment
	}

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

// buildSourceLinkedLeaves builds the ordered leaf array for a playoffs
// competition that is source-linked to a finished mixed comp (SourceCompID
// != ""). The topology (bye positions, court grouping) matches the pool
// preview bracket generated for the source comp, and placeholders are
// replaced with the actual pool-standings winners so the live bracket
// names are resolved at draw time (mp-5ng7).
func (e *Engine) buildSourceLinkedLeaves(comp *state.Competition) ([]string, error) {
	srcID := comp.SourceCompID
	srcComp, err := e.store.LoadCompetition(srcID)
	if err != nil || srcComp == nil {
		return nil, notFoundErrorf("playoffs source competition %q not found", srcID)
	}

	pools, err := e.store.LoadPools(srcID)
	if err != nil {
		return nil, fmt.Errorf("loading source pools for %q: %w", srcID, err)
	}

	poolWinners := srcComp.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2
	}

	// Build placeholder array in the same order the preview bracket uses,
	// then apply pool adjustments so the topology is identical.
	finals := helper.GenerateFinals(pools, poolWinners)
	if len(finals) == 0 {
		return nil, validationErrorf("source competition %q has no pool finalists", srcComp.Name)
	}
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	leaves := helper.TreeToLeafArray(tree)

	// Build resolver: placeholder key "Pool X-Nth" → actual player name.
	standings, err := e.CalculatePoolStandings(srcID)
	if err != nil {
		return nil, fmt.Errorf("calculating pool standings for %q: %w", srcID, err)
	}
	resolver := make(map[string]string, len(finals))
	for _, pool := range pools {
		poolStandings := standings[pool.PoolName]
		for rank := 1; rank <= poolWinners && rank-1 < len(poolStandings); rank++ {
			key := fmt.Sprintf("%s-%s", pool.PoolName, helper.GetOrdinal(rank))
			resolver[key] = poolStandings[rank-1].Player.Name
		}
	}

	// Replace placeholders; leave empty slots ("") as-is (they are byes).
	for i, leaf := range leaves {
		if leaf == "" {
			continue
		}
		if name, ok := resolver[leaf]; ok {
			leaves[i] = name
		}
	}

	return leaves, nil
}

// buildBracketFromLeaves builds a balanced single-elimination bracket from an
// ordered pow2 leaf array. Callers must provide a pow2-length slice produced
// by helper.TreeToLeafArray (which mirrors the Excel bracket topology). Labels
// may be resolved player names (live playoffs) or pool-origin placeholders
// (preview bracket) — the tree shape, court assignment, bye resolution, and
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
				// assignBracketMatchSlots — uniform start times
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

	return bracket, nil
}
