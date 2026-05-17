package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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

	// Create balanced tree
	leaves := make([]string, len(seededPlayers))
	for i, p := range seededPlayers {
		leaves[i] = p.Name
	}

	// NextPow2 ensures we have a balanced tree with enough slots
	pow2 := helper.NextPow2(len(leaves))
	leafValues := make([]string, pow2)
	for i := 0; i < pow2; i++ {
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

	// Per-court slot assignment (T150) + ceremony-block skipping
	// (T151). See pools.go for the same wiring; tournament load
	// failures abort the start so the operator notices the missing
	// schedule data rather than silently shipping a uniform-start
	// bracket.
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return err
	}
	state.ApplyTournamentDefaults(tournament)
	state.ApplyCompetitionDefaults(comp)
	assignBracketMatchSlots(bracket.Rounds, comp, tournament)

	return e.store.SaveBracket(comp.ID, bracket)
}
