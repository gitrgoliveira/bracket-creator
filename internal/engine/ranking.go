package engine

import (
	"fmt"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// GetBracketRanking returns the player who achieved rank in the bracket of compID.
// Supported ranks: 1 (winner), 2 (finalist), 3-4 (semi-final losers).
// Full player data (dojo, displayName) is resolved from the source competition's participants.
func (e *Engine) GetBracketRanking(compID string, rank int) (*domain.Player, error) {
	bracket, err := e.store.LoadBracket(compID)
	if err != nil {
		return nil, fmt.Errorf("cannot load bracket for %q: %w", compID, err)
	}
	if bracket == nil || len(bracket.Rounds) == 0 {
		return nil, notFoundErrorf("no bracket data for competition %q", compID)
	}

	finalRound := bracket.Rounds[len(bracket.Rounds)-1]
	var winnerName string
	switch rank {
	case 1:
		for _, m := range finalRound {
			if m.Status == state.MatchStatusCompleted && m.Winner != "" {
				winnerName = m.Winner
			}
		}
	case 2:
		for _, m := range finalRound {
			if m.Status == state.MatchStatusCompleted && m.Winner != "" {
				loser := m.SideA
				if loser == m.Winner {
					loser = m.SideB
				}
				winnerName = loser
			}
		}
	default:
		if rank <= 4 && len(bracket.Rounds) >= 2 {
			semiRound := bracket.Rounds[len(bracket.Rounds)-2]
			idx := rank - 3
			var semis []string
			for _, m := range semiRound {
				if m.Status == state.MatchStatusCompleted && m.Winner != "" {
					loser := m.SideA
					if loser == m.Winner {
						loser = m.SideB
					}
					semis = append(semis, loser)
				}
			}
			if idx < len(semis) {
				winnerName = semis[idx]
			}
		}
	}

	if winnerName == "" {
		return nil, notFoundErrorf("rank %d not found in completed bracket for competition %q", rank, compID)
	}

	// Resolve full player record from source participants.
	srcComp, _ := e.store.LoadCompetition(compID)
	withZekken := srcComp != nil && srcComp.EffectiveWithZekkenName()
	srcPlayers, _ := e.store.LoadParticipants(compID, withZekken)
	for i := range srcPlayers {
		if srcPlayers[i].Name == winnerName {
			return &srcPlayers[i], nil
		}
	}

	return &domain.Player{Name: winnerName}, nil
}

// GetPoolRanking returns the player who achieved rank in the pool standings of compID.
// If multiple pools exist, SourceRank is treated as a global index across all pools
// ordered by pool name (e.g., Rank 1 = Winner of Pool 1, Rank 2 = Winner of Pool 2).
func (e *Engine) GetPoolRanking(compID string, rank int) (*domain.Player, error) {
	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}

	if len(standings) == 0 {
		return nil, notFoundErrorf("no pool standings found for competition %q", compID)
	}

	// Sort pool names to ensure deterministic rank mapping
	var poolNames []string
	for k := range standings {
		poolNames = append(poolNames, k)
	}
	sort.Strings(poolNames)

	// If rank is within pool winner range (e.g., 16 pools, rank 1-16)
	// We map it to the first player of each pool.
	poolIdx := (rank - 1) % len(poolNames)
	rankInPool := (rank - 1) / len(poolNames)

	poolName := poolNames[poolIdx]
	poolStandings, ok := standings[poolName]
	if !ok {
		return nil, notFoundErrorf("pool %q not found in standings for competition %q", poolName, compID)
	}

	if rankInPool < len(poolStandings) {
		return &poolStandings[rankInPool].Player, nil
	}

	return nil, notFoundErrorf("rank %d (calculated as Pool %q, Index %d) not found in pool standings for competition %q",
		rank, poolName, rankInPool, compID)
}
