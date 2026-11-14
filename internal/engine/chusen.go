package engine

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ChusenGroup is a consequential pool tie that its tie-breaker could not
// separate: for a team, two or more members share the same daihyosen win count
// (a true win/loss cycle, an all-drawn round, or any other tie in win counts);
// for an individual, a finished ippon-shobu round left some of them level (a
// circle, every bout won). The finishing order is still undetermined. Per the
// rules (running_a_kendo_tournament.md:181, EKC 6.2.5.1) the last resort is
// chusen (drawing lots): the operator draws lots and records the order, which
// persists as a per-pool rank override and lets the competition advance. An
// individual group can be fought again instead (StillLevel, RunTiebreakerRound),
// the operator's choice (decision F5).
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
	// StillLevel lists, for an individual group still waiting for a decision,
	// the runs of its members a finished ippon-shobu round left level
	// (tieBreakLeftLevel), in standings order: each is what "Run another
	// tie-breaker round" fights again (RunTiebreakerRound). The lots, when the
	// operator chooses them, rank the whole group (Teams), as for a team. Nil
	// for a team group and for a recorded one.
	StillLevel [][]state.PlayerStanding
}

// ChusenReport is every consequential pool tie a tie-breaker left
// undetermined, split by whether the operator has recorded its chusen yet.
type ChusenReport struct {
	// Pending groups still need a chusen (drawing lots), or for an individual
	// group the lots or another tie-breaker round (StillLevel).
	Pending []ChusenGroup
	// Recorded groups were settled by a chusen already recorded
	// (groupSettledByChusen). The operator may change the recorded order, in
	// case it was entered wrongly.
	Recorded []ChusenGroup
}

// groupNeedsChusen reports whether a tied group remains unresolved after its
// tie-breaker (tieBreakerLeftTied) and therefore needs a chusen (drawing lots),
// or for an individual group another round instead. It is the single
// per-group predicate shared by dhCycleExists (which blocks auto-completion) and
// ChusenStatus (which surfaces the groups to the operator). group is rows of
// computed standings, whose IsOverridden says which recorded ranks are in force
// (poolRanksInForce).
//
// Returns false when the operator has already ranked every member (chusen
// recorded, chusenRecorded) or when tieBreakerLeftTied says the bouts settled
// the order or have not all been fought yet.
func groupNeedsChusen(group []state.PlayerStanding, allMatches []state.MatchResult) bool {
	return !chusenRecorded(group) && tieBreakerLeftTied(group, allMatches)
}

// groupSettledByChusen reports whether a tied group WAS decided by chusen: its
// tie-breaker left the order undetermined, exactly as groupNeedsChusen requires,
// and the operator has since recorded a rank for every member. It is the one
// owner of "which groups were decided by chusen", which are the only groups
// whose recorded order the operator is offered to change: a chusen entered in
// the wrong order must stay fixable, but no other rank is set by hand.
func groupSettledByChusen(group []state.PlayerStanding, allMatches []state.MatchResult) bool {
	return chusenRecorded(group) && tieBreakerLeftTied(group, allMatches)
}

// tieBreakerLeftTied reports whether a tied group's tie-breaker left its order
// undetermined, whatever the operator has recorded since: its daihyosen round
// (daihyosenLeftTied, a team's) or a finished ippon-shobu round that left a run
// of it level (tieBreakLeftLevel, an individual's: a circle, every bout won).
// Only one kind of row is ever held for a competition (InjectPoolDaihyosenMatches
// for a team one, InjectTiebreakerMatches for an individual one), so asking both
// is the kind-aware question with no kind to pass. An individual group left
// level has two ways on, another tie-breaker round (RunTiebreakerRound) or the
// lots, both the operator's choice (decision F5); the lots are why it is asked
// here.
func tieBreakerLeftTied(group []state.PlayerStanding, allMatches []state.MatchResult) bool {
	return daihyosenLeftTied(group, allMatches) || len(tieBreakLeftLevel(group, allMatches)) > 0
}

