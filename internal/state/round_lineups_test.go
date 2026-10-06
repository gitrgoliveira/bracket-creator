package state

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A competition with no lineups at all has nothing to move, whether the
// caller read an empty file or none: the settlement answers a map it can write
// into, changes nothing and leaves nothing waiting.
func TestSettleRoundLineups_NoLineups(t *testing.T) {
	comp := &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: CompFormatKnockout}
	players := []domain.Player{{ID: "t", Name: "Tora"}}

	for name, lineups := range map[string]map[string]domain.TeamLineup{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			for _, onLoad := range []bool{true, false} {
				res := settleRoundLineups(comp, players, lineups, nil, onLoad)

				assert.NotNil(t, res.lineups)
				assert.Empty(t, res.lineups)
				assert.False(t, res.changed)
				assert.Zero(t, res.waiting)
				assert.Empty(t, res.given)
				assert.False(t, res.givenGrew)
				assert.Empty(t, res.notes)
			}
		})
	}
}

// settleRoundLineups reads only its arguments: the lineups it was given are
// never modified, the map it answers is its own.
func TestSettleRoundLineups_NeverModifiesWhatItWasGiven(t *testing.T) {
	settledBefore := map[string][]string{"t": {"r0-m0"}}
	comp := &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: CompFormatKnockout, RoundLineupsGiven: cloneRoundLineupsGiven(settledBefore)}
	players := []domain.Player{{ID: "t", Name: "Tora"}}
	round1 := domain.TeamLineup{TeamID: "t", CompetitionID: "c", Round: 1,
		Positions: map[domain.Position]string{"1": "Sato"}, MemberIDs: map[domain.Position]string{"1": "m"}}
	given := map[string]domain.TeamLineup{teamLineupKey("t", 1): round1}
	draw := []DrawMatch{{ID: "r1-m0", SideAID: "t", SideBID: "o", Knockout: true, Round: 1}}

	res := settleRoundLineups(comp, players, given, draw, true)

	assert.True(t, res.changed)
	assert.Len(t, given, 1, "the map it was given still holds its lineup")
	assert.Contains(t, given, teamLineupKey("t", 1))
	moved := res.lineups[teamLineupMatchKey("t", "r1-m0")]
	moved.Positions["1"] = "changed"
	assert.Equal(t, "Sato", round1.Positions["1"], "a lineup it made carries its own copy of the maps")
	assert.Equal(t, "Sato", given[teamLineupKey("t", 1)].Positions["1"])
	assert.Equal(t, settledBefore, comp.RoundLineupsGiven, "the competition's record of the pairs settled is not modified")
	assert.Equal(t, map[string][]string{"t": {"r0-m0", "r1-m0"}}, res.given, "the answer is the record plus the pair it settled")
	res.given["t"][0] = "changed"
	assert.Equal(t, settledBefore, comp.RoundLineupsGiven, "and shares no list with it")
}

// settleRoundLineups gives a team that has lineups for later rounds the lineup
// v2.1.1 showed at each match it is seated in: the round lineup with the highest
// round at or below the match's, else the highest round there was.
func TestSettleRoundLineups_ReadsAsV211Did(t *testing.T) {
	comp := &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: CompFormatLeague}
	players := []domain.Player{{ID: "t", Name: "Tora"}, {ID: "o", Name: "Other"}}
	for _, tc := range []struct {
		name   string
		rounds []int
		// reads maps a match's round to the round whose lineup it shows.
		reads map[int]int
	}{
		{"one later round only: it is the highest there is, so earlier matches show it too", []int{1}, map[int]int{0: 1, 1: 1, 2: 1}},
		{"a starting lineup and a later round", []int{0, 2}, map[int]int{0: 0, 1: 0, 2: 2, 3: 2}},
		{"two later rounds and no starting lineup: an earlier match shows the highest", []int{1, 2}, map[int]int{0: 2, 1: 1, 2: 2, 3: 2}},
		{"two later rounds, the lower of them above round 1", []int{2, 3}, map[int]int{0: 3, 1: 3, 2: 2, 3: 3, 4: 3}},
		{"every round", []int{0, 1, 2}, map[int]int{0: 0, 1: 1, 2: 2, 3: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lineups := map[string]domain.TeamLineup{}
			kept := 0
			for _, r := range tc.rounds {
				lineups[teamLineupKey("t", r)] = domain.TeamLineup{TeamID: "t", CompetitionID: "c", Round: r,
					Positions: map[domain.Position]string{"1": fmt.Sprintf("round%d", r)}}
				if r >= 1 {
					kept++
				}
			}
			bouts := func(pool string) []DrawMatch {
				var draw []DrawMatch
				for round := range tc.reads {
					draw = append(draw, DrawMatch{ID: fmt.Sprintf("Pool %s-%d", pool, round), SideAID: "t", SideBID: "o", PoolRound: round})
				}
				return draw
			}

			// The load repair settles the matches the team is seated in at load.
			first := settleRoundLineups(comp, players, lineups, bouts("A"), true)
			// A write settles again, after matches it is seated in later were added.
			later := *comp
			later.RoundLineupsGiven = first.given
			second := settleRoundLineups(&later, players, first.lineups, append(bouts("A"), bouts("B")...), false)

			for round, want := range tc.reads {
				for pool, res := range map[string]roundLineupSettlement{"A": first, "B": second} {
					got := res.lineups[teamLineupMatchKey("t", fmt.Sprintf("Pool %s-%d", pool, round))]
					assert.Equal(t, fmt.Sprintf("round%d", want), got.Positions["1"], "a match at round %d, given by pass %s", round, pool)
				}
			}
			assert.Equal(t, kept, first.waiting, "the round lineups stay until the competition is completed")
			assert.Equal(t, kept, second.waiting)
			for _, r := range tc.rounds {
				if r >= 1 {
					assert.Contains(t, second.lineups, teamLineupKey("t", r))
				}
			}
		})
	}
}

