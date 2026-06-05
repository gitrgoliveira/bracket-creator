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

// resolvePoolWinnersFromSource resolves pool winners from the competition
// identified by srcID into a player roster. It is the shared core used by
// both the legacy separate-playoffs path (called via resolvePoolWinners) and
// the in-place StartKnockout path.
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
// When preserveIdentity is true, the returned players carry the source's ID
// and Number fields through (so bracket match sides can reference real
// participant UUIDs). When false, ID is left empty so SaveParticipants mints
// a fresh UUID — the legacy separate-playoffs path needs this to avoid column-
// shift hazards when the source carried non-UUID (client-slug) IDs.
//
// The source is required to be in status CompStatusPools or later (i.e., pools
// must be final). Resolution is read-only against the source competition so it
// never contends with the caller's own lock.
func (e *Engine) resolvePoolWinnersFromSource(srcID string, preserveIdentity bool) ([]domain.Player, error) {
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
	totalWinners := len(pools) * winnersPerPool

	players := make([]domain.Player, 0, totalWinners)
	for rank := 1; rank <= totalWinners; rank++ {
		p, err := e.GetPoolRanking(srcID, rank)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve pool winner rank %d from %q: %w", rank, srcID, err)
		}
		player := domain.Player{
			Name:        p.Name,
			DisplayName: p.DisplayName,
			Dojo:        p.Dojo,
		}
		if preserveIdentity {
			// Preserve ID and Number so bracket match sides carry the real
			// participant UUID and display number. Seed/metadata/tag are still
			// dropped — they are stale in a knockout context.
			player.ID = p.ID
			player.Number = p.Number
		}
		// When preserveIdentity is false: ID deliberately left empty so
		// SaveParticipants mints a UUID. That keeps participants.csv column 0 a
		// UUID even when the source carried non-UUID (client-slug) IDs — so the
		// loader's auto-detect parses it correctly regardless of HasParticipantIDs.
		players = append(players, player)
	}
	return players, nil
}

// resolvePoolWinners builds the roster for a playoffs competition that was
// created from a mixed (Pools + Knockout) source via POST /playoffs. It reads
// playoffsComp.SourceCompID, verifies the source is a finalized mixed
// competition, and resolves the qualifying pool winners (ranks
// 1..totalWinners) into real players via GetPoolRanking. Returns the resolved
// roster.
//
// The returned roster carries each winner's display fields (Name/DisplayName/
// Dojo) but NOT the source-inherited ID: the playoffs comp is a brand-new set
// of participants with no prior references, so we leave ID empty and let
// SaveParticipants mint a fresh UUID. See resolvePoolWinnersFromSource for the
// full rationale.
func (e *Engine) resolvePoolWinners(playoffsComp *state.Competition) ([]domain.Player, error) {
	srcID := playoffsComp.SourceCompID
	// Legacy path: require the source to be in complete/playoffs (fully final).
	srcComp, err := e.store.LoadCompetition(srcID)
	if err != nil || srcComp == nil {
		return nil, notFoundErrorf("playoffs source competition %q not found", srcID)
	}
	if srcComp.Status != state.CompStatusComplete && srcComp.Status != state.CompStatusPlayoffs {
		return nil, validationErrorf("source competition %q pool results are not final yet (status: %s)", srcComp.Name, srcComp.Status)
	}
	// Delegate to the shared resolver without identity preservation (legacy
	// behavior: fresh UUIDs for the separate playoffs comp's participants).
	return e.resolvePoolWinnersFromSource(srcID, false)
}
