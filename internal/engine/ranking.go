package engine

import (
	"fmt"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bracketLoserIdentity returns m's LOSING side's name and id (bc-brid),
// resolved via domain.AttributeWinnerSide (the id-first rule, so a same-name
// pair -- two competitors from different dojos, legal per
// CheckDuplicateEntriesByNameDojo -- is not mis-attributed the way a bare
// `loser := m.SideA; if loser == m.Winner { loser = m.SideB }` comparison
// always would: that comparison credits SideA whenever the winner's name
// matches BOTH sides identically, exactly the "always side A" bug this
// codebase's other winner/loser derivations were fixed against). Returns
// ("", "") when the match carries no attributable winner.
func bracketLoserIdentity(m state.BracketMatch) (name, id string) {
	switch domain.AttributeWinnerSide(domain.WinnerAttribution{
		Winner: m.Winner, SideA: m.SideA, SideB: m.SideB,
		WinnerID: m.WinnerID, SideAID: m.SideAID, SideBID: m.SideBID,
	}) {
	case domain.MatchSideA:
		return m.SideB, m.SideBID
	case domain.MatchSideB:
		return m.SideA, m.SideAID
	default:
		return "", ""
	}
}

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
	var winnerName, winnerID string
	switch rank {
	case 1:
		for _, m := range finalRound {
			if m.Status == state.MatchStatusCompleted && m.Winner != "" {
				winnerName, winnerID = m.Winner, m.WinnerID
			}
		}
	case 2:
		for _, m := range finalRound {
			if m.Status == state.MatchStatusCompleted && m.Winner != "" {
				winnerName, winnerID = bracketLoserIdentity(m)
			}
		}
	default:
		if rank <= 4 && len(bracket.Rounds) >= 2 {
			semiRound := bracket.Rounds[len(bracket.Rounds)-2]
			idx := rank - 3
			var semis, semiIDs []string
			for _, m := range semiRound {
				if m.Status == state.MatchStatusCompleted && m.Winner != "" {
					loser, loserID := bracketLoserIdentity(m)
					semis = append(semis, loser)
					semiIDs = append(semiIDs, loserID)
				}
			}
			if idx < len(semis) {
				winnerName, winnerID = semis[idx], semiIDs[idx]
			}
		}
	}

	if winnerName == "" {
		return nil, notFoundErrorf("rank %d not found in completed bracket for competition %q", rank, compID)
	}

	// Resolve full player record from source participants. Prefers the id
	// when the bracket row stamped one (bc-brid): two participants can
	// legally share a display name from different dojos, and a name-only
	// scan would silently pick the FIRST such match rather than the
	// competitor actually holding this rank. Falls back to name when
	// winnerID is empty (an unstamped bracket row -- a bye, an unresolved
	// feeder, or an unrepaired legacy row).
	srcComp, _ := e.store.LoadCompetition(compID)
	withZekken := srcComp != nil && srcComp.EffectiveWithZekkenName()
	srcPlayers, _ := e.store.LoadParticipants(compID, withZekken)
	if winnerID != "" {
		for i := range srcPlayers {
			if srcPlayers[i].ID == winnerID {
				return &srcPlayers[i], nil
			}
		}
	}
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