// A legacy team's round lineup is kept until the competition is completed,
// whatever the draw holds (a team can still be seated in a match by a correction
// until then), and removed when it is; the marker follows, which is what waiting
// counts.
func TestSettleRoundLineups_WaitsUntilTheCompetitionIsCompleted(t *testing.T) {
	round1 := map[string]domain.TeamLineup{teamLineupKey("t", 1): {TeamID: "t", CompetitionID: "c", Round: 1,
		Positions: map[domain.Position]string{"1": "Sato"}}}
	players := []domain.Player{{ID: "t", Name: "Tora"}, {ID: "o", Name: "Other"}}
	comp := func(format string, status CompetitionStatus) *Competition {
		return &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: format, Status: status}
	}
	decided := DrawMatch{ID: "Pool A-0", SideAID: "t", SideBID: "o"}
	undecided := DrawMatch{ID: "r1-m0", SideAID: "t", Knockout: true, Round: 1}
	for _, tc := range []struct {
		name string
		comp *Competition
		draw []DrawMatch
		kept bool
	}{
		{"nothing is drawn", comp(CompFormatLeague, CompStatusSetup), nil, true},
		{"every match of a league has both sides, and the league is on", comp(CompFormatLeague, CompStatusPools), []DrawMatch{decided}, true},
		{"a knockout with a side to be decided", comp(CompFormatKnockout, CompStatusKnockout), []DrawMatch{undecided}, true},
		{"a Swiss competition between rounds", comp(CompFormatSwiss, CompStatusPools), []DrawMatch{{ID: "Swiss-R1-0", SideAID: "t", SideBID: "o"}}, true},
		{"a competition that is drawn and not started", comp(CompFormatMixed, CompStatusDrawReady), []DrawMatch{decided}, true},
		{"a completed league", comp(CompFormatLeague, CompStatusComplete), []DrawMatch{decided}, false},
		{"a completed knockout, whatever is undecided", comp(CompFormatKnockout, CompStatusComplete), []DrawMatch{undecided}, false},
		{"a completed competition with no draw", comp(CompFormatKnockout, CompStatusComplete), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := settleRoundLineups(tc.comp, players, round1, tc.draw, false)

			if tc.kept {
				assert.Equal(t, 1, res.waiting)
				assert.Contains(t, res.lineups, teamLineupKey("t", 1))
			} else {
				assert.Zero(t, res.waiting)
				assert.NotContains(t, res.lineups, teamLineupKey("t", 1))
			}
		})
	}
}

