package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TiedGroup describes a group of teams that are tied on all official ranking
// criteria at the end of league play. MinPosition and MaxPosition are the
// 1-based finishing positions that the tied teams collectively occupy (e.g.
// a two-way tie at 2nd place gives MinPosition=2, MaxPosition=3).
type TiedGroup struct {
	// Teams holds the standings entries of all tied members, in the order
	// produced by detectPoolTies (which preserves the sorted standings order).
	Teams []state.PlayerStanding

	// MinPosition is the 1-based rank of the best-placed team in the group.
	// For a group starting at index i (0-based) in the sorted standings,
	// MinPosition == i+1.
	MinPosition int

	// MaxPosition is the 1-based rank of the worst-placed team in the group.
	// For a group of n teams starting at index i, MaxPosition == i+n.
	MaxPosition int
}

// effectiveTopN returns the resolved LeaguePlayoffTopN for the competition.
// When the field is zero (unset), it defaults to 3 (the standard kendo top-N).
func effectiveTopN(comp *state.Competition) int {
	if comp.LeaguePlayoffTopN > 0 {
		return comp.LeaguePlayoffTopN
	}
	return 3
}

// isConsequentialTie reports whether a TiedGroup requires an operator-initiated
// play-off given the competition's playoff band and two-third-places convention.
//
// A group is consequential when ALL of the following hold:
//  1. The group's MinPosition is within the playoff band [1..topN], meaning at
//     least one of the tied positions determines a top-N place.
//  2. The group is NOT fully covered by the two-joint-3rd-places exemption:
//     when LeagueTwoThirdPlaces is true and ALL positions in the group are at
//     position >= 3, there is no need to distinguish 3rd from 4th — both teams
//     share 3rd. The group is therefore non-consequential.
//
// Rule precision for the two-thirds exemption:
//   - The exemption fires only when EVERY position in the group is >= 3
//     (i.e. MinPosition >= 3). If MinPosition < 3 the group straddles 2nd/3rd
//     or higher, and a play-off IS needed to decide who finishes 2nd.
//   - The exemption is applied regardless of topN — even if topN==4, a
//     group sitting entirely at positions [3,4] is just "two joint 3rds" and
//     no play-off is required when LeagueTwoThirdPlaces is true.
func isConsequentialTie(g TiedGroup, comp *state.Competition) bool {
	topN := effectiveTopN(comp)

	// Group must intersect [1..topN]: the best position in the group must be
	// within the playoff band.
	if g.MinPosition > topN {
		return false
	}

	// Two-joint-3rd-places exemption: when enabled, a group that sits entirely
	// at positions >= 3 (i.e. only 3rd-place-or-below slots) does not need a
	// decider — all members share 3rd place. The minimum position of the group
	// must be at least 3 for this exemption to apply.
	if comp.LeagueTwoThirdPlaces && g.MinPosition >= 3 {
		return false
	}

	return true
}

// LeaguePlayoffCandidates returns the consequential tied groups in a team-league
// competition after all regular pool matches are complete. A group is
// "consequential" when it intersects the playoff band [1..LeaguePlayoffTopN] and
// is not covered by the LeagueTwoThirdPlaces exemption (see isConsequentialTie).
//
// Returns an empty slice (not an error) when:
//   - The competition is not a team league (Format != "league" or TeamSize == 0).
//   - There are no ties in the standings.
//   - All ties fall outside the consequential band.
//
// This function is the single source of truth for "are there ties that need an
// operator decision?" and is used by both MaybeAutoCompletePools (to block
// premature completion) and Phase 3b's operator endpoints (to list which teams
// need a play-off).
//
// The league standings come from a single implicit pool ("Pool A" by convention
// for single-pool leagues). All tied groups across all pools are evaluated;
// in practice a league has exactly one pool.
func (e *Engine) LeaguePlayoffCandidates(compID string) ([]TiedGroup, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}

	// Only applicable to team leagues.
	if comp.Format != state.CompFormatLeague || comp.TeamSize == 0 {
		return nil, nil
	}

	// When the operator has accepted shared ranks without a play-off
	// (Phase 3b finalize endpoint), treat the competition as having no
	// consequential ties so MaybeAutoCompletePools can transition normally.
	if comp.LeaguePlayoffFinalized {
		return nil, nil
	}

	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}

	var candidates []TiedGroup
	for _, poolStandings := range standings {
		for _, positions := range detectPoolTies(poolStandings) {
			if len(positions) == 0 {
				continue
			}
			// positions is 0-based into poolStandings (sorted descending by Points).
			// Convert to 1-based ranks.
			minPos := positions[0] + 1
			maxPos := positions[len(positions)-1] + 1

			g := TiedGroup{
				Teams:       standingsAt(poolStandings, positions),
				MinPosition: minPos,
				MaxPosition: maxPos,
			}
			if isConsequentialTie(g, comp) {
				candidates = append(candidates, g)
			}
		}
	}
	return candidates, nil
}

