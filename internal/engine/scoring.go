package engine

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// applyHansokuIppons auto-awards ippons from accumulated hansoku counts per
// FIK Article 20: every 2 hansoku on one side grants 1 ippon to the opponent.
// Strips any prior 'H' entries and re-appends the correct count so that both
// increases and decreases in hansoku are handled correctly on re-scores.
func applyHansokuIppons(result *state.MatchResult) {
	if result == nil {
		return
	}
	applyOneSide := func(hansoku int, ippons *[]string) {
		expected := hansoku / 2
		if *ippons == nil && expected == 0 {
			return
		}
		filtered := make([]string, 0, len(*ippons))
		for _, v := range *ippons {
			if v != "H" {
				filtered = append(filtered, v)
			}
		}
		for range expected {
			filtered = append(filtered, "H")
		}
		*ippons = filtered
	}
	applyOneSide(result.HansokuA, &result.IpponsB)
	applyOneSide(result.HansokuB, &result.IpponsA)
	for i := range result.SubResults {
		applyOneSide(result.SubResults[i].HansokuA, &result.SubResults[i].IpponsB)
		applyOneSide(result.SubResults[i].HansokuB, &result.SubResults[i].IpponsA)
	}
}

// deriveDaihyosenWinner fills result.Winner from a completed daihyosen
// sub-result (Position == -1) when the caller has not set it explicitly.
// Playoff team matches end in daihyosen when IV and PW are tied; the
// operator scores a single representative bout whose winner becomes the
// team match winner. The sub-result Winner may be the representative
// player's name or the team name; this function maps it back to the
// canonical team name (result.SideA / result.SideB) using the same
// side-matching logic as computeStandings.
func deriveDaihyosenWinner(result *state.MatchResult) {
	if result == nil || result.Winner != "" {
		return
	}
	for _, sub := range result.SubResults {
		if sub.Position != -1 || sub.Winner == "" {
			continue
		}
		sideAWin := sub.Winner == result.SideA || sub.Winner == sub.SideA
		sideBWin := sub.Winner == result.SideB || sub.Winner == sub.SideB
		switch {
		case sideAWin:
			result.Winner = result.SideA
		case sideBWin:
			result.Winner = result.SideB
		}
		return
	}
}

func (e *Engine) RecordMatchResult(compId string, matchId string, result *state.MatchResult) error {
	result.ID = matchId // normalize ID-less payloads before overwriting
	applyHansokuIppons(result)
	return e.writeMatchResult(compId, matchId, result)
}

// writeMatchResult persists the result without applying hansoku auto-award.
// RecordMatchResult calls this after applyHansokuIppons; the K3 rollback
// path calls it directly to restore the prior result byte-for-byte.
func (e *Engine) writeMatchResult(compId string, matchId string, result *state.MatchResult) error {
	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
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
	// Side-effect writes are non-fatal: the match score is already on disk,
	// so propagating would cause a 500 retry that double-records the score.
	if _, err := e.recordIneligibilityFromDecision(compId, matchId, result); err != nil {
		log.Printf("engine: recordIneligibilityFromDecision compId=%s matchId=%s: %v", compId, matchId, err)
	}
	e.maybeLockTeamLineupsForRound(compId, result)
	return nil
}

// RecordMatchResultWithIneligibility is the variant used by the score
// and decision handlers that need to broadcast the
// `competitor-status-updated` SSE event after a kiken/fusenpai is
// recorded. It returns the new CompetitorStatus (or nil when none was
// written) alongside any error.
//
// The match-score persistence semantics are identical to
// RecordMatchResult; only the side-effect status is surfaced for the
// caller's broadcast. Side-effect write failures are still non-fatal —
// the function returns (nil, nil) and logs.
//
// T085/T092.
func (e *Engine) RecordMatchResultWithIneligibility(compId string, matchId string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
	result.ID = matchId
	applyHansokuIppons(result)
	deriveDaihyosenWinner(result)

	// T105/CHK047: capture the prior result so we can rollback if the atomic
	// ineligibility write below fails with AlreadyIneligibleError.
	prior, _ := e.lookupExistingResult(compId, matchId)

	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
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
			return nil, err
		}
		if err := e.recordBracketMatchResult(compId, matchId, result); err != nil {
			return nil, err
		}
	}
	status, err := e.recordIneligibilityFromDecision(compId, matchId, result)
	if err != nil {
		// K2/CHK047: when the atomic check-and-set inside
		// recordIneligibilityFromDecision detects a concurrent kiken
		// (different operator already wrote ineligibility for this
		// player from another match), propagate the error so the handler
		// can return HTTP 409.
		var alreadyErr *AlreadyIneligibleError
		if errors.As(err, &alreadyErr) {
			// K3/CHK047: rollback the partial write. The match score was
			// already persisted, but the intended loser is already
			// ineligible from a different match. Revert the match score
			// to its prior state before returning 409 so the operator
			// sees a clean rejection rather than a mutated match.
			if prior != nil {
				_ = e.writeMatchResult(compId, matchId, prior)
			}
			return nil, err
		}
		log.Printf("engine: recordIneligibilityFromDecision compId=%s matchId=%s: %v", compId, matchId, err)
		return nil, nil
	}
	// T128 — same lineup-lock side effect as RecordMatchResult above.
	e.maybeLockTeamLineupsForRound(compId, result)
	return status, nil
}