// A lineup saved under a team's name is keyed by the team's id when exactly one
// team has that name, and waits while a team of that name has no id yet.
func TestSettleRoundLineups_ANameAwaitsAnIdUntilEveryTeamOfThatNameHasOne(t *testing.T) {
	comp := &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: CompFormatKnockout}
	byName := func(name string) map[string]domain.TeamLineup {
		return map[string]domain.TeamLineup{teamLineupKey(name, 0): {TeamID: name, CompetitionID: "c", Positions: map[domain.Position]string{"1": "Sato"}}}
	}
	for _, tc := range []struct {
		name    string
		players []domain.Player
		keyedBy string
		waiting int
	}{
		{"one team, with an id", []domain.Player{{ID: "t", Name: "Tora"}}, "t", 0},
		{"one team, with no id yet", []domain.Player{{Name: "Tora"}}, "Tora", 1},
		{"two teams of the name, one with no id yet", []domain.Player{{ID: "t", Name: "Tora"}, {Name: "Tora"}}, "Tora", 1},
		{"two teams of the name, both with ids", []domain.Player{{ID: "t", Name: "Tora"}, {ID: "u", Name: "Tora"}}, "Tora", 0},
		{"no team of the name", []domain.Player{{ID: "t", Name: "Usagi"}}, "Tora", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := settleRoundLineups(comp, tc.players, byName("Tora"), nil, true)

			assert.Contains(t, res.lineups, teamLineupKey(tc.keyedBy, 0), "keyed by %q", tc.keyedBy)
			assert.Equal(t, tc.waiting, res.waiting)
		})
	}
}

// Settling what it has already settled decides nothing.
func TestSettleRoundLineups_IsIdempotent(t *testing.T) {
	comp := &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: CompFormatMixed}
	players := []domain.Player{{ID: "t", Name: "Tora"}, {ID: "o", Name: "Other"}}
	lineups := map[string]domain.TeamLineup{
		teamLineupKey("t", 0): {TeamID: "t", CompetitionID: "c", Round: 0, Positions: map[domain.Position]string{"1": "Sato"}},
		teamLineupKey("t", 1): {TeamID: "t", CompetitionID: "c", Round: 1, Positions: map[domain.Position]string{"1": "Ito"}},
	}
	draw := []DrawMatch{
		{ID: "Pool A-0", SideAID: "t", SideBID: "o", PoolRound: 1},
		{ID: "r0-m0", Knockout: true},
	}

	first := settleRoundLineups(comp, players, lineups, draw, true)
	again := *comp
	again.RoundLineupsGiven = first.given
	second := settleRoundLineups(&again, players, first.lineups, draw, false)

	assert.True(t, first.changed)
	assert.True(t, first.givenGrew)
	assert.Equal(t, map[string][]string{"t": {"Pool A-0"}}, first.given)
	assert.Equal(t, 1, first.waiting)
	assert.False(t, second.changed)
	assert.False(t, second.givenGrew)
	assert.Equal(t, first.lineups, second.lineups)
	assert.Equal(t, first.given, second.given)
	assert.Equal(t, 1, second.waiting)
	assert.Empty(t, second.notes)
}

// settlementComp is a team competition of the format, in the status.
func settlementComp(format string, status CompetitionStatus) *Competition {
	return &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: format, Status: status}
}

// round1For is the lineups of team "t" that a Lineups page saved a round 1
// lineup in, as settleRoundLineups is given them.
func round1For() map[string]domain.TeamLineup {
	return map[string]domain.TeamLineup{teamLineupKey("t", 1): {TeamID: "t", CompetitionID: "c", Round: 1,
		Positions: map[domain.Position]string{"1": "Sato"}}}
}