// poolRanksInForce is the ONE owner of which recorded pool ranks govern a
// pool's order now; computeStandingsFrom asks it and marks the rows it keeps
// IsOverridden, and every other reader of a chusen (chusenRecorded, so
// ChusenStatus and dhCycleExists; drawnSupplementaryRuledByChusen; the Pools tab's notice
// in admin_pools.jsx) reads that mark off the standings, never the recorded
// ranks. sorted is the pool's standings in Points order (after the
// supplementary bouts' sort, which reorders only within a tied group); recorded
// is the pool's recorded ranks (state.Overrides.PoolRanks[pool]), keyed and read
// by participant id only (lookupPoolRankOverride).
//
// A recorded rank is in force only for a member of a group the standings still
// hold level on Points that no fought supplementary result has settled
// (tieSettledByBouts): there the lots are the only order there is, and they
// order that group alone. Anywhere else the rank has outlived the tie it was
// drawn for and is ignored, with nothing written: a regular-match correction
// that moved the team off the tie, or a re-fought representative bout that now
// names a winner, decides the order instead, and a correction that puts the tie
// back puts the lots back with it.
func poolRanksInForce(sorted []state.PlayerStanding, matches []state.MatchResult, isTeam bool, recorded map[string]int) map[string]int {
	if len(recorded) == 0 {
		return nil
	}
	var inForce map[string]int
	for _, positions := range detectPoolTies(sorted) {
		group := standingsAt(sorted, positions)
		if tieSettledByBouts(group, matches, isTeam) {
			continue
		}
		for _, s := range group {
			if r, ok := lookupPoolRankOverride(recorded, s.Player.ID); ok {
				if inForce == nil {
					inForce = map[string]int{}
				}
				inForce[helper.CompetitorKey(s.Player.ID, "", "")] = r
			}
		}
	}
	return inForce
}

// drawnSupplementaryRuledByChusen reports a drawn pool representative ("-DH-")
// or tie-break ("-TB-") bout whose tied group a chusen has ranked: the lots were
// drawn outside the app over the group the bout could not separate, and the
// organiser entered the order they gave, so the draw needs no rematch and the
// pool may complete (poolMatchSettled), and a result for the bout is first
// warned about (refuseOverRecordedLots). The app never changes or overturns a
// recorded order by itself, so a competitor reinstated afterwards does not reopen
// the bout: the lots stay in force until the organiser corrects them or confirms
// that the bout's result replaces them. The group is the run of the tied group
// the standings hold the bout's two sides in (lotsGroup: two separate ties in a
// pool stay apart, and so do the runs the fought bouts left level, which the
// lots rank one by one), and the lots rule the bout only once every one of those
// competitors carries a rank in force (IsOverridden, poolRanksInForce), like
// chusenRecorded: a chusen still being entered, or one over a smaller run,
// settles nothing. poolStandings is the competition's pool standings (read only
// when a drawn supplementary bout exists, lotsStandings); a pool they do not
// hold, or a side they no longer hold level with the other
// (drawnSupplementaryMoot), rules nothing. Competitors are read by participant
// id only.
func drawnSupplementaryRuledByChusen(m *state.MatchResult, poolStandings map[string][]state.PlayerStanding) bool {
	if !drawnSupplementaryBout(m) {
		return false
	}
	group, level := lotsGroup(poolStandings, m)
	return level && chusenRecorded(group)
}

