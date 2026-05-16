package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// errMatchNotFound is returned by withPoolMatch / withBracketMatch when no
// match with the given ID exists in the respective data store.
var errMatchNotFound = errors.New("match not found")

// withPoolMatch atomically loads pool matches, calls mutate on the one
// matching matchId, and saves the updated slice. Returns errMatchNotFound
// (unwrapped) when the ID is not present so callers can fall through to
// the bracket store.
//
// Delegates to state.Store.UpdatePoolMatchByID so the entire
// load + find + mutate + save sequence runs under the per-competition
// lock. Pre-atomic-primitive, two operators scoring different matches
// on different courts could each LoadPoolMatches into separate copies,
// mutate their target match, and SavePoolMatches in sequence — the
// later save would overwrite the earlier save's mutation with stale
// data for the OTHER match. One operator's score would be silently
// lost during a live tournament.
func (e *Engine) withPoolMatch(compId, matchId string, mutate func(*state.MatchResult)) error {
	found, err := e.store.UpdatePoolMatchByID(compId, matchId, mutate)
	if err != nil {
		return err
	}
	if !found {
		return errMatchNotFound
	}
	return nil
}

// withBracketMatch atomically loads the bracket, calls mutate on the
// match matching matchId, and saves the updated bracket. Returns
// errMatchNotFound when not present (so RecordMatchResult callers
// fall through cleanly when neither pool-match nor bracket-match
// has that ID).
//
// Same TOCTOU-closure rationale as withPoolMatch: delegates to
// state.Store.UpdateBracket which holds the per-competition lock
// across load + mutate + save. Returning errMatchNotFound from the
// mutate closure is how we signal "don't save the unchanged bracket
// back" — see UpdateBracket's docstring.
func (e *Engine) withBracketMatch(compId, matchId string, mutate func(*state.BracketMatch)) error {
	return e.store.UpdateBracket(compId, func(bracket *state.Bracket) error {
		if bracket == nil {
			return errMatchNotFound
		}
		for rIdx := range bracket.Rounds {
			for mIdx := range bracket.Rounds[rIdx] {
				if bracket.Rounds[rIdx][mIdx].ID == matchId {
					mutate(&bracket.Rounds[rIdx][mIdx])
					return nil
				}
			}
		}
		return errMatchNotFound
	})
}

func (e *Engine) RecordMatchResult(compId string, matchId string, result *state.MatchResult) error {
	result.ID = matchId // normalize ID-less payloads before overwriting
	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		// Preserve scheduling and side fields if missing in the update payload.
		// The scoring UI only sends scoring-related fields; without this guard,
		// `*r = *result` would zero Court/ScheduledAt/SideA/SideB.
		if result.SideA == "" {
			result.SideA = r.SideA
		}
		if result.SideB == "" {
			result.SideB = r.SideB
		}
		if result.Court == "" {
			result.Court = r.Court
		}
		if result.ScheduledAt == "" {
			result.ScheduledAt = r.ScheduledAt
		}
		*r = *result
	})
	if err != nil {
		if !errors.Is(err, errMatchNotFound) {
			return err
		}
		if err := e.recordBracketMatchResult(compId, matchId, result); err != nil {
			return err
		}
	}
	// T085 — persist the loser's competitor-status when this update
	// recorded a kiken or fusenpai decision. Failure to write the
	// status is non-fatal to the score-recording itself; the handler
	// will log and may surface a degraded-broadcast warning.
	return e.recordIneligibilityFromDecision(compId, matchId, result)
}

func (e *Engine) CalculatePoolStandings(compId string) (map[string][]state.PlayerStanding, error) {
	// Fast path: return cached result when neither pool-matches nor overrides changed.
	pmMtime := e.store.FileMtime(compId, "pool-matches.csv")
	ovMtime := e.store.FileMtime(compId, "overrides.json")
	if v, ok := e.standingsCache.Load(compId); ok {
		cached := v.(*standingsCacheEntry)
		if cached.poolMatchesMtime == pmMtime && cached.overridesMtime == ovMtime {
			return cached.result, nil
		}
	}

	// Single-flight: collapse concurrent cold-cache callers into one compute.
	flightV, _ := e.standingsFlight.LoadOrStore(compId, &sync.Once{})
	once := flightV.(*sync.Once)
	var (
		flightResult map[string][]state.PlayerStanding
		flightErr    error
	)
	once.Do(func() {
		defer e.standingsFlight.Delete(compId)
		flightResult, flightErr = e.computeStandings(compId)
		if flightErr == nil {
			e.standingsCache.Store(compId, &standingsCacheEntry{
				poolMatchesMtime: pmMtime,
				overridesMtime:   ovMtime,
				result:           flightResult,
			})
		}
	})
	if flightErr != nil {
		return nil, flightErr
	}
	if flightResult != nil {
		return flightResult, nil
	}
	// Lost the flight race — read from cache populated by the winner.
	if v, ok := e.standingsCache.Load(compId); ok {
		return v.(*standingsCacheEntry).result, nil
	}
	// Narrow window: cache was invalidated between Do completion and this Load.
	return e.CalculatePoolStandings(compId)
}

