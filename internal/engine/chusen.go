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
	// Teams are the tied members in current standings order (which, once a
	// chusen is recorded, is the order the override ranks sort them into).
	Teams []state.PlayerStanding
	// MinPosition is the 1-based finishing position of the best-placed member.
	MinPosition int
	// Ranks is each team's recorded chusen rank, parallel to Teams, read from
	// the override itself rather than from the standings row. Set only on a
	// group already settled by chusen (ChusenReport.Recorded); nil on one
	// still waiting for it.
	Ranks []int
}

// ChusenReport is every consequential team-pool tie the daihyosen left
// undetermined, split by whether the operator has recorded its chusen yet.
type ChusenReport struct {
	// Pending groups still need a chusen (drawing lots).
	Pending []ChusenGroup
	// Recorded groups were settled by a chusen already recorded
	// (groupSettledByChusen). The operator may change the recorded order, in
	// case it was entered wrongly.
	Recorded []ChusenGroup
}

// groupNeedsChusen reports whether a tied group remains unresolved after its
// daihyosen bouts and therefore needs a chusen (drawing lots). It is the single
// per-group predicate shared by dhCycleExists (which blocks auto-completion) and
// ChusenStatus (which surfaces the groups to the operator). groupOverrides is
// poolRanks[poolName] for the group's pool (nil when none).
//
// Returns false when the operator has already ranked every member (chusen
// recorded, chusenRecorded) or when daihyosenLeftTied says the bouts settled
// the order or have not all been fought yet.
func groupNeedsChusen(group []state.PlayerStanding, allMatches []state.MatchResult, groupOverrides map[string]int) bool {
	return !chusenRecorded(group, groupOverrides) && daihyosenLeftTied(group, allMatches)
}

// groupSettledByChusen reports whether a tied group WAS decided by chusen: its
// daihyosen left the order undetermined, exactly as groupNeedsChusen requires,
// and the operator has since recorded a rank for every member. It is the one
// owner of "which groups were decided by chusen", which are the only groups
// whose recorded order the operator is offered to change: a chusen entered in
// the wrong order must stay fixable, but no other rank is set by hand.
func groupSettledByChusen(group []state.PlayerStanding, allMatches []state.MatchResult, groupOverrides map[string]int) bool {
	return chusenRecorded(group, groupOverrides) && daihyosenLeftTied(group, allMatches)
}

// daihyosenRuledByChusen reports a drawn pool representative ("-DH-") bout
// whose tied group a chusen has ranked: the operator drew lots over the group
// the bout could not separate, so the draw needs no rematch and the pool may
// complete (poolMatchSettled). The group is the tied group the standings hold
// the bout's two sides in (lotsGroup: two separate ties in a pool stay apart),
// and the lots rule the bout only once every one of those teams is ranked,
// like chusenRecorded: a chusen still being entered, or one over a smaller
// group, settles nothing. poolStandings is the competition's pool standings
// (read only when a drawn representative bout exists, lotsStandings); a pool
// they do not hold rules nothing, so the bout stays unsettled rather than
// guessed at. poolRanks is state.Overrides.PoolRanks (nil when none); teams
// are read by participant id only. Any full ranking of the group, written
// through the chusen door (OverridePoolRanks), settles it: that door is the
// only way a pool rank is recorded, and it does not ask whether the group was
// tied.
func daihyosenRuledByChusen(m *state.MatchResult, poolStandings map[string][]state.PlayerStanding, poolRanks map[string]map[string]int) bool {
	if !IsPoolDaihyosenMatchID(m.ID) || !drawnSupplementaryBout(m) {
		return false
	}
	poolName, ok := poolNameFromMatchID(m.ID)
	if !ok {
		return false
	}
	standings := poolStandings[poolName]
	if len(standings) == 0 {
		return false
	}
	for id := range lotsGroup(standings, m.SideAID, m.SideBID) {
		if _, ranked := lookupPoolRankOverride(poolRanks[poolName], id); !ranked {
			return false
		}
	}
	return true
}

