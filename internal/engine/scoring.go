package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func (e *Engine) RecordMatchResult(compId string, matchId string, result state.MatchResult) error {
	// 1. Identify if it's a pool match or bracket match
	if strings.Contains(matchId, "Pool") {
		return e.recordPoolMatchResult(compId, matchId, result)
	}
	return e.recordBracketMatchResult(compId, matchId, result)
}

func (e *Engine) recordPoolMatchResult(compId string, matchId string, result state.MatchResult) error {
	results, err := e.store.LoadPoolMatches(compId)
	if err != nil {
		return err
	}

	found := false
	for i, r := range results {
		if r.ID == matchId {
			results[i] = result
			results[i].ID = matchId
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("pool match %s not found", matchId)
	}

	return e.store.SavePoolMatches(compId, results)
}

func (e *Engine) CalculatePoolStandings(compId string) (map[string][]state.PlayerStanding, error) {
	pools, err := e.store.LoadPools(compId)
	if err != nil {
		return nil, err
	}
	results, err := e.store.LoadPoolMatches(compId)
	if err != nil {
		return nil, err
	}

	// Map match results by pool — IDs are formatted as "PoolName-MatchIdx"
	poolResults := make(map[string][]state.MatchResult)
	for _, r := range results {
		parts := strings.SplitN(r.ID, "-", 2)
		if len(parts) == 2 {
			poolResults[parts[0]] = append(poolResults[parts[0]], r)
		}
	}

	allStandings := make(map[string][]state.PlayerStanding)
	for _, p := range pools {
		matches := poolResults[p.PoolName]
		playerStandings := make(map[string]*state.PlayerStanding)
		for _, player := range p.Players {
			playerStandings[player.Name] = &state.PlayerStanding{
				Player: player,
			}
		}

		for _, m := range matches {
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			sA := playerStandings[m.SideA]
			sB := playerStandings[m.SideB]
			if sA == nil || sB == nil {
				continue
			}

			// Wins/Losses
			if m.Winner == m.SideA {
				sA.Wins++
				sB.Losses++
			} else if m.Winner == m.SideB {
				sB.Wins++
				sA.Losses++
			} else if m.Decision == "hikewake" || m.Winner == "" {
				sA.Draws++
				sB.Draws++
			}

			// Ippons
			sA.IpponsGiven += len(m.IpponsA)
			sA.IpponsTaken += len(m.IpponsB)
			sB.IpponsGiven += len(m.IpponsB)
			sB.IpponsTaken += len(m.IpponsA)
		}

		var sorted []state.PlayerStanding
		for _, s := range playerStandings {
			sorted = append(sorted, *s)
		}

		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Wins != sorted[j].Wins {
				return sorted[i].Wins > sorted[j].Wins
			}
			if sorted[i].IpponsGiven != sorted[j].IpponsGiven {
				return sorted[i].IpponsGiven > sorted[j].IpponsGiven
			}
			return sorted[i].IpponsTaken < sorted[j].IpponsTaken
		})

		for i := range sorted {
			sorted[i].Rank = i + 1
		}
		allStandings[p.PoolName] = sorted
	}

	return allStandings, nil
}

func (e *Engine) recordBracketMatchResult(compId string, matchId string, result state.MatchResult) error {
	bracket, err := e.store.LoadBracket(compId)
	if err != nil {
		return err
	}
	if bracket == nil {
		return fmt.Errorf("bracket not found for competition %s", compId)
	}

	found := false
	for rIdx, round := range bracket.Rounds {
		for mIdx, m := range round {
			if m.ID == matchId {
				bracket.Rounds[rIdx][mIdx].Winner = result.Winner
				bracket.Rounds[rIdx][mIdx].Status = state.MatchStatusCompleted
				bracket.Rounds[rIdx][mIdx].ScoreA = formatScore(result.IpponsA, result.HansokuA)
				bracket.Rounds[rIdx][mIdx].ScoreB = formatScore(result.IpponsB, result.HansokuB)
				found = true

				// Propagate winner to next round
				if rIdx < len(bracket.Rounds)-1 {
					nextMatchIdx := mIdx / 2
					if mIdx%2 == 0 {
						bracket.Rounds[rIdx+1][nextMatchIdx].SideA = result.Winner
					} else {
						bracket.Rounds[rIdx+1][nextMatchIdx].SideB = result.Winner
					}
				}
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return fmt.Errorf("bracket match %s not found", matchId)
	}

	return e.store.SaveBracket(compId, bracket)
}

func formatScore(ippons []string, hansoku int) string {
	score := strings.Join(ippons, "")
	if hansoku > 0 {
		if score != "" {
			score += " "
		}
		score += fmt.Sprintf("(H%d)", hansoku)
	}
	return score
}