// refuseOverRecordedLots warns before a write puts a drawn bout back in play, or
// gives it a winner, while the lots recorded for its tied group rule its drawn
// result (drawnSupplementaryRuledByChusen). The lots were drawn outside the app
// and the organiser entered them, so the app never draws, changes or deletes
// them on its own: the organiser may still correct the bout, once told that its
// result replaces the lots for that place. It answers a *LotsReplacedError until
// the write carries the organiser's confirmation (confirmed, ForceOptions.
// ConfirmLots); confirmed, the write lands, the bout's result decides the place
// and the lots stop applying to it (poolRanksInForce) while they stay stored.
// It reads the standings only for a drawn supplementary bout (loadPoolLots), so
// any other row costs nothing.
func (e *Engine) refuseOverRecordedLots(tx state.StoreTx, compID string, comp *state.Competition, stored *state.MatchResult, confirmed bool) error {
	if confirmed || stored == nil || !drawnSupplementaryBout(stored) {
		return nil
	}
	lots, err := loadPoolLots([]state.MatchResult{*stored}, func() (map[string][]state.PlayerStanding, error) {
		return e.computeStandingsFrom(tx, compID)
	}, nil)
	if err != nil {
		return err
	}
	if !drawnSupplementaryRuledByChusen(stored, lots.standings) {
		return nil
	}
	group, _ := lotsGroup(lots.standings, stored)
	return &LotsReplacedError{MatchID: stored.ID, Msg: lotsReplacedSentence(group, lots.standings[poolNameOf(stored.ID)])}
}

// poolNameOf is the pool a pool-match id belongs to ("" for a non-pool id).
func poolNameOf(matchID string) string {
	name, _ := poolNameFromMatchID(matchID)
	return name
}

// lotsReplacedSentence is the confirmation's sentence: the place the lots were
// entered for and the order the organiser gave, in the places they stand at in
// standings ("Bea 1st, Aki 2nd"). group is the lots' run (lotsGroup) in that
// order.
func lotsReplacedSentence(group, standings []state.PlayerStanding) string {
	first := 0
	order := make([]string, 0, len(group))
	for i, g := range group {
		at := slices.IndexFunc(standings, func(s state.PlayerStanding) bool { return s.Player.ID == g.Player.ID })
		if i == 0 {
			first = at
		}
		order = append(order, fmt.Sprintf("%s %s", g.Player.Name, helper.GetOrdinal(at+1)))
	}
	// One bout's winner decides a pair's place at once; in a larger group the lots
	// keep ordering every member until the bouts' results do (poolRanksInForce).
	tail := "The bout's result will decide the place."
	if len(group) > 2 {
		tail = "Your lots still decide the places until every bout of this tie-break has a result that puts them in order."
	}
	return fmt.Sprintf("This replaces the lots you entered for %s: %s. %s",
		helper.GetOrdinal(first+1), strings.Join(order, ", "), tail)
}

// drawnSupplementaryMoot reports a drawn supplementary bout (a representative
// "-DH-" bout or an ippon-shobu "-TB-" one) whose two sides the standings hold
// and whose tie decides no place any more: a regular-match correction split the
// tie the bout was fought for (the sides are no longer level on Points), or moved
// it outside the band a tie is broken in (TieOutOfBand, tieNeedsBreak). It
// settles as it stands (poolMatchSettled), with no rematch and no lots owed.
func drawnSupplementaryMoot(m *state.MatchResult, poolStandings map[string][]state.PlayerStanding) bool {
	if !drawnSupplementaryBout(m) {
		return false
	}
	group, level := lotsGroup(poolStandings, m)
	return group != nil && (!level || group[0].TieOutOfBand)
}

// lotsGroup is the standings group a drawn representative bout's lots had to
// rank: the run (TieRun, tieBreakRuns) of the rows level on Points with both of
// its sides that the fought bouts left level, the same stretch ChusenStatus
// offers the operator (detectPoolTies, barredDrawRuns). Lots rank a run alone,
// so a pair the fought bouts left level inside a larger Points tie is judged on
// the pair; two sides the bouts have put in different runs are judged on the
// whole level group, which no lots rank. level is false when the
// standings no longer put the two sides level (group is then the two sides'
// rows) or do not hold both (group is then nil). The pool's other
// representative bouts are not asked: a result correction can shrink a tie
// and leave the rows of its old, larger round behind, and those would pull a
// team back in that the chusen panel no longer offers.
func lotsGroup(poolStandings map[string][]state.PlayerStanding, m *state.MatchResult) (group []state.PlayerStanding, level bool) {
	poolName, ok := poolNameFromMatchID(m.ID)
	if !ok {
		return nil, false
	}
	standings := poolStandings[poolName]
	var a, b *state.PlayerStanding
	for i := range standings {
		switch standings[i].Player.ID {
		case "":
		case m.SideAID:
			a = &standings[i]
		case m.SideBID:
			b = &standings[i]
		}
	}
	if a == nil || b == nil {
		return nil, false
	}
	if a.Points != b.Points {
		return []state.PlayerStanding{*a, *b}, false
	}
	sameRun := a.TieRun == b.TieRun
	for _, s := range standings {
		if s.Points == a.Points && (!sameRun || s.TieRun == a.TieRun) {
			group = append(group, s)
		}
	}
	return group, true
}