func (e *Engine) computeStandings(compId string) (map[string][]state.PlayerStanding, error) {
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
			} else if state.IsDraw(m.Decision) || m.Winner == "" {
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
				s.ScoreSummary = fmt.Sprintf("W:%d L:%d D:%d | IV:%d IL:%d IT:%d | PW:%d PL:%d",
					s.Wins, s.Losses, s.Draws,
					s.IndividualWins, s.IndividualLosses, s.IndividualDraws,
					s.PointsWon, s.PointsLost)
			} else {
				// Individual weighted score (Excel formula):
				// W × 1,000,000 − L × 10,000 + D × 100 + PW × 1 − PL × 0.01
				// Scaled by 100 to use integers:
				s.Points = s.Wins*100_000_000 - s.Losses*1_000_000 + s.Draws*10_000 + s.IpponsGiven*100 - s.IpponsTaken
				s.ScoreSummary = fmt.Sprintf("W:%d L:%d D:%d | P:%d-%d",
					s.Wins, s.Losses, s.Draws, s.IpponsGiven, s.IpponsTaken)
			}
			sorted = append(sorted, *s)
		}

		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Points > sorted[j].Points
		})

		// Apply manual rank overrides
		overrides, _ := e.store.LoadOverrides(compId)
		if overrides != nil && overrides.PoolRanks[p.PoolName] != nil {
			poolOverrides := overrides.PoolRanks[p.PoolName]
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

// recordBracketMatchResult is the main bracket-side scoring path. It
// runs the entire mutation (find target match, set winner/status/
// scores, propagate winner to subsequent rounds) under the per-
// competition lock via state.Store.UpdateBracket so two operators
// scoring different elimination-round matches in the same competition
// can't lose each other's mutations through TOCTOU.
//
// Pre-atomic-primitive, LoadBracket + mutate + SaveBracket ran
// without a shared lock between Load and Save; the propagateBracketWinner
// step amplified the risk because it mutates ADJACENT bracket cells
// (the next-round match), so a concurrent save with a stale view
// could clobber another operator's propagation too.
func (e *Engine) recordBracketMatchResult(compId string, matchId string, result *state.MatchResult) error {
	return e.store.UpdateBracket(compId, func(bracket *state.Bracket) error {
		if bracket == nil {
			return notFoundErrorf("bracket not found for competition %s", compId)
		}

		found := false
		for rIdx, round := range bracket.Rounds {
			for mIdx, m := range round {
				if m.ID == matchId {
					bracket.Rounds[rIdx][mIdx].Winner = result.Winner
					// Preserve incoming Status — pre-fix this was
					// unconditionally set to Completed, so the scoring
					// modal's "Start" tap (which sends
					// `{status: "running"}`) immediately persisted the
					// bracket match as completed with no winner. Mirrors
					// the pool match path (recordMatchResult above) which
					// copies the full result. Default to Completed when
					// status is empty (backward-compat with older
					// scoring payloads that didn't include the field).
					status := result.Status
					if status == "" {
						status = state.MatchStatusCompleted
					}
					bracket.Rounds[rIdx][mIdx].Status = status
					bracket.Rounds[rIdx][mIdx].ScoreA = formatScore(result.IpponsA, result.HansokuA)
					bracket.Rounds[rIdx][mIdx].ScoreB = formatScore(result.IpponsB, result.HansokuB)
					// Echo the persisted scheduling fields back into the result so the
					// caller (and SSE broadcast) sees the full, correct match state
					// rather than the empty Court/ScheduledAt the scoring UI sends.
					if result.Court == "" {
						result.Court = m.Court
					}
					if result.ScheduledAt == "" {
						result.ScheduledAt = m.ScheduledAt
					}
					found = true

					// Only propagate the winner when the match is
					// actually completed. A "running" update is for
					// live-status display only — the next round's
					// SideA/SideB shouldn't be filled until the match
					// has a final result.
					if status == state.MatchStatusCompleted {
						e.propagateBracketWinner(bracket, rIdx, mIdx)
					}
					break
				}
			}
			if found {
				break
			}
		}

		if !found {
			return notFoundErrorf("bracket match %s not found", matchId)
		}
		return nil
	})
}

func (e *Engine) propagateBracketWinner(bracket *state.Bracket, rIdx, mIdx int) {
	if rIdx >= len(bracket.Rounds)-1 {
		return
	}
	m := bracket.Rounds[rIdx][mIdx]
	nextMatchIdx := mIdx / 2
	nextM := &bracket.Rounds[rIdx+1][nextMatchIdx]

	if mIdx%2 == 0 {
		nextM.SideA = m.Winner
	} else {
		nextM.SideB = m.Winner
	}

	// Try to resolve the OTHER side if it's a "Winner of" placeholder
	if strings.HasPrefix(nextM.SideA, "Winner of") {
		// nextM.SideA is "Winner of rX-mY"
		r, m := parseWinnerOf(nextM.SideA, len(bracket.Rounds))
		if r >= 0 && r < len(bracket.Rounds) && m >= 0 && m < len(bracket.Rounds[r]) {
			srcM := bracket.Rounds[r][m]
			if srcM.Status == state.MatchStatusCompleted {
				nextM.SideA = srcM.Winner
			}
		}
	}
	if strings.HasPrefix(nextM.SideB, "Winner of") {
		r, m := parseWinnerOf(nextM.SideB, len(bracket.Rounds))
		if r >= 0 && r < len(bracket.Rounds) && m >= 0 && m < len(bracket.Rounds[r]) {
			srcM := bracket.Rounds[r][m]
			if srcM.Status == state.MatchStatusCompleted {
				nextM.SideB = srcM.Winner
			}
		}
	}

	// Recursive resolution
	if nextM.SideA != "" && nextM.SideB == "" && !strings.HasPrefix(nextM.SideA, "Winner of") {
		nextM.Winner = nextM.SideA
		nextM.Status = state.MatchStatusCompleted
		e.propagateBracketWinner(bracket, rIdx+1, nextMatchIdx)
	} else if nextM.SideA == "" && nextM.SideB != "" && !strings.HasPrefix(nextM.SideB, "Winner of") {
		nextM.Winner = nextM.SideB
		nextM.Status = state.MatchStatusCompleted
		e.propagateBracketWinner(bracket, rIdx+1, nextMatchIdx)
	} else if nextM.SideA == "" && nextM.SideB == "" {
		nextM.Status = state.MatchStatusCompleted
		e.propagateBracketWinner(bracket, rIdx+1, nextMatchIdx)
	}
}

// parseWinnerOf parses "Winner of rX-mY" and returns (rIdx, mIdx)
// Depth in the string is 1-based (root is 1). Rounds in bracket are 0-indexed (Round 1 is index 0).
// Depth d corresponds to Round (maxDepth - d).
func parseWinnerOf(s string, numRounds int) (int, int) {
	var depth, matchIdx int
	_, err := fmt.Sscanf(s, "Winner of r%d-m%d", &depth, &matchIdx)
	if err != nil {
		return -1, -1
	}
	// depth 1 is the final (last round).
	// rounds are 0..numRounds-1.
	// depth d = round index (numRounds - d).
	return numRounds - depth, matchIdx
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

// patchScheduleCourt updates the court for a single match entry in place,
// avoiding a full schedule regeneration on every court change.
func (e *Engine) patchScheduleCourt(compId, matchId, newCourt string) error {
	entries, err := e.store.LoadSchedule(compId)
	if err != nil {
		return err
	}
	for i := range entries {
		// Pool entries: MatchRef == matchId; bracket entries: MatchRef == "R{n}-M{matchId}".
		if entries[i].MatchRef == matchId || strings.HasSuffix(entries[i].MatchRef, "-M"+matchId) {
			entries[i].Court = newCourt
		}
	}
	return e.store.SaveSchedule(compId, entries)
}

func (e *Engine) UpdateMatchCourt(compId string, matchId string, newCourt string) error {
	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		r.Court = newCourt
	})
	if err == nil {
		return e.patchScheduleCourt(compId, matchId, newCourt)
	}
	if !errors.Is(err, errMatchNotFound) {
		return err
	}
	if err = e.withBracketMatch(compId, matchId, func(m *state.BracketMatch) {
		m.Court = newCourt
	}); err != nil {
		return err
	}
	return e.patchScheduleCourt(compId, matchId, newCourt)
}

