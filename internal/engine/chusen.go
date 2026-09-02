package engine

import (
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ChusenGroup is a consequential team-pool tie that the daihyosen could not
// separate: two or more members share the same daihyosen win count (a true
// win/loss cycle, an all-drawn round, or any other tie in win counts), so the
// finishing order is still undetermined. Per the rules (running_a_kendo_
// tournament.md:181, EKC 6.2.5.1) the last resort is chusen (drawing lots): the
// operator draws lots and records the order, which persists as a per-pool rank
// override and lets the competition advance.
//
// This is an INTERNAL engine type, not a wire DTO: the GET /chusen-candidates
// handler builds its JSON response from a gin.H literal (a "teamNames" array),
// so these fields are never marshaled directly and carry no json tags.
type ChusenGroup struct {
	PoolName string
	// Teams are the still-tied members in current standings order.
	Teams []state.PlayerStanding
	// MinPosition is the 1-based finishing position of the best-placed member.
	MinPosition int
}

// groupNeedsChusen reports whether a tied group remains unresolved after its
// daihyosen bouts and therefore needs a chusen (drawing lots). It is the single
// per-group predicate shared by dhCycleExists (which blocks auto-completion) and
// ChusenCandidates (which surfaces the groups to the operator). groupOverrides
// is poolRanks[poolName] for the group's pool (nil when none).
//
// Returns false when: the operator has already ranked every member (chusen
// recorded), no daihyosen bout among the group has been played yet, or the
// played bouts produced a strictly-ordered win count. Returns true whenever two
// or more members finish the completed round on the same daihyosen win count
// (a true win/loss cycle, an all-drawn round, or any other partial tie) so the
// order is undetermined.
//
// groupOverrides is resolved per member via lookupPoolRankOverride (bc-cse):
// identity-keyed first (id-preferred, name+dojo fallback), then a legacy
// bare-name key for pre-fix overrides.json data. Two same-name,
// different-dojo teammates in one tied group therefore no longer share a
// single "already recorded" verdict -- each is checked against its own
// override entry.
func groupNeedsChusen(group []state.PlayerStanding, allMatches []state.MatchResult, groupOverrides map[string]int) bool {
	if len(groupOverrides) > 0 {
		allOverridden := true
		for _, s := range group {
			if _, ok := lookupPoolRankOverride(groupOverrides, s.Player.ID, s.Player.Name, s.Player.Dojo); !ok {
				allOverridden = false
				break
			}
		}
		if allOverridden {
			return false
		}
	}
	// Membership and win counts key on competitor IDENTITY rather than the
	// display name, matching applyTiebreakSort next door. Note what this is
	// and is not: chusen is team-only (ChusenCandidates returns nil for an
	// individual competition) and two TEAMS may not share a name even across
	// dojos (checkNewTeamNameCollisions, state/participants.go), so unlike the
	// individual tiebreak path this is NOT a routinely reachable collision.
	// It is kept because the team-name rule has one documented hole -- an
	// unreadable config.md disables it for that write, logged and allowed
	// through -- and because a bare-name key would then credit one namesake's
	// daihyosen win to the other, reading a decided group as still tied or a
	// genuine tie as decided; either answer changes who advances.
	// generatePoolDaihyosenMatches stamps SideAID/SideBID/WinnerID, so the
	// ids are there to key on and the hardening costs nothing.
	resolve := newGroupKeyResolver(group)

	dhWins := make(map[string]int, len(group))
	dhCompleted := 0
	for _, m := range allMatches {
		if !IsPoolDaihyosenMatchID(m.ID) || m.Status != state.MatchStatusCompleted {
			continue
		}
		keyA, okA := resolve(m.SideAID, m.SideA)
		keyB, okB := resolve(m.SideBID, m.SideB)
		if okA && okB && keyA != keyB {
			dhCompleted++
			// A hikiwake (Winner == "") counts toward round completeness but adds
			// no win, so an all-drawn round leaves every member on 0 wins - a
			// duplicate, which correctly surfaces as needing chusen below.
			if m.Winner != "" {
				if wk, ok := resolve(m.WinnerID, m.Winner); ok {
					dhWins[wk]++
				}
			}
		}
	}
	// Only judge the group once its FULL pairwise daihyosen round is complete
	// (a round-robin = N*(N-1)/2 bouts). Mid-round, partial win counts look like
	// a duplicate (e.g. after the first of three bouts the counts are 1/0/0,
	// whose two zeros are a spurious tie), which would surface the chusen panel
	// before the remaining bouts are played. Once complete, any duplicate win
	// count - a true win/loss cycle, an all-drawn round (every member on 0
	// wins), or any other partial tie - leaves the order undetermined; a
	// strict win order (all distinct counts) does not.
	n := len(group)
	expected := n * (n - 1) / 2
	if expected == 0 || dhCompleted < expected {
		return false
	}
	seen := make(map[int]bool, len(group))
	for _, s := range group {
		count := dhWins[standingsPlayerKey(s.Player.ID, s.Player.Name)]
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
	overridesObj, err := e.store.LoadOverrides(compID)
	if err != nil {
		return nil, err
	}
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