// poolLots is what a drawn supplementary bout is judged settled on
// (poolMatchSettled): the pool standings its tied group, and the ranks in
// force over it (IsOverridden), are read from, and the competitors' statuses
// say whether the bout's two sides are both barred (drawnTiebreakOfBarredSides).
// loadPoolLots is the one door that fills it; the zero value holds none, so a
// drawn supplementary bout is never settled by it (isBlockingEngiTBRow, which
// only judges tie-break rows, passes it on purpose).
type poolLots struct {
	standings map[string][]state.PlayerStanding
	statuses  map[string]domain.CompetitorStatus
	// league is set by a caller judging a league (PoolsSettledNow, the
	// league's own completion scan): only there does a representative bout
	// drawn between two barred teams count as played (barredDrawPlayed). A
	// pools + knockout competition, and every caller that leaves it unset,
	// keeps that bout open until its lots are entered, as before.
	league bool
}

// barredDrawPlayed reports a drawn tie-break bout between two barred sides that
// counts as played (drawnTiebreakOfBarredSides): an individual ippon-shobu bout
// in any competition, a team representative bout in a league only.
func (l poolLots) barredDrawPlayed(m *state.MatchResult) bool {
	if IsPoolDaihyosenMatchID(m.ID) && !l.league {
		return false
	}
	return drawnTiebreakOfBarredSides(m, l.statuses)
}

// loadPoolLots reads a competition's lots for matches: the standings
// (lotsStandings) and the competitor statuses, only when the matches hold the
// bout that needs them. The recorded ranks are read once, inside the standings
// (computeStandingsFrom), and nowhere else. A nil loadStatuses reads none.
func loadPoolLots(matches []state.MatchResult, loadStandings func() (map[string][]state.PlayerStanding, error), loadStatuses func() (map[string]domain.CompetitorStatus, error)) (poolLots, error) {
	standings, err := lotsStandings(matches, loadStandings)
	if err != nil {
		return poolLots{}, err
	}
	lots := poolLots{standings: standings}
	if loadStatuses == nil || !slices.ContainsFunc(matches, func(m state.MatchResult) bool { return drawnSupplementaryBout(&m) }) {
		return lots, nil
	}
	if lots.statuses, err = loadStatuses(); err != nil {
		return poolLots{}, err
	}
	return lots, nil
}

// drawnTiebreakOfBarredSides reports a drawn tie-break bout (an ippon-shobu
// "-TB-" or a representative "-DH-" bout) whose two sides are both barred (a
// withdrawal on another match, engine.BarredSides), the one draw the decision
// door records for a league's tie-breaker (handleBothSidesBarredHikiwake:
// nobody can fight it, so encho and hantei are no remedy). It is a draw nobody
// fought and counts played as it stands (poolMatchSettled); the tie it was for
// stays, and in a league the place goes to the lots (barredDrawRuns).
// Judged on the live statuses, so a side reinstated since opens the bout again.
func drawnTiebreakOfBarredSides(m *state.MatchResult, statuses map[string]domain.CompetitorStatus) bool {
	if !drawnSupplementaryBout(m) {
		return false
	}
	a, b := BarredSides(statuses, m.ID, m.SideAID, m.SideBID)
	return a != nil && b != nil
}

// levelRun is a stretch of a tied group that the fought tie-breaker bouts left
// level (tieBreakRuns): the members the lots rank, and the 0-based places they
// stand at (as detectPoolTies reports them).
type levelRun struct {
	members   []state.PlayerStanding
	positions []int
}

