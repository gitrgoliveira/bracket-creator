package engine

import (
	"fmt"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// IsTiebreakerMatchID reports whether matchID identifies a supplementary
// ippon-shobu tiebreaker match (IDs of the form "Pool X-TB-N").
func IsTiebreakerMatchID(matchID string) bool {
	return strings.Contains(matchID, "-TB-")
}

// detectPoolTies walks a sorted (descending Points) standings slice and
// returns all groups of 2+ competitors that share the same Points value.
// The caller must pass standings already sorted by Points descending.
func detectPoolTies(standings []state.PlayerStanding) [][]state.PlayerStanding {
	var groups [][]state.PlayerStanding
	i := 0
	for i < len(standings) {
		j := i + 1
		for j < len(standings) && standings[j].Points == standings[i].Points {
			j++
		}
		if j-i >= 2 {
			g := make([]state.PlayerStanding, j-i)
			copy(g, standings[i:j])
			groups = append(groups, g)
		}
		i = j
	}
	return groups
}

// tiebreakerPairKey returns a canonical (order-independent) key for a
// pair of player names — used to detect already-existing TB matches.
func tiebreakerPairKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}

// generateTiebreakerMatches creates the round-robin MatchResult entries
// for tiedGroup. existingTBCount is the current number of TB matches in
// the pool (used to produce unique TB-N indices). court is the court
// label assigned to the pool. Pairs already in existingPairs are skipped.
func generateTiebreakerMatches(poolName string, tiedGroup []state.PlayerStanding, existingTBCount int, court string, existingPairs map[string]bool) []state.MatchResult {
	var results []state.MatchResult
	idx := existingTBCount
	for _, a := range tiedGroup {
		for _, b := range tiedGroup {
			if a.Player.Name >= b.Player.Name {
				continue // only generate each pair once (a < b alphabetically)
			}
			key := tiebreakerPairKey(a.Player.Name, b.Player.Name)
			if existingPairs[key] {
				continue
			}
			results = append(results, state.MatchResult{
				ID:     fmt.Sprintf("%s-TB-%d", poolName, idx),
				SideA:  a.Player.Name,
				SideB:  b.Player.Name,
				Status: state.MatchStatusScheduled,
				Court:  court,
			})
			existingPairs[key] = true
			idx++
		}
	}
	return results
}

// InjectTiebreakerMatches inspects all pool standings for compID after
// regular pool matches are complete. For every tied group (same Points
// after the full cascade), it generates a round-robin of ippon-shobu
// tiebreaker matches, appends them to the pool-matches CSV, and
// regenerates the schedule. Returns the newly injected matches (nil
// when there are no ties or all TB pairs already exist).
func (e *Engine) InjectTiebreakerMatches(compID string) ([]state.MatchResult, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}

	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}

	allMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	// Scan existing TB matches per pool for idempotency and ID sequencing.
	type poolTBInfo struct {
		existingPairs map[string]bool
		count         int
	}
	poolTB := map[string]*poolTBInfo{}
	poolCourt := map[string]string{}
	for _, m := range allMatches {
		pn, ok := poolNameFromMatchID(m.ID)
		if !ok {
			continue
		}
		if _, inStandings := standings[pn]; !inStandings {
			continue
		}
		if _, ok := poolCourt[pn]; !ok {
			poolCourt[pn] = m.Court
		}
		if IsTiebreakerMatchID(m.ID) {
			if poolTB[pn] == nil {
				poolTB[pn] = &poolTBInfo{existingPairs: map[string]bool{}}
			}
			poolTB[pn].count++
			poolTB[pn].existingPairs[tiebreakerPairKey(m.SideA, m.SideB)] = true
		}
	}

	var injected []state.MatchResult
	for poolName, poolStandings := range standings {
		info := poolTB[poolName]
		existingCount := 0
		existingPairs := map[string]bool{}
		if info != nil {
			existingCount = info.count
			existingPairs = info.existingPairs
		}

		for _, group := range detectPoolTies(poolStandings) {
			newMatches := generateTiebreakerMatches(poolName, group, existingCount, poolCourt[poolName], existingPairs)
			existingCount += len(newMatches)
			injected = append(injected, newMatches...)
		}
	}

	if len(injected) == 0 {
		return nil, nil
	}

	allMatches = append(allMatches, injected...)

	// Reassign slots so the new TB matches get ScheduledAt values.
	// Snapshot operator-adjusted times first so they survive the reassignment;
	// only newly injected matches (ScheduledAt == "") should receive new slots.
	existingTimes := make(map[string]string, len(allMatches))
	for _, m := range allMatches {
		if m.ScheduledAt != "" {
			existingTimes[m.ID] = m.ScheduledAt
		}
	}
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}
	state.ApplyTournamentDefaults(tournament)
	state.ApplyCompetitionDefaults(comp)
	allMatches = assignPoolMatchSlots(allMatches, comp, tournament)
	for i := range allMatches {
		if t, ok := existingTimes[allMatches[i].ID]; ok {
			allMatches[i].ScheduledAt = t
		}
	}

	if err := e.store.SavePoolMatches(compID, allMatches); err != nil {
		return nil, err
	}

	// Invalidate the standings cache so the next read reflects the injected matches.
	e.standingsCache.Delete(compID)
	e.standingsFlight.Delete(compID)

	return injected, e.GenerateSchedule(compID)
}
