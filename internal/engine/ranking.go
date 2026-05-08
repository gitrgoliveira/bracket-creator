package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// GetBracketRanking returns the player who achieved rank in the bracket of compID.
// Supported ranks: 1 (winner), 2 (finalist), 3-4 (semi-final losers).
// Full player data (dojo, displayName) is resolved from the source competition's participants.
func (e *Engine) GetBracketRanking(compID string, rank int) (*helper.Player, error) {
	bracket, err := e.store.LoadBracket(compID)
	if err != nil {
		return nil, fmt.Errorf("cannot load bracket for %q: %w", compID, err)
	}
	if bracket == nil || len(bracket.Rounds) == 0 {
		return nil, fmt.Errorf("no bracket data for competition %q", compID)
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
		return nil, fmt.Errorf("rank %d not found in completed bracket for competition %q", rank, compID)
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

	return &helper.Player{Name: winnerName}, nil
}

// resolveReservedSlots replaces placeholder participants (Tag="reserved") with
// real players from source competition bracket results.
func (e *Engine) resolveReservedSlots(compID string, players []helper.Player) ([]helper.Player, error) {
	slots, err := e.store.LoadReservedSlots(compID)
	if err != nil {
		return players, nil
	}
	if len(slots) == 0 {
		return players, nil
	}

	for _, slot := range slots {
		srcComp, err := e.store.LoadCompetition(slot.SourceCompID)
		if err != nil || srcComp == nil {
			return nil, fmt.Errorf("reserved slot source competition %q not found", slot.SourceCompID)
		}
		if srcComp.Status != "playoffs" && srcComp.Status != "completed" {
			return nil, fmt.Errorf("reserved slot source %q has not reached playoffs yet (status: %s)", srcComp.Name, srcComp.Status)
		}

		real, err := e.GetBracketRanking(slot.SourceCompID, slot.SourceRank)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve rank %d from %q: %w", slot.SourceRank, slot.SourceCompID, err)
		}

		for i := range players {
			if players[i].ID == slot.ParticipantID {
				players[i].Name = real.Name
				players[i].DisplayName = real.DisplayName
				players[i].Dojo = real.Dojo
				players[i].Tag = "" // no longer a placeholder
				break
			}
		}
	}

	return players, nil
}
