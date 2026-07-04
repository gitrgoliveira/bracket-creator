package engine

import (
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ChusenGroup is a consequential team-pool tie that the daihyosen could not
// separate (equal representative-bout wins: a cycle or all-drawn), so the
// finishing order is still undetermined. Per the rules (running_a_kendo_
// tournament.md:181, EKC 6.2.5.1) the last resort is chusen (drawing lots): the
// operator draws lots and records the order, which persists as a per-pool rank
// override and lets the competition advance.
type ChusenGroup struct {
	PoolName string `json:"poolName"`
	// Teams are the still-tied members in current standings order.
	Teams []state.PlayerStanding `json:"teams"`
	// MinPosition is the 1-based finishing position of the best-placed member.
	MinPosition int `json:"minPosition"`
}

// groupNeedsChusen reports whether a tied group remains unresolved after its
// daihyosen bouts and therefore needs a chusen (drawing lots). It is the single
// per-group predicate shared by dhCycleExists (which blocks auto-completion) and
// ChusenCandidates (which surfaces the groups to the operator). groupOverrides
// is poolRanks[poolName] for the group's pool (nil when none).
//
// Returns false when: the operator has already ranked every member (chusen
// recorded), no daihyosen bout among the group has been played yet, or the
// played bouts produced a strictly-ordered win count. Returns true only when two
// members share a daihyosen win count (a cycle / all-drawn) so the order is
// undetermined.
func groupNeedsChusen(group []state.PlayerStanding, allMatches []state.MatchResult, groupOverrides map[string]int) bool {
	if len(groupOverrides) > 0 {
		allOverridden := true
		for _, s := range group {
			if _, ok := groupOverrides[s.Player.Name]; !ok {
				allOverridden = false
				break
			}
		}
		if allOverridden {
			return false
		}
	}
	names := make(map[string]bool, len(group))
	for _, s := range group {
		names[s.Player.Name] = true
	}
	dhWins := make(map[string]int, len(group))
	dhPlayed := false
	for _, m := range allMatches {
		if !IsPoolDaihyosenMatchID(m.ID) || m.Status != state.MatchStatusCompleted || m.Winner == "" {
			continue
		}
		if names[m.SideA] && names[m.SideB] {
			dhWins[m.Winner]++
			dhPlayed = true
		}
	}
	if !dhPlayed {
		return false
	}
	seen := make(map[int]bool, len(group))
	for _, s := range group {
		count := dhWins[s.Player.Name]
		if seen[count] {
			return true
		}
		seen[count] = true
	}
	return false
}

// ChusenCandidates returns the consequential team-pool ties that the daihyosen
// left undetermined and that therefore need a chusen (drawing lots). It is the
// single source of truth for "which groups still need an operator lots-draw",
// used by the GET /chusen-candidates endpoint. Empty (not an error) when the
// competition is not a team comp in the pools stage, or no such group exists.
//
// Pools are returned in name order for stable output.
func (e *Engine) ChusenCandidates(compID string) ([]ChusenGroup, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}
	isTeam := comp.Kind == "team" || comp.TeamSize > 0
	if !isTeam || comp.Status != state.CompStatusPools {
		return nil, nil
	}

	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	overridesObj, _ := e.store.LoadOverrides(compID)
	var poolRanks map[string]map[string]int
	if overridesObj != nil {
		poolRanks = overridesObj.PoolRanks
	}
	poolWinners := comp.EffectivePoolWinners()

	poolNames := make([]string, 0, len(standings))
	for name := range standings {
		poolNames = append(poolNames, name)
	}
	sort.Strings(poolNames)

	var out []ChusenGroup
	for _, poolName := range poolNames {
		poolStandings := standings[poolName]
		for _, positions := range detectPoolTies(poolStandings) {
			// Only a tie that affects advancement/seed warrants a decider at all.
			if !tieAffectsAdvancement(positions, poolWinners) {
				continue
			}
			group := standingsAt(poolStandings, positions)
			if groupNeedsChusen(group, matches, poolRanks[poolName]) {
				out = append(out, ChusenGroup{
					PoolName:    poolName,
					Teams:       group,
					MinPosition: positions[0] + 1,
				})
			}
		}
	}
	return out, nil
}
