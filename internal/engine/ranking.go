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
	withZekken := srcComp != nil && srcComp.WithZekkenName
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

// resolvePoolWinners builds the roster for a playoffs competition that was
// created from a mixed (Pools + Knockout) source via POST /playoffs. It reads
// playoffsComp.SourceCompID, verifies the source's pools are final, and
// resolves the qualifying pool winners (ranks 1..totalWinners) into real
// players via GetPoolRanking. Returns the resolved roster.
//
// totalWinners is recomputed from the source's final pool configuration
// (numPools × PoolWinners, with the same defaults the POST /playoffs handler
// uses) rather than stored at playoffs-creation time, so it always reflects
// the source as drawn — immune to source-roster edits made between playoffs
// creation and the source draw.
//
// The returned roster carries the source players' identities; the caller
// persists it (participants.csv was empty on disk for a source-linked playoffs
// comp) and flips HasParticipantIDs. Resolution is read-only against the
// source competition, so it never contends with the playoffs comp's own lock.
func (e *Engine) resolvePoolWinners(playoffsComp *state.Competition) ([]domain.Player, error) {
	srcID := playoffsComp.SourceCompID
	srcComp, err := e.store.LoadCompetition(srcID)
	if err != nil || srcComp == nil {
		return nil, notFoundErrorf("playoffs source competition %q not found", srcID)
	}
	if srcComp.Status != state.CompStatusComplete && srcComp.Status != state.CompStatusPlayoffs {
		return nil, validationErrorf("source competition %q pool results are not final yet (status: %s)", srcComp.Name, srcComp.Status)
	}

	// Mirror the POST /playoffs handler's sizing: numPools × winnersPerPool
	// with the same poolSize/winners defaults.
	parts, err := e.store.LoadParticipants(srcID, srcComp.WithZekkenName)
	if err != nil {
		return nil, fmt.Errorf("cannot load source participants for %q: %w", srcID, err)
	}
	poolSize := srcComp.PoolSize
	if poolSize <= 0 {
		poolSize = 3
	}
	numPools := (len(parts) + poolSize - 1) / poolSize
	winnersPerPool := srcComp.PoolWinners
	if winnersPerPool <= 0 {
		winnersPerPool = 2
	}
	totalWinners := numPools * winnersPerPool

	players := make([]domain.Player, 0, totalWinners)
	for rank := 1; rank <= totalWinners; rank++ {
		p, err := e.GetPoolRanking(srcID, rank)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve pool winner rank %d from %q: %w", rank, srcID, err)
		}
		players = append(players, *p)
	}
	return players, nil
}