// GenerateLeaguePlayoffMatches generates the round-robin daihyosen (play-off)
// matches for a specific set of tied teams in a team-league competition. This is
// the operator-triggered path (Phase 3b will call this via an HTTP endpoint).
//
// The matches use the "Pool X-DH-N" ID format so they are recognized by the
// existing IsPoolDaihyosenMatchID predicate and routed to the DH score editor.
// Idempotent: pairs that already exist in the store are skipped.
//
// This function is exported here so Phase 3b can call it directly after the
// operator selects which teams to include. For league competitions it operates
// on the single league pool.
//
// TODO(Phase-3b): add an HTTP handler (e.g. POST /api/competitions/:id/league-playoff)
// that accepts the operator's selected team names, validates them against
// LeaguePlayoffCandidates, calls GenerateLeaguePlayoffMatches, and broadcasts
// EventMatchUpdated + EventScheduleUpdated. Also add a "finalize shared ranks"
// action (POST /api/competitions/:id/league-playoff/finalize) that accepts the
// current standings as final, skips the play-off, and transitions the competition
// to CompStatusComplete. The finalize path should call MaybeAutoCompletePools after
// accepting shared ranks to trigger the normal completion flow.
func (e *Engine) GenerateLeaguePlayoffMatches(compID string, tiedTeamNames []string) ([]state.MatchResult, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}
	if comp.Format != state.CompFormatLeague || comp.TeamSize == 0 {
		return nil, validationErrorf("GenerateLeaguePlayoffMatches is only valid for team-league competitions")
	}

	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}

	// Resolve the standings for the requested team names. Validate that all
	// provided names exist in standings.
	nameSet := make(map[string]bool, len(tiedTeamNames))
	for _, n := range tiedTeamNames {
		nameSet[n] = true
	}

	// Locate the pool and build the group.
	var poolName string
	var tiedGroup []state.PlayerStanding
	for pn, ps := range standings {
		poolName = pn
		for _, s := range ps {
			if nameSet[s.Player.Name] {
				tiedGroup = append(tiedGroup, s)
			}
		}
	}
	if len(tiedGroup) == 0 {
		return nil, validationErrorf("none of the requested teams found in standings for competition %s", compID)
	}

	// Determine the court from existing matches.
	allMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	court := ""
	existingCount := 0
	existingPairs := map[string]bool{}
	for _, m := range allMatches {
		pn, ok := poolNameFromMatchID(m.ID)
		if !ok {
			continue
		}
		if pn == poolName && court == "" {
			court = m.Court
		}
		if IsPoolDaihyosenMatchID(m.ID) && pn == poolName {
			existingCount++
			existingPairs[tiebreakerPairKey(m.SideA, m.SideB)] = true
		}
	}

	injected := generatePoolDaihyosenMatches(poolName, tiedGroup, existingCount, court, existingPairs)
	if len(injected) == 0 {
		return nil, nil
	}

	allMatches = append(allMatches, injected...)

	// Reassign schedule slots for the new DH matches.
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
	allMatches, _ = assignPoolMatchSlots(allMatches, comp, tournament)
	for i := range allMatches {
		if t, ok := existingTimes[allMatches[i].ID]; ok {
			allMatches[i].ScheduledAt = t
		}
	}

	if err := e.store.SavePoolMatches(compID, allMatches); err != nil {
		return nil, err
	}

	e.standingsCache.Delete(compID)
	e.standingsFlight.Delete(compID)

	return injected, e.GenerateSchedule(compID)
}
