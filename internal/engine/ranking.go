package engine

import (
	"fmt"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
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

// resolvePoolWinnersFromSource resolves pool winners from the competition
// identified by srcID into a player roster. Used by StartKnockout to promote
// finalists into the mixed competition's own bracket in place.
//
// totalWinners is derived from the source's PERSISTED pools — the actual
// finalized pool count (len(pools)) × PoolWinners — NOT recomputed from
// participant count and PoolSize. The recomputation would use a fixed
// ceiling-division formula, but helper.CreatePools picks the pool count
// differently depending on PoolSizeMode (floor division in "min" mode), so a
// recomputation can disagree with the finalized draw and over-promote
// non-qualifiers. The source is required to be final here, so pools.csv is
// authoritative.
//
// The returned players carry the source's ID and Number fields so that bracket
// match sides can reference real participant UUIDs.
//
// The source is required to be in status CompStatusPools or later (i.e., pools
// must be final). Resolution is read-only against the source competition so it
// never contends with the caller's own lock.
func (e *Engine) resolvePoolWinnersFromSource(srcID string) ([]domain.Player, error) {
	srcComp, err := e.store.LoadCompetition(srcID)
	if err != nil || srcComp == nil {
		return nil, notFoundErrorf("playoffs source competition %q not found", srcID)
	}
	// Only mixed (Pools + Knockout) competitions have pool winners to promote.
	if srcComp.Format != state.CompFormatMixed {
		return nil, validationErrorf("playoffs source %q must be a mixed (Pools + Knockout) competition (got %q)", srcComp.Name, srcComp.Format)
	}
	if srcComp.Status != state.CompStatusComplete && srcComp.Status != state.CompStatusPlayoffs && srcComp.Status != state.CompStatusPools {
		return nil, validationErrorf("source competition %q pool results are not final yet (status: %s)", srcComp.Name, srcComp.Status)
	}

	// Pool count from the finalized pools.csv (authoritative), not recomputed
	// from participant count — see the function comment for why.
	pools, err := e.store.LoadPools(srcID)
	if err != nil {
		return nil, fmt.Errorf("cannot load source pools for %q: %w", srcID, err)
	}
	if len(pools) == 0 {
		return nil, validationErrorf("source competition %q has no pools to promote from", srcComp.Name)
	}
	winnersPerPool := srcComp.PoolWinners
	if winnersPerPool <= 0 {
		winnersPerPool = 2
	}

	// Resolve winners in BRACKET-LEAF order (helper.FinalsSlotOrder), NOT
	// global-rank order. The preview bracket was built from
	// helper.GenerateFinals(pools, winnersPerPool), which cross-seeds finalists
	// (e.g. Pool A-1st vs Pool B-2nd) so pool winners land on opposite ends of
	// the draw. Returning players in global-rank order ([A-1st, B-1st, A-2nd,
	// B-2nd]) and feeding them positionally to buildBracketFromLeaves would put
	// the two pool winners in the SAME first-round match — corrupting the seed.
	// Using the same slot order as GenerateFinals guarantees the resolved
	// bracket matches the previewed one slot-for-slot. (mp-turx seeding fix.)
	standings, err := e.CalculatePoolStandings(srcID)
	if err != nil {
		return nil, fmt.Errorf("cannot compute pool standings for %q: %w", srcID, err)
	}
	slots := helper.FinalsSlotOrder(len(pools), winnersPerPool)
	players := make([]domain.Player, 0, len(slots))
	for _, s := range slots {
		poolIdx, rankIdx := s[0], s[1]
		poolName := pools[poolIdx].PoolName
		ranked, ok := standings[poolName]
		if !ok || rankIdx >= len(ranked) {
			return nil, fmt.Errorf("cannot resolve finalist (pool %q rank %d) from %q", poolName, rankIdx+1, srcID)
		}
		p := ranked[rankIdx].Player
		// Preserve ID and Number so bracket match sides carry the real
		// participant UUID and display number. Seed/metadata/tag are still
		// dropped — they are stale in a knockout context.
		players = append(players, domain.Player{
			Name:        p.Name,
			DisplayName: p.DisplayName,
			Dojo:        p.Dojo,
			ID:          p.ID,
			Number:      p.Number,
		})
	}
	return players, nil
}
