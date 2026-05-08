package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// errMatchNotFound is returned by withPoolMatch / withBracketMatch when no
// match with the given ID exists in the respective data store.
var errMatchNotFound = errors.New("match not found")

// withPoolMatch loads pool matches, calls mutate on the one matching matchId,
// and saves the updated slice.  Returns errMatchNotFound (unwrapped) when the
// ID is not present so callers can fall through to the bracket store.
func (e *Engine) withPoolMatch(compId, matchId string, mutate func(*state.MatchResult)) error {
	results, err := e.store.LoadPoolMatches(compId)
	if err != nil {
		return err
	}
	for i := range results {
		if results[i].ID == matchId {
			mutate(&results[i])
			return e.store.SavePoolMatches(compId, results)
		}
	}
	return errMatchNotFound
}

// withBracketMatch loads the bracket, calls mutate on the match matching
// matchId, and saves the updated bracket.  Returns errMatchNotFound when not
// present.
func (e *Engine) withBracketMatch(compId, matchId string, mutate func(*state.BracketMatch)) error {
	bracket, err := e.store.LoadBracket(compId)
	if err != nil {
		return err
	}
	if bracket == nil {
		return fmt.Errorf("bracket not found for competition %s", compId)
	}
	for rIdx := range bracket.Rounds {
		for mIdx := range bracket.Rounds[rIdx] {
			if bracket.Rounds[rIdx][mIdx].ID == matchId {
				mutate(&bracket.Rounds[rIdx][mIdx])
				return e.store.SaveBracket(compId, bracket)
			}
		}
	}
	return errMatchNotFound
}

func (e *Engine) RecordMatchResult(compId string, matchId string, result state.MatchResult) error {
	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		*r = result
		r.ID = matchId // payload may arrive ID-less
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, errMatchNotFound) {
		return err
	}
	return e.recordBracketMatchResult(compId, matchId, result)
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

	comp, _ := e.store.LoadCompetition(compId)
	isTeam := comp != nil && comp.TeamSize > 0

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

			// Team W/L/D (or individual W/L/D)
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

			if isTeam && len(m.SubResults) > 0 {
				for _, sub := range m.SubResults {
					sideAWin := sub.Winner == m.SideA || sub.Winner == sub.SideA
					sideBWin := sub.Winner == m.SideB || sub.Winner == sub.SideB
					switch {
					case sideAWin:
						sA.IndividualWins++
						sB.IndividualLosses++
					case sideBWin:
						sB.IndividualWins++
						sA.IndividualLosses++
					case sub.Winner == "":
						sA.IndividualDraws++
						sB.IndividualDraws++
					}
					sA.PointsWon += len(sub.IpponsA)
					sA.PointsLost += len(sub.IpponsB)
					sB.PointsWon += len(sub.IpponsB)
					sB.PointsLost += len(sub.IpponsA)
				}
			} else {
				// Individual scoring: ippons at match level
				sA.IpponsGiven += len(m.IpponsA)
				sA.IpponsTaken += len(m.IpponsB)
				sB.IpponsGiven += len(m.IpponsB)
				sB.IpponsTaken += len(m.IpponsA)
			}
		}

		var sorted []state.PlayerStanding
		for _, s := range playerStandings {
			if isTeam {
				// Team weighted score (Excel formula):
				// W × 1B − L × 10M + T × 100K + IV × 1000 − IL × 100 + IT × 10 + PW − PL × 0.01
				// Scaled by 100 to use integers:
				s.Points = s.Wins*100_000_000_000 - s.Losses*1_000_000_000 + s.Draws*10_000_000 +
					s.IndividualWins*100_000 - s.IndividualLosses*10_000 + s.IndividualDraws*1_000 +
					s.PointsWon*100 - s.PointsLost
			} else {
				// Individual weighted score (Excel formula):
				// W × 1,000,000 − L × 10,000 + D × 100 + PW × 1 − PL × 0.01
				// Scaled by 100 to use integers:
				s.Points = s.Wins*100_000_000 - s.Losses*1_000_000 + s.Draws*10_000 + s.IpponsGiven*100 - s.IpponsTaken
			}
			sorted = append(sorted, *s)
		}

		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Points > sorted[j].Points
		})

		// 4. Apply manual rank overrides
		overrides, _ := e.store.LoadOverrides(compId)
		if overrides != nil && overrides.PoolRanks[p.PoolName] != nil {
			poolOverrides := overrides.PoolRanks[p.PoolName]
			// Sort again by rank override
			sort.Slice(sorted, func(i, j int) bool {
				rankI, okI := poolOverrides[sorted[i].Player.Name]
				rankJ, okJ := poolOverrides[sorted[j].Player.Name]
				if okI && okJ {
					return rankI < rankJ
				}
				if okI {
					return true
				}
				if okJ {
					return false
				}
				// Fallback to original order (computed ranks)
				return sorted[i].Rank < sorted[j].Rank
			})
		}

		for i := range sorted {
			sorted[i].Rank = i + 1
			if overrides != nil && overrides.PoolRanks[p.PoolName] != nil {
				if _, ok := overrides.PoolRanks[p.PoolName][sorted[i].Player.Name]; ok {
					sorted[i].IsOverridden = true
				}
			}
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

func (e *Engine) UpdateMatchCourt(compId string, matchId string, newCourt string) error {
	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		r.Court = newCourt
	})
	if err == nil {
		return e.GenerateSchedule(compId)
	}
	if !errors.Is(err, errMatchNotFound) {
		return err
	}
	if err = e.withBracketMatch(compId, matchId, func(m *state.BracketMatch) {
		m.Court = newCourt
	}); err != nil {
		return err
	}
	return e.GenerateSchedule(compId)
}