// lotsGroup is the set of teams a drawn representative bout's lots had to
// rank: the standings group level on Points with both of its sides, the same
// group ChusenStatus offers the operator (detectPoolTies), or just the two
// sides when the standings no longer put them level. The pool's other
// representative bouts are not asked: a result correction can shrink a tie
// and leave the rows of its old, larger round behind, and those would pull a
// team back in that the chusen panel no longer offers.
func lotsGroup(standings []state.PlayerStanding, sideA, sideB string) map[string]bool {
	group := map[string]bool{sideA: true, sideB: true}
	var pointsA, pointsB int
	var foundA, foundB bool
	for _, s := range standings {
		switch s.Player.ID {
		case sideA:
			pointsA, foundA = s.Points, true
		case sideB:
			pointsB, foundB = s.Points, true
		}
	}
	if !foundA || !foundB || pointsA != pointsB {
		return group
	}
	for _, s := range standings {
		if s.Points == pointsA {
			group[s.Player.ID] = true
		}
	}
	return group
}

// poolLots is what a drawn representative bout is judged settled on
// (poolMatchSettled): the recorded pool ranks and the pool standings the tied
// group is read from. loadPoolLots is the one door that fills it; the zero
// value holds neither, so a drawn representative bout is never settled by it
// (isBlockingEngiTBRow, which only judges tie-break rows, passes it on purpose).
type poolLots struct {
	ranks     map[string]map[string]int
	standings map[string][]state.PlayerStanding
}

// loadPoolLots reads a competition's lots for matches: the recorded ranks
// (LoadPoolRanksJudging) and the standings (lotsStandings), each only when the
// matches hold the bout that needs it. loadRanks is a store's or a
// transaction's LoadPoolRanks.
func loadPoolLots(compID string, matches []state.MatchResult, loadRanks func(string) (map[string]map[string]int, error), loadStandings func() (map[string][]state.PlayerStanding, error)) (poolLots, error) {
	ranks, err := LoadPoolRanksJudging(compID, matches, loadRanks)
	if err != nil {
		return poolLots{}, err
	}
	standings, err := lotsStandings(matches, loadStandings)
	if err != nil {
		return poolLots{}, err
	}
	return poolLots{ranks: ranks, standings: standings}, nil
}

// lotsStandings reads the pool standings a drawn representative bout's lots
// are judged against (daihyosenRuledByChusen), through load, and only when the
// matches hold such a bout: with none, nothing consults them, so it returns
// nil without computing any. Callers hand the result to poolMatchSettled.
func lotsStandings(matches []state.MatchResult, load func() (map[string][]state.PlayerStanding, error)) (map[string][]state.PlayerStanding, error) {
	for i := range matches {
		if IsPoolDaihyosenMatchID(matches[i].ID) && drawnSupplementaryBout(&matches[i]) {
			return load()
		}
	}
	return nil, nil
}

// chusenRecorded reports whether every member of a tied group carries a
// pool-rank override, i.e. the operator has recorded the drawn order.
//
// groupOverrides is resolved per member via lookupPoolRankOverride, keyed by
// participant id ONLY (bc-cse, bc-pnum). Two same-name, different-dojo
// teammates in one tied group therefore never share a single "already
// recorded" verdict -- each is checked against its own override entry.
func chusenRecorded(group []state.PlayerStanding, groupOverrides map[string]int) bool {
	if len(groupOverrides) == 0 {
		return false
	}
	for _, s := range group {
		if _, ok := lookupPoolRankOverride(groupOverrides, s.Player.ID); !ok {
			return false
		}
	}
	return true
}