// levelRuns lists the runs of a tied group (positions are its places, as
// detectPoolTies reports them) in the order tieBreakRuns leaves them, each with
// the places it stands at, so a run is judged on its own places.
func levelRuns(group []state.PlayerStanding, positions []int, matches []state.MatchResult, isTeam bool) []levelRun {
	var out []levelRun
	start := 0
	for _, run := range tieBreakRuns(group, matches, isTeam) {
		out = append(out, levelRun{members: run, positions: positions[start : start+len(run)]})
		start += len(run)
	}
	return out
}

// barredDrawRuns lists the runs of a league tie whose tie-breaker the bouts
// cannot settle because two of its members could not fight: every tie-break bout
// among the group's members is finished and at least one of them is a draw nobody
// fought (drawnTiebreakOfBarredSides). Each run (tieBreakRuns) holding both sides
// of such a bout owes its place to the lots the organiser draws and enters on the
// chusen panel, the panel a circle uses (operator decision Q1, round rule-3). The
// lots rank that run alone, at the places it stands at: the fought bouts that put
// other members clear of it stand, and the barred pair never decides a place the
// bouts already gave. No further round is offered, since nobody can fight it. It
// is the league's rule only (PoolsSettledNow, chusenStatusFrom): a pools +
// knockout competition keeps its own. positions are the group's places
// (detectPoolTies), matches the pool rows as stored (a drawn representative bout
// included, which chusenMatches leaves out), statuses the competitors' statuses
// now. The caller judges each run against the band and leaves out one whose lots
// are recorded.
func barredDrawRuns(group []state.PlayerStanding, positions []int, matches []state.MatchResult, statuses map[string]domain.CompetitorStatus, isTeam bool) []levelRun {
	if len(statuses) == 0 {
		return nil
	}
	ids := groupMemberIDs(group)
	var drawn []*state.MatchResult
	for i := range matches {
		m := &matches[i]
		if !state.IsPoolSupplementaryMatchID(m.ID) || !ids[m.SideAID] || !ids[m.SideBID] || m.SideAID == m.SideBID {
			continue
		}
		if m.Status != state.MatchStatusCompleted {
			return nil
		}
		if drawnTiebreakOfBarredSides(m, statuses) {
			drawn = append(drawn, m)
		}
	}
	if len(drawn) == 0 {
		return nil
	}
	var out []levelRun
	for _, run := range levelRuns(group, positions, matches, isTeam) {
		runIDs := groupMemberIDs(run.members)
		if slices.ContainsFunc(drawn, func(m *state.MatchResult) bool { return runIDs[m.SideAID] && runIDs[m.SideBID] }) {
			out = append(out, run)
		}
	}
	return out
}

// lotsRuleADrawnBout reports whether the lots recorded for group (every member
// ranked in force, chusenRecorded) rule a drawn representative or tie-break bout
// between two of its members (drawnSupplementaryRuledByChusen): the bout stays
// settled on those lots
// even after a side that was barred when it was drawn is reinstated, so the
// group stays recorded and its lots stay correctable. matches are the pool rows
// as stored, poolStandings the competition's pool standings.
func lotsRuleADrawnBout(group []state.PlayerStanding, matches []state.MatchResult, poolStandings map[string][]state.PlayerStanding) bool {
	ids := groupMemberIDs(group)
	return slices.ContainsFunc(matches, func(m state.MatchResult) bool {
		return ids[m.SideAID] && ids[m.SideBID] && drawnSupplementaryRuledByChusen(&m, poolStandings)
	})
}

// lotsStandings reads the pool standings a drawn supplementary bout is judged
// against (drawnSupplementaryRuledByChusen for the lots, drawnSupplementaryMoot for a
// split tie), through load, and only when the matches hold such a bout: with
// none, nothing consults them, so it returns nil without computing any.
// Callers hand the result to poolMatchSettled.
func lotsStandings(matches []state.MatchResult, load func() (map[string][]state.PlayerStanding, error)) (map[string][]state.PlayerStanding, error) {
	for i := range matches {
		if drawnSupplementaryBout(&matches[i]) {
			return load()
		}
	}
	return nil, nil
}

