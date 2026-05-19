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

// resolveReservedSlots replaces placeholder participants (Tag="reserved") with
// real players from source competition results (bracket or pools). Returns
// the (possibly-mutated) players slice, a bool indicating whether ANY
// mutation actually happened (in-place field update OR placeholder removal),
// and any error encountered. The mutated flag lets callers gate a trailing
// SaveParticipants on real changes only — re-saving an unmutated snapshot
// would clobber a concurrent participants upload.
func (e *Engine) resolveReservedSlots(compID string, players []domain.Player) ([]domain.Player, bool, error) {
	// LoadReservedSlots returns ([]ReservedSlot{}, nil) for the "file does
	// not exist" case (see parseReservedSlotsFile). Any other error from
	// LoadReservedSlots is a genuine I/O / parse failure (corrupt JSON,
	// permission, etc.). Previously this swallowed the error and returned
	// (players, false, nil), making the caller think "no slots, no save
	// needed" — but a corrupt slots file would silently proceed to generate
	// pools / bracket with the placeholder "Reserved: rank N" entries left
	// in `players`, since the resolution step was skipped. Propagate the
	// error so StartCompetition aborts before generating broken artifacts.
	slots, err := e.store.LoadReservedSlots(compID)
	if err != nil {
		return nil, false, fmt.Errorf("cannot load reserved slots for %q: %w", compID, err)
	}
	if len(slots) == 0 {
		return players, false, nil
	}

	slotsChanged := false
	playersMutated := false
	var toRemove []string

	for sIdx, slot := range slots {
		srcComp, err := e.store.LoadCompetition(slot.SourceCompID)
		if err != nil || srcComp == nil {
			return nil, false, notFoundErrorf("reserved slot source competition %q not found", slot.SourceCompID)
		}

		var real *domain.Player
		if srcComp.Format == state.CompFormatPools || srcComp.Format == state.CompFormatLeague {
			if srcComp.Status != state.CompStatusComplete && srcComp.Status != state.CompStatusPlayoffs {
				return nil, false, validationErrorf("reserved slot source %q pool results are not final yet (status: %s)", srcComp.Name, srcComp.Status)
			}
			real, err = e.GetPoolRanking(slot.SourceCompID, slot.SourceRank)
		} else {
			if srcComp.Status != state.CompStatusPlayoffs && srcComp.Status != state.CompStatusComplete {
				return nil, false, validationErrorf("reserved slot source %q has not reached playoffs yet (status: %s)", srcComp.Name, srcComp.Status)
			}
			real, err = e.GetBracketRanking(slot.SourceCompID, slot.SourceRank)
		}

		if err != nil {
			return nil, false, fmt.Errorf("cannot resolve rank %d from %q: %w", slot.SourceRank, slot.SourceCompID, err)
		}

		// Find placeholder and check for existing name
		var existingIdx = -1
		var placeholderIdx = -1
		for i := range players {
			if players[i].ID == slot.ParticipantID {
				placeholderIdx = i
			} else if players[i].Name == real.Name {
				existingIdx = i
			}
		}

		if placeholderIdx == -1 {
			continue // Already resolved or removed?
		}

		if existingIdx != -1 {
			// Player already exists! Link slot to existing player and remove placeholder
			slots[sIdx].ParticipantID = players[existingIdx].ID
			slotsChanged = true
			toRemove = append(toRemove, slot.ParticipantID)
		} else {
			// Update placeholder with real player info
			players[placeholderIdx].Name = real.Name
			players[placeholderIdx].DisplayName = real.DisplayName
			players[placeholderIdx].Dojo = real.Dojo
			players[placeholderIdx].Tag = "" // no longer a placeholder
			playersMutated = true
		}
	}

	if slotsChanged {
		if err := e.store.SaveReservedSlots(compID, slots); err != nil {
			return nil, false, fmt.Errorf("failed to save updated reserved slots: %w", err)
		}
	}

	if len(toRemove) > 0 {
		filtered := make([]domain.Player, 0, len(players))
		removeMap := make(map[string]bool)
		for _, id := range toRemove {
			removeMap[id] = true
		}
		for _, p := range players {
			if !removeMap[p.ID] {
				filtered = append(filtered, p)
			}
		}
		players = filtered
		playersMutated = true
	}

	return players, playersMutated, nil
}