// daihyosenLeftTied reports whether a tied group's daihyosen bouts left its
// order undetermined, whatever the operator has recorded since. Returns false
// when no daihyosen bout among the group has been played yet, the round is not
// complete, or the played bouts produced a strictly-ordered win count. Returns
// true whenever two or more members finish the completed round on the same
// daihyosen win count (a true win/loss cycle, an all-drawn round, or any other
// partial tie).
func daihyosenLeftTied(group []state.PlayerStanding, allMatches []state.MatchResult) bool {
	// Membership and win counts key on competitor identity by participant id
	// ONLY (operator ruling bc-pnum), matching applyTiebreakSort next door.
	// Note what this is and is not: chusen is team-only (ChusenStatus
	// reports nothing for an individual competition) and two TEAMS may not share
	// a name even across dojos (checkNewTeamNameCollisions,
	// state/participants.go), so unlike the individual tiebreak path this is
	// NOT a routinely reachable collision. It is kept because the team-name
	// rule has one documented hole -- an unreadable config.md disables it for
	// that write, logged and allowed through -- and because a bare-name key
	// would then credit one namesake's daihyosen win to the other, reading a
	// decided group as still tied or a genuine tie as decided; either answer
	// changes who advances. generatePoolDaihyosenMatches stamps
	// SideAID/SideBID/WinnerID, so the ids are there to key on and there is
	// no fallback if they were ever missing.
	ids := groupMemberIDs(group)

	dhWins := make(map[string]int, len(group))
	dhCompleted := 0
	for _, m := range allMatches {
		if !IsPoolDaihyosenMatchID(m.ID) || m.Status != state.MatchStatusCompleted {
			continue
		}
		if !ids[m.SideAID] || !ids[m.SideBID] || m.SideAID == m.SideBID {
			continue
		}
		dhCompleted++
		// The winner is attributed EXACTLY as applyTiebreakSort attributes a
		// TB/DH win: resolveWinnerSide over the SIDE ids (id-only, operator
		// ruling bc-pnum). A hikiwake (WinnerID == "", or a mark that
		// resolves to neither side) counts toward round completeness but
		// adds no win, so an all-drawn round leaves every member on 0 wins -
		// a duplicate, which correctly surfaces as needing chusen below.
		winnerIsA, winnerIsB := resolveWinnerSide(m)
		switch {
		case winnerIsA:
			dhWins[m.SideAID]++
		case winnerIsB:
			dhWins[m.SideBID]++
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
		count := dhWins[s.Player.ID]
		if seen[count] {
			return true
		}
		seen[count] = true
	}
	return false
}

// ChusenStatus returns the consequential team-pool ties that the daihyosen
// left undetermined, split into those that still need a chusen (drawing lots)
// and those a recorded chusen already settled. It is the single source of
// truth for both, used by the GET /chusen-candidates endpoint: "which groups
// still need an operator lots-draw" is groupNeedsChusen, and "which groups were
// decided by chusen" is groupSettledByChusen. Empty (not an error) when the
// competition is not a team comp whose pool order can still be set by hand
// (state.Competition.AcceptsPoolRankOverride: the pools stage, or a pools +
// knockout competition's knockout, where a pool correction can reopen a tie),
// or no such group exists.
//
// Pools are returned in name order for stable output.
func (e *Engine) ChusenStatus(compID string) (ChusenReport, error) {
	var report ChusenReport
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return report, err
	}
	if comp == nil {
		return report, notFoundErrorf("competition %s not found", compID)
	}
	isTeam := comp.IsTeam()
	if !isTeam || !comp.AcceptsPoolRankOverride() {
		return report, nil
	}

	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return report, err
	}
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return report, err
	}
	poolRanks, err := e.store.LoadPoolRanks(compID)
	if err != nil {
		return report, err
	}
	pools, err := e.store.LoadPools(compID)
	if err != nil {
		return report, err
	}
	bracket, err := e.store.LoadBracket(compID)
	if err != nil {
		return report, err
	}

	poolNames := make([]string, 0, len(standings))
	for name := range standings {
		poolNames = append(poolNames, name)
	}
	sort.Strings(poolNames)

	for _, poolName := range poolNames {
		poolStandings := standings[poolName]
		groupOverrides := poolRanks[poolName]
		cut := poolTieBreakCut(comp, pools, bracket, poolName)
		for _, positions := range detectPoolTies(poolStandings) {
			// Only a tie that affects advancement/seed warrants a decider at all.
			if !tieAffectsAdvancement(positions, cut) {
				continue
			}
			group := standingsAt(poolStandings, positions)
			g := ChusenGroup{
				PoolName:    poolName,
				Teams:       group,
				MinPosition: positions[0] + 1,
			}
			switch {
			case groupNeedsChusen(group, matches, groupOverrides):
				report.Pending = append(report.Pending, g)
			case groupSettledByChusen(group, matches, groupOverrides):
				g.Ranks = make([]int, len(group))
				for i, s := range group {
					if r, ok := lookupPoolRankOverride(groupOverrides, s.Player.ID); ok {
						g.Ranks[i] = r
					}
				}
				report.Recorded = append(report.Recorded, g)
			}
		}
	}
	return report, nil
}