// A legacy team is given a lineup for the team matches it is seated in by id, and
// for no other: a structural bye (hidden) is a match nobody plays, a pool
// representative bout or tie-break is one individual bout, a league's
// bracket.json is vestigial, and a match the team is not in is not its own.
func TestSettleRoundLineups_GivesLineupsOnlyForTheMatchesATeamPlays(t *testing.T) {
	players := []domain.Player{{ID: "t", Name: "Tora"}, {ID: "o", Name: "Other"}}
	decided := DrawMatch{ID: "Pool A-0", SideAID: "t", SideBID: "o"}
	for _, tc := range []struct {
		name string
		comp *Competition
		draw []DrawMatch
		// matches are the ids of the matches a lineup is given for.
		matches []string
	}{
		{"the matches of a knockout", settlementComp(CompFormatKnockout, CompStatusKnockout),
			[]DrawMatch{{ID: "r0-m0", SideAID: "t", SideBID: "o", Knockout: true}, {ID: "r1-m0", SideAID: "t", Knockout: true, Round: 1}}, []string{"r0-m0", "r1-m0"}},
		{"a pool match whose other side has no id", settlementComp(CompFormatLeague, CompStatusPools),
			[]DrawMatch{{ID: "Pool A-0", SideAID: "t"}}, []string{"Pool A-0"}},
		{"a hidden bye", settlementComp(CompFormatKnockout, CompStatusKnockout),
			[]DrawMatch{{ID: "r0-m0", SideAID: "t", Knockout: true, Hidden: true}, {ID: "r1-m0", SideAID: "t", SideBID: "o", Knockout: true, Round: 1}}, []string{"r1-m0"}},
		{"a pool representative bout and a tie-break", settlementComp(CompFormatLeague, CompStatusPools),
			[]DrawMatch{decided, {ID: "Pool A-DH-1", SideAID: "t", SideBID: "o"}, {ID: "Pool A-TB-1", SideAID: "t", SideBID: "o"}}, []string{"Pool A-0"}},
		{"a league's vestigial bracket", settlementComp(CompFormatLeague, CompStatusPools),
			[]DrawMatch{decided, {ID: "r0-m0", SideAID: "t", SideBID: "o", Knockout: true}}, []string{"Pool A-0"}},
		{"a match the team is not in", settlementComp(CompFormatLeague, CompStatusPools),
			[]DrawMatch{{ID: "Pool A-0", SideAID: "o", SideBID: "x"}}, nil},
		{"the 3rd-place match", settlementComp(CompFormatKnockout, CompStatusKnockout),
			[]DrawMatch{{ID: "r0-m0", SideAID: "t", SideBID: "o", Knockout: true}, {ID: BronzeMatchID, SideAID: "t", SideBID: "o", Knockout: true, Round: 1}}, []string{"r0-m0", BronzeMatchID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := settleRoundLineups(tc.comp, players, round1For(), tc.draw, false)

			var want map[string][]string
			if len(tc.matches) > 0 {
				want = map[string][]string{"t": slices.Sorted(slices.Values(tc.matches))}
			}
			assert.Equal(t, want, res.given)
			for _, id := range tc.matches {
				assert.Contains(t, res.lineups, teamLineupMatchKey("t", id))
			}
			assert.Len(t, res.lineups, 1+len(tc.matches), "the round lineup and one lineup per match")
		})
	}
}

// A legacy team that has no starting lineup is given, on the load pass, the
// lineup v2.1.1 showed before any round it saved: the highest, which v2.1.1
// fell back to. A match seated later and read at a round below the team's
// lowest then reads that lineup, as v2.1.1 did.
func TestSettleRoundLineups_SeedsTheStartingLineupWithWhatV211ShowedBeforeAnyRound(t *testing.T) {
	comp := settlementComp(CompFormatMixed, CompStatusPools)
	players := []domain.Player{{ID: "t", Name: "Tora"}}
	for _, tc := range []struct {
		name   string
		rounds []int
		onLoad bool
		// start is the lineup the team has as its starting lineup afterwards, "" for none.
		start string
	}{
		{"one later round", []int{1}, true, "round1"},
		{"two later rounds: v2.1.1 fell back to the highest", []int{1, 2}, true, "round2"},
		{"two later rounds, the lower above round 1", []int{2, 3}, true, "round3"},
		{"a starting lineup of its own is kept", []int{0, 2}, true, "round0"},
		{"a write seeds nothing", []int{1, 2}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lineups := map[string]domain.TeamLineup{}
			for _, r := range tc.rounds {
				lineups[teamLineupKey("t", r)] = domain.TeamLineup{TeamID: "t", CompetitionID: "c", Round: r,
					Positions: map[domain.Position]string{"1": fmt.Sprintf("round%d", r)}}
			}

			res := settleRoundLineups(comp, players, lineups, nil, tc.onLoad)

			start, ok := res.lineups[teamLineupKey("t", 0)]
			if tc.start == "" {
				assert.False(t, ok)
				return
			}
			assert.True(t, ok)
			assert.Equal(t, tc.start, start.Positions["1"])
			if slices.Contains(tc.rounds, 0) {
				return // the team's own lineup, not a copy
			}
			start.Positions["1"] = "changed"
			for _, r := range tc.rounds {
				assert.NotEqual(t, "changed", lineups[teamLineupKey("t", r)].Positions["1"], "a seeded starting lineup carries its own copy of the maps")
			}
		})
	}
}