func (e *Engine) OverrideBracketWinner(compId string, matchId string, winnerName string) error {
	bracket, err := e.store.LoadBracket(compId)
	if err != nil {
		return err
	}

	found := false
	for rIdx := 0; rIdx < len(bracket.Rounds); rIdx++ {
		for mIdx := 0; mIdx < len(bracket.Rounds[rIdx]); mIdx++ {
			m := &bracket.Rounds[rIdx][mIdx]
			if m.ID == matchId {
				m.Winner = winnerName
				m.IsOverridden = true
				m.Status = state.MatchStatusCompleted
				found = true

				// Propagate winner to next rounds
				currentWinner := winnerName
				currentRIdx := rIdx
				currentMIdx := mIdx

				for currentRIdx < len(bracket.Rounds)-1 {
					nextMatchIdx := currentMIdx / 2
					nextMatch := &bracket.Rounds[currentRIdx+1][nextMatchIdx]

					if currentMIdx%2 == 0 {
						nextMatch.SideA = currentWinner
					} else {
						nextMatch.SideB = currentWinner
					}

					if nextMatch.SideA != "" && nextMatch.SideB == "" {
						nextMatch.Winner = nextMatch.SideA
						nextMatch.Status = state.MatchStatusCompleted
						currentWinner = nextMatch.SideA
						currentRIdx++
						currentMIdx = nextMatchIdx
					} else if nextMatch.SideA == "" && nextMatch.SideB != "" {
						nextMatch.Winner = nextMatch.SideB
						nextMatch.Status = state.MatchStatusCompleted
						currentWinner = nextMatch.SideB
						currentRIdx++
						currentMIdx = nextMatchIdx
					} else {
						break
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

	if err := e.store.SaveWinnerOverride(compId, matchId, winnerName); err != nil {
		return err
	}

	return e.store.SaveBracket(compId, bracket)
}

func (e *Engine) UpdateMatchTime(compId string, matchId string, scheduledAt string) error {
	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		r.ScheduledAt = scheduledAt
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, errMatchNotFound) {
		return err
	}
	return e.withBracketMatch(compId, matchId, func(m *state.BracketMatch) {
		m.ScheduledAt = scheduledAt
	})
}