// OverrideBracketWinner atomically loads the bracket, locates the
// target match, sets the winner + IsOverridden + Status, propagates
// the winner to subsequent rounds, and saves. Same UpdateBracket
// primitive as recordBracketMatchResult and withBracketMatch — the
// entire find + mutate + propagate + save sequence runs under the
// per-competition lock, so a concurrent bracket score / court / time
// update (also under the same lock via the atomic primitives) can't
// land between our load and save and have its mutation clobbered.
//
// Uses the same UpdateBracket atomic primitive as the rest of the
// scoring path to avoid the LoadBracket + mutate + Save TOCTOU window.
func (e *Engine) OverrideBracketWinner(compId string, matchId string, winnerName string) error {
	err := e.store.UpdateBracket(compId, func(bracket *state.Bracket) error {
		if bracket == nil {
			return notFoundErrorf("bracket not found for competition %s", compId)
		}
		for rIdx := range bracket.Rounds {
			for mIdx := range bracket.Rounds[rIdx] {
				m := &bracket.Rounds[rIdx][mIdx]
				if m.ID == matchId {
					m.Winner = winnerName
					m.IsOverridden = true
					m.Status = state.MatchStatusCompleted
					e.propagateBracketWinner(bracket, rIdx, mIdx)
					return nil
				}
			}
		}
		return notFoundErrorf("bracket match %s not found", matchId)
	})
	if err != nil {
		return err
	}

	// Record the override for auditing. A failure here leaves the bracket
	// display correct (it was already saved atomically above); log but
	// don't surface as an error.
	if err := e.store.SaveWinnerOverride(compId, matchId, winnerName); err != nil {
		fmt.Printf("warning: failed to persist winner override audit record for %s: %v\n", matchId, err)
	}

	return nil
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