// chusenRecorded reports whether every member of a tied group carries a
// pool rank in force (IsOverridden, poolRanksInForce), i.e. the operator has
// recorded the drawn order and it still governs the group. Each member is
// judged on its own row, by participant id, so two same-name, different-dojo
// teams never share one verdict.
func chusenRecorded(group []state.PlayerStanding) bool {
	if len(group) == 0 {
		return false
	}
	for _, s := range group {
		if !s.IsOverridden {
			return false
		}
	}
	return true
}

// chusenMatches is the pool rows a competition's lots are judged on
// (daihyosenLeftTied, through dhCycleExists and ChusenStatus). In a LEAGUE a
// tie-breaker must have a winner (refuseDrawnLeagueTiebreak), so a drawn
// representative bout, which only a write made before that rule can hold, is
// a bout still to be decided by encho and hantei, never a fought one: it is
// left out, so an all-drawn round is an unfinished round and opens no lots by
// itself, which stay the operator's choice for a true cycle (every bout won).
// A pools + knockout competition's rows are returned as they are.
func chusenMatches(comp *state.Competition, matches []state.MatchResult) []state.MatchResult {
	if comp == nil || comp.Format != state.CompFormatLeague {
		return matches
	}
	out := make([]state.MatchResult, 0, len(matches))
	for i := range matches {
		if IsPoolDaihyosenMatchID(matches[i].ID) && drawnSupplementaryBout(&matches[i]) {
			continue
		}
		out = append(out, matches[i])
	}
	return out
}