// A pair the competition's record lists as settled is left alone, whatever the
// lineups hold: a lineup the operator removed from a match stays removed. A
// seated pair the record does not list is given the lineup v2.1.1 showed there
// when the team has none, and listed either way.
func TestSettleRoundLineups_APairAlreadyGivenIsLeftAlone(t *testing.T) {
	comp := settlementComp(CompFormatLeague, CompStatusPools)
	comp.RoundLineupsGiven = map[string][]string{"t": {"Pool A-0"}}
	players := []domain.Player{{ID: "t", Name: "Tora"}, {ID: "o", Name: "Other"}}
	lineups := round1For()
	own := domain.TeamLineup{TeamID: "t", CompetitionID: "c", MatchID: "Pool A-2", Positions: map[domain.Position]string{"1": "Ito"}}
	lineups[teamLineupMatchKey("t", "Pool A-2")] = own
	draw := []DrawMatch{
		{ID: "Pool A-0", SideAID: "t", SideBID: "o"},
		{ID: "Pool A-1", SideAID: "t", SideBID: "o"},
		{ID: "Pool A-2", SideAID: "t", SideBID: "o"},
	}

	res := settleRoundLineups(comp, players, lineups, draw, false)

	assert.NotContains(t, res.lineups, teamLineupMatchKey("t", "Pool A-0"), "listed, and without a lineup: the operator removed it, and that stands")
	assert.Equal(t, "Sato", res.lineups[teamLineupMatchKey("t", "Pool A-1")].Positions["1"], "not listed: given what v2.1.1 showed")
	assert.Equal(t, own, res.lineups[teamLineupMatchKey("t", "Pool A-2")], "not listed, with a lineup of its own: that is v2.1.1's reading, and is left as it is")
	assert.Equal(t, map[string][]string{"t": {"Pool A-0", "Pool A-1", "Pool A-2"}}, res.given, "all three are settled now")
	assert.True(t, res.givenGrew)
}

// The record keeps each team's list sorted and without duplicates, whatever a
// hand-edited config.md held: a list with its ids out of order or repeated is
// read as the set it is, and a team with no ids is not kept.
func TestSettleRoundLineups_KeepsEachTeamsListSortedWithoutDuplicates(t *testing.T) {
	comp := settlementComp(CompFormatLeague, CompStatusPools)
	comp.RoundLineupsGiven = map[string][]string{"t": {"Pool A-1", "Pool A-0", "Pool A-1"}, "nobody": {}}
	players := []domain.Player{{ID: "t", Name: "Tora"}, {ID: "o", Name: "Other"}}
	draw := []DrawMatch{
		{ID: "Pool A-0", SideAID: "t", SideBID: "o"},
		{ID: "Pool A-1", SideAID: "t", SideBID: "o"},
		{ID: "Pool A-2", SideAID: "t", SideBID: "o"},
	}

	res := settleRoundLineups(comp, players, round1For(), draw, false)

	assert.Equal(t, map[string][]string{"t": {"Pool A-0", "Pool A-1", "Pool A-2"}}, res.given)
	assert.NotContains(t, res.lineups, teamLineupMatchKey("t", "Pool A-0"), "a pair listed out of order is still listed")
	assert.NotContains(t, res.lineups, teamLineupMatchKey("t", "Pool A-1"))
	assert.Contains(t, res.lineups, teamLineupMatchKey("t", "Pool A-2"))
}

// A copy of a competition shares no part of its record of the pairs settled: not
// the map, and not a team's list.
func TestCopyCompetition_DeepCopiesTheRoundLineupsGiven(t *testing.T) {
	original := &Competition{ID: "c", RoundLineupsGiven: map[string][]string{"t": {"r0-m0", "r1-m0"}}}

	cp := (&Store{}).copyCompetition(original)
	cp.RoundLineupsGiven["t"][0] = "changed"
	cp.RoundLineupsGiven["u"] = []string{"r0-m1"}

	assert.Equal(t, map[string][]string{"t": {"r0-m0", "r1-m0"}}, original.RoundLineupsGiven)
	assert.Nil(t, (&Store{}).copyCompetition(&Competition{ID: "c"}).RoundLineupsGiven, "no record stays none")
}

// The lineups are saved before the competition's record. If saving the record
// failed, the next pass finds each lineup it gave and only lists the pairs.
func TestSettleRoundLineups_LineupsThatLandedBeforeTheRecordOnlyAddThePairs(t *testing.T) {
	comp := settlementComp(CompFormatLeague, CompStatusPools)
	players := []domain.Player{{ID: "t", Name: "Tora"}, {ID: "o", Name: "Other"}}
	draw := []DrawMatch{
		{ID: "Pool A-0", SideAID: "t", SideBID: "o"},
		{ID: "Pool A-1", SideAID: "t", SideBID: "o"},
	}
	landed := settleRoundLineups(comp, players, round1For(), draw, false)
	require.True(t, landed.changed)

	next := settleRoundLineups(comp, players, landed.lineups, draw, false) // comp still lists nothing

	assert.False(t, next.changed, "nothing to give: the lineups are there")
	assert.True(t, next.givenGrew)
	assert.Equal(t, landed.given, next.given)
	assert.Equal(t, landed.lineups, next.lineups)
}