// maybeLockTeamLineupsForRound freezes any persisted team lineups for
// the round this match belongs to, but only when this update marks
// the match as live (running or completed). Side effect only — any
// store error is logged and swallowed so the score-recording isn't
// retried (which would double-record the score; same rationale as
// recordIneligibilityFromDecision above).
//
// TODO(T128): round mapping. The match's "round" is currently always
// treated as 0 because:
//   - pool matches have no round field (every pool match is round 0
//     by convention); and
//   - bracket-round inference requires loading the bracket and
//     scanning Rounds[] for matchId, which is overhead we don't
//     pay until multi-round lineups are actually in use.
//
// Once team-pool-rotation or per-round elimination lineups land, this
// helper grows the bracket-scan lookup. The store-side
// roundHasLiveOrCompletedMatchLocked in state/team_lineup.go already
// handles per-round bracket inspection — the gap is just the
// matchId→round mapping here.
//
// FR-040.
func (e *Engine) maybeLockTeamLineupsForRound(compId string, result *state.MatchResult) {
	if result == nil {
		return
	}
	// Only act on the running/completed transition — a "scheduled"
	// update (e.g. time-only adjust) must NOT freeze lineups.
	if result.Status != state.MatchStatusRunning && result.Status != state.MatchStatusCompleted {
		return
	}
	// Cheap guard: skip the file write entirely for non-team
	// competitions. A non-team comp can't have lineups, so calling
	// LockTeamLineupsForRound would always be a no-op file read.
	comp, err := e.store.LoadCompetition(compId)
	if err != nil || comp == nil || comp.TeamSize <= 0 {
		return
	}
	const round = 0
	if err := e.store.LockTeamLineupsForRound(compId, round, time.Now().UTC()); err != nil {
		log.Printf("engine: LockTeamLineupsForRound compId=%s round=%d: %v", compId, round, err)
	}
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
			// helper.Player is a type alias for domain.Player (NFR-007);
			// the pool player flows directly into PlayerStanding.
			playerStandings[player.Name] = &state.PlayerStanding{
				Player: player,
			}
		}

		for _, m := range matches {
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			// Tiebreaker and pool-daihyosen matches don't count toward regular pool stats.
			if IsTiebreakerMatchID(m.ID) || IsPoolDaihyosenMatchID(m.ID) {
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

		// Apply tiebreaker results as a secondary sort within tied groups.
		// Win counts are scoped per group: only TB matches between the players
		// in the same tied group influence that group's ordering, preventing
		// wins from an unrelated tied group from bleeding into another.
		for i := 0; i < len(sorted); {
			j := i + 1
			for j < len(sorted) && sorted[j].Points == sorted[i].Points {
				j++
			}
			if j-i >= 2 {
				groupNames := make(map[string]bool, j-i)
				for k := i; k < j; k++ {
					groupNames[sorted[k].Player.Name] = true
				}
				groupTBWins := map[string]int{}
				for _, m := range matches {
					if !IsTiebreakerMatchID(m.ID) || m.Status != state.MatchStatusCompleted || m.Winner == "" {
						continue
					}
					if groupNames[m.SideA] && groupNames[m.SideB] {
						groupTBWins[m.Winner]++
					}
				}
				if len(groupTBWins) > 0 {
					sort.SliceStable(sorted[i:j], func(a, b int) bool {
						return groupTBWins[sorted[i+a].Player.Name] > groupTBWins[sorted[i+b].Player.Name]
					})
				}
			}
			i = j
		}

		// Apply pool-daihyosen results as a secondary sort within tied groups
		// for team competitions. Mirrors the TB block above: DH wins are scoped
		// per tied group to prevent cross-group bleed.
		if isTeam {
			for i := 0; i < len(sorted); {
				j := i + 1
				for j < len(sorted) && sorted[j].Points == sorted[i].Points {
					j++
				}
				if j-i >= 2 {
					groupNames := make(map[string]bool, j-i)
					for k := i; k < j; k++ {
						groupNames[sorted[k].Player.Name] = true
					}
					groupDHWins := map[string]int{}
					for _, m := range matches {
						if !IsPoolDaihyosenMatchID(m.ID) || m.Status != state.MatchStatusCompleted || m.Winner == "" {
							continue
						}
						if groupNames[m.SideA] && groupNames[m.SideB] {
							groupDHWins[m.Winner]++
						}
					}
					if len(groupDHWins) > 0 {
						sort.SliceStable(sorted[i:j], func(a, b int) bool {
							return groupDHWins[sorted[i+a].Player.Name] > groupDHWins[sorted[i+b].Player.Name]
						})
					}
				}
				i = j
			}
		}

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
					bracket.Rounds[rIdx][mIdx].Decision = result.Decision
					bracket.Rounds[rIdx][mIdx].DecisionBy = result.DecisionBy
					bracket.Rounds[rIdx][mIdx].DecisionReason = result.DecisionReason
					bracket.Rounds[rIdx][mIdx].Encho = result.Encho
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