// daihyosenLeftTied reports whether a tied group's daihyosen bouts left its
// order undetermined, whatever the operator has recorded since. Returns false
// when no daihyosen bout among the group has been played yet, the round is not
// complete, or the played bouts produced a strictly-ordered win count. Returns
// true whenever two or more members finish the completed round on the same
// daihyosen win count (a true win/loss cycle, an all-drawn round, or any other
// partial tie). A league's callers pass its rows through chusenMatches, so
// there a drawn bout leaves the round incomplete rather than counting as
// fought.
func daihyosenLeftTied(group []state.PlayerStanding, allMatches []state.MatchResult) bool {
	// Membership and win counts key on competitor identity by participant id
	// ONLY (operator ruling bc-pnum), matching applyTiebreakSort next door.
	// Note what this is and is not: representative bouts are team-only (an
	// individual's circle is tieBreakLeftLevel's) and two TEAMS may not share
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

// ChusenStatus returns the consequential pool ties that a tie-breaker left
// undetermined (a team's daihyosen round, an individual's ippon-shobu round
// that went in a circle), split into those that still need a decision (the
// lots, or for an individual another round, StillLevel) and those a recorded
// chusen already settled. It is the single source of truth for both, used by
// the GET /chusen-candidates endpoint and the override-rank door's check of an
// individual's lots: "which groups still need an operator decision" is
// groupNeedsChusen, and "which groups were decided by chusen" is
// groupSettledByChusen. Empty (not an error) when the competition's pool order
// can no longer be set by hand
// (state.Competition.AcceptsPoolRankOverride: the pools stage, a pools +
// knockout competition's knockout, where a pool correction can reopen a tie,
// or a finished league, whose recorded lots stay correctable), or no such
// group exists.
//
// Pools are returned in name order for stable output.
func (e *Engine) ChusenStatus(compID string) (ChusenReport, error) {
	return e.chusenStatusFrom(e.store, func() (map[string][]state.PlayerStanding, error) {
		return e.CalculatePoolStandings(compID)
	}, compID)
}

// chusenStatusFrom is ChusenStatus' body over any loader, so a write
// transaction can ask it under its own lock (the override-rank door's check of
// an individual's lots, overridePoolRanksTx) and the cached store path stays
// the read door's. standings supplies the pool standings the same way
// poolCompletion's does.
func (e *Engine) chusenStatusFrom(loader poolStandingsLoader, standings func() (map[string][]state.PlayerStanding, error), compID string) (ChusenReport, error) {
	var report ChusenReport
	comp, err := loader.LoadCompetition(compID)
	if err != nil {
		return report, err
	}
	if comp == nil {
		return report, notFoundErrorf("competition %s not found", compID)
	}
	isTeam := comp.IsTeam()
	if !comp.AcceptsPoolRankOverride() {
		return report, nil
	}

	poolStandingsByName, err := standings()
	if err != nil {
		return report, err
	}
	stored, err := loader.LoadPoolMatches(compID)
	if err != nil {
		return report, err
	}
	matches := chusenMatches(comp, stored)
	// A league's tie-breaker drawn because both its sides are barred is
	// settled by the lots (barredDrawRuns), judged on the statuses now.
	var statuses map[string]domain.CompetitorStatus
	isLeague := comp.Format == state.CompFormatLeague
	if isLeague && slices.ContainsFunc(stored, func(m state.MatchResult) bool { return drawnSupplementaryBout(&m) }) {
		if statuses, err = loader.LoadCompetitorStatus(compID); err != nil {
			return report, err
		}
	}
	poolRanks, err := loader.LoadPoolRanks(compID)
	if err != nil {
		return report, err
	}
	pools, err := loader.LoadPools(compID)
	if err != nil {
		return report, err
	}
	bracket, err := loader.LoadBracket(compID)
	if err != nil {
		return report, err
	}

	poolNames := make([]string, 0, len(poolStandingsByName))
	for name := range poolStandingsByName {
		poolNames = append(poolNames, name)
	}
	sort.Strings(poolNames)

	band := newBreakBand(comp, pools, bracket)
	for _, poolName := range poolNames {
		poolStandings := poolStandingsByName[poolName]
		for _, positions := range detectPoolTies(poolStandings) {
			group := standingsAt(poolStandings, positions)
			// Only a tie in the band a tie-breaker is held for warrants a
			// decider at all: the same band the cycle gate blocks on.
			if !band(poolName, positions, group) {
				continue
			}
			g := ChusenGroup{
				PoolName:    poolName,
				Teams:       group,
				MinPosition: positions[0] + 1,
			}
			recorded := func(g ChusenGroup) {
				g.Ranks = make([]int, len(g.Teams))
				for i, s := range g.Teams {
					if r, ok := lookupPoolRankOverride(poolRanks[poolName], s.Player.ID); ok {
						g.Ranks[i] = r
					}
				}
				report.Recorded = append(report.Recorded, g)
			}
			switch {
			case groupNeedsChusen(group, matches):
				if !isTeam {
					g.StillLevel = tieBreakLeftLevel(group, matches)
				}
				report.Pending = append(report.Pending, g)
			case groupSettledByChusen(group, matches):
				recorded(g)
			default:
				// Lots rank a run the fought bouts left level, on its own places
				// (levelRuns), never the whole Points group. A run whose lots rule
				// a drawn bout stays recorded, and so correctable, even after a
				// side that was barred is reinstated. In a league a run holding a
				// draw nobody fought owes lots: nobody can fight another round
				// (StillLevel nil), and the places the bouts decided are kept.
				owed := map[int]bool{}
				if isLeague {
					for _, run := range barredDrawRuns(group, positions, stored, statuses, isTeam) {
						owed[run.positions[0]] = true
					}
				}
				for _, run := range levelRuns(group, positions, stored, isTeam) {
					if len(run.members) < 2 || !band(poolName, run.positions, run.members) {
						continue
					}
					rg := ChusenGroup{PoolName: poolName, Teams: run.members, MinPosition: run.positions[0] + 1}
					switch {
					case chusenRecorded(run.members) && (owed[run.positions[0]] || lotsRuleADrawnBout(run.members, stored, poolStandingsByName)):
						recorded(rg)
					case owed[run.positions[0]] && !chusenRecorded(run.members):
						report.Pending = append(report.Pending, rg)
					}
				}
			}
		}
	}
	return report, nil
}
