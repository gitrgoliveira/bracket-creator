package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
)

// An individual tie-breaker round that goes in a circle (decision F5,
// round-or-lots): the competition is held open per tied group (fail closed),
// the operator chooses another round (RunTiebreakerRound) or the lots, and
// nothing ranks the circle by itself.

// indivPoolFromResults saves a single-pool individual competition (each
// player's dojo "Dojo <name>") whose every regular match is completed with the
// winner names.
func indivPoolFromResults(t *testing.T, compID string, names []string, winner func(a, b string) string, opts ...func(*state.Competition)) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	comp := &state.Competition{
		ID: compID, Name: "Individual " + compID, Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}
	for _, opt := range opts {
		opt(comp)
	}
	require.NoError(t, store.SaveCompetition(comp))
	players := make([]helper.Player, len(names))
	for i, n := range names {
		players[i] = helper.Player{Name: n, Dojo: "Dojo " + n}
	}
	var matches []state.MatchResult
	idx := 0
	for i, a := range names {
		for _, b := range names[i+1:] {
			matches = append(matches, state.MatchResult{ID: fmt.Sprintf("Pool A-%d", idx), SideA: a, SideB: b, Status: state.MatchStatusCompleted, Court: "A", Winner: winner(a, b)})
			idx++
		}
	}
	bctest.StampIDs(players, matches)
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, matches))
	return eng, store
}

func playerID(name string) string { return bctest.StampPlayerID(name, "Dojo "+name) }

// akiBeaCalCircle: Aki beat Bea, Bea beat Cal, Cal beat Aki.
func akiBeaCalCircle(a, b string) string {
	pair := map[string]bool{a: true, b: true}
	switch {
	case pair["Aki"] && pair["Bea"]:
		return "Aki"
	case pair["Bea"] && pair["Cal"]:
		return "Bea"
	default:
		return "Cal"
	}
}

// scoreScheduledTB finishes every scheduled ippon-shobu bout with the winner
// pick names, and returns how many it finished.
func scoreScheduledTB(t *testing.T, eng *Engine, store *state.Store, compID string, pick func(a, b string) string) int {
	t.Helper()
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	scored := 0
	for i := range all {
		if !IsTiebreakerMatchID(all[i].ID) || all[i].Status != state.MatchStatusScheduled {
			continue
		}
		w := pick(all[i].SideA, all[i].SideB)
		all[i].Status = state.MatchStatusCompleted
		all[i].Winner = w
		if w == all[i].SideA {
			all[i].WinnerID = all[i].SideAID
		} else {
			all[i].WinnerID = all[i].SideBID
		}
		scored++
	}
	require.NoError(t, store.SavePoolMatches(compID, all))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)
	return scored
}

func circleIDs() []string { return []string{playerID("Aki"), playerID("Bea"), playerID("Cal")} }

func standingNames(rows []state.PlayerStanding) []string {
	out := make([]string, len(rows))
	for i, s := range rows {
		out[i] = s.Player.Name
	}
	return out
}

// circledLeague is an individual league of Aki, Bea and Cal, level at 1st on
// every criterion, whose injected ippon-shobu round went in a circle as well.
func circledLeague(t *testing.T, compID string) (*Engine, *state.Store) {
	t.Helper()
	eng, store := indivPoolFromResults(t, compID, []string{"Aki", "Bea", "Cal"}, akiBeaCalCircle)
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome, "the three-way tie at 1st gets its ippon-shobu round")
	require.Equal(t, 3, scoreScheduledTB(t, eng, store, compID, akiBeaCalCircle))
	return eng, store
}

func TestIndividualLeagueCircle_HoldsTheLeagueAndOffersRoundOrLots(t *testing.T) {
	const compID = "indiv-circle"
	eng, store := circledLeague(t, compID)

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "no further round is started by itself, and the league does not finish")
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "a circle never finishes the league in an order the app picks")
	assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID))

	injected, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	assert.Empty(t, injected, "the injector never starts the next round: the operator chooses round or lots")

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the circle is offered to the operator")
	g := report.Pending[0]
	assert.Equal(t, 1, g.MinPosition)
	assert.ElementsMatch(t, []string{"Aki", "Bea", "Cal"}, chusenTeamNames(g))
	require.Len(t, g.StillLevel, 1, "one run is left level")
	assert.ElementsMatch(t, []string{"Aki", "Bea", "Cal"}, standingNames(g.StillLevel[0]))
}

func TestRunTiebreakerRound_ASecondRoundSettlesTheLeague(t *testing.T) {
	const compID = "indiv-round-two"
	eng, store := circledLeague(t, compID)

	added, err := eng.RunTiebreakerRound(compID, "Pool A", circleIDs())
	require.NoError(t, err)
	require.Len(t, added, 3, "every pair of the circle fights again")
	ids := map[string]bool{}
	for _, m := range added {
		ids[m.ID] = true
		assert.Equal(t, state.MatchStatusScheduled, m.Status)
	}
	assert.Equal(t, map[string]bool{"Pool A-TB-3": true, "Pool A-TB-4": true, "Pool A-TB-5": true}, ids, "numbered after the first round")
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	held := 0
	for _, m := range stored {
		if ids[m.ID] {
			held++
		}
	}
	assert.Equal(t, 3, held, "the new round is saved with the pool's matches")

	_, err = eng.RunTiebreakerRound(compID, "Pool A", circleIDs())
	var verr *ValidationError
	require.ErrorAs(t, err, &verr, "a round already under way is not added twice")

	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, eng, store, compID), "the second round is still to fight")

	// Bea wins both, Cal beats Aki.
	order := rankOrder([]string{"Bea", "Cal", "Aki"})
	require.Equal(t, 3, scoreScheduledTB(t, eng, store, compID, order))
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "a decisive second round finishes the league")

	st, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Bea", "Cal", "Aki"}, standingNames(st["Pool A"]), "the table follows the second round")
}

func TestRunTiebreakerRound_ASecondCircleAsksAgain(t *testing.T) {
	const compID = "indiv-circle-twice"
	eng, store := circledLeague(t, compID)
	_, err := eng.RunTiebreakerRound(compID, "Pool A", circleIDs())
	require.NoError(t, err)
	require.Equal(t, 3, scoreScheduledTB(t, eng, store, compID, akiBeaCalCircle))

	assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID), "a second circle holds the league again")
	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1)
	require.Len(t, report.Pending[0].StillLevel, 1)

	added, err := eng.RunTiebreakerRound(compID, "Pool A", circleIDs())
	require.NoError(t, err)
	assert.Len(t, added, 3, "a third round is the operator's to run as well")
}

func TestIndividualLeagueCircle_TheLotsSettleIt(t *testing.T) {
	const compID = "indiv-circle-lots"
	eng, store := circledLeague(t, compID)

	require.NoError(t, store.SaveRankOverride(compID, "Pool A", playerID("Cal"), 1))
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", playerID("Aki"), 2))
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", playerID("Bea"), 3))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	require.Len(t, report.Recorded, 1, "the recorded lots stay correctable")
	assert.Nil(t, report.Recorded[0].StillLevel)

	_, err = eng.RunTiebreakerRound(compID, "Pool A", circleIDs())
	var verr *ValidationError
	require.ErrorAs(t, err, &verr, "the lots already ordered the circle")

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "the lots settle the circle")
	st, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Cal", "Aki", "Bea"}, standingNames(st["Pool A"]))
}

func TestMixedPoolCircle_PoolIsNotDoneUntilRoundOrLots(t *testing.T) {
	const compID = "mixed-circle"
	eng, store := indivPoolFromResults(t, compID, []string{"Aki", "Bea", "Cal"}, akiBeaCalCircle, func(c *state.Competition) {
		c.Format = state.CompFormatMixed
		c.PoolWinners = 1
	})
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)

	done, err := eng.completedPoolNames(compID, comp)
	require.NoError(t, err)
	assert.False(t, done["Pool A"], "the tie at 1st gets its round first")
	require.Equal(t, 3, scoreScheduledTB(t, eng, store, compID, akiBeaCalCircle))

	done, err = eng.completedPoolNames(compID, comp)
	require.NoError(t, err)
	assert.False(t, done["Pool A"], "a round that went in a circle leaves the pool's 1st place undecided")

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1)
	require.Len(t, report.Pending[0].StillLevel, 1)

	added, err := eng.RunTiebreakerRound(compID, "Pool A", circleIDs())
	require.NoError(t, err)
	require.Len(t, added, 3)
	require.Equal(t, 3, scoreScheduledTB(t, eng, store, compID, rankOrder([]string{"Cal", "Aki", "Bea"})))
	done, err = eng.completedPoolNames(compID, comp)
	require.NoError(t, err)
	assert.True(t, done["Pool A"], "the second round decided the pool")
}

func TestMixedPoolCircle_TheLotsMarkThePoolDone(t *testing.T) {
	const compID = "mixed-circle-lots"
	eng, store := indivPoolFromResults(t, compID, []string{"Aki", "Bea", "Cal"}, akiBeaCalCircle, func(c *state.Competition) {
		c.Format = state.CompFormatMixed
		c.PoolWinners = 1
	})
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	_, err = eng.completedPoolNames(compID, comp)
	require.NoError(t, err)
	require.Equal(t, 3, scoreScheduledTB(t, eng, store, compID, akiBeaCalCircle))

	for i, name := range []string{"Bea", "Cal", "Aki"} {
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", playerID(name), i+1))
	}
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)
	done, err := eng.completedPoolNames(compID, comp)
	require.NoError(t, err)
	assert.True(t, done["Pool A"], "the lots decide the circle")
}

func TestRunTiebreakerRound_Refusals(t *testing.T) {
	var verr *ValidationError
	var nf *NotFoundError

	t.Run("not a run left level", func(t *testing.T) {
		const compID = "indiv-round-refuse"
		eng, _ := circledLeague(t, compID)
		_, err := eng.RunTiebreakerRound(compID, "Pool A", circleIDs()[:2])
		require.ErrorAs(t, err, &verr, "part of a circle is not a run left level")
		_, err = eng.RunTiebreakerRound(compID, "Pool A", []string{playerID("Aki"), playerID("Aki")})
		require.ErrorAs(t, err, &verr)
		_, err = eng.RunTiebreakerRound(compID, "Pool B", circleIDs())
		require.ErrorAs(t, err, &nf)
	})

	t.Run("a round still being fought", func(t *testing.T) {
		const compID = "indiv-round-unfought"
		eng, _ := indivPoolFromResults(t, compID, []string{"Aki", "Bea", "Cal"}, akiBeaCalCircle)
		_, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		_, err = eng.RunTiebreakerRound(compID, "Pool A", circleIDs())
		require.ErrorAs(t, err, &verr, "the first round is not finished yet")
	})

	t.Run("a team competition", func(t *testing.T) {
		const compID = "team-round-refuse"
		eng, _ := teamLeagueFromResults(t, compID, []string{"Alpha", "Beta", "Gamma"}, rankOrder([]string{"Alpha", "Beta", "Gamma"}))
		_, err := eng.RunTiebreakerRound(compID, "Pool A", []string{teamID("Alpha"), teamID("Beta")})
		require.ErrorAs(t, err, &verr, "a team's circle is decided by the lots")
	})
}

// judgeTieBreakRounds orders a group round by round: a run the first round
// left level is ordered by its own second round, never by wins summed over
// both, which would put a run's winner level with the first round's.
func TestJudgeTieBreakRounds_OrdersRoundByRound(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{ID: "a", Name: "A"}},
		{Player: domain.Player{ID: "b", Name: "B"}},
		{Player: domain.Player{ID: "c", Name: "C"}},
		{Player: domain.Player{ID: "d", Name: "D"}},
	}
	n := 0
	won := func(winner, loser string) state.MatchResult {
		m := state.MatchResult{ID: fmt.Sprintf("Pool A-TB-%d", n), SideAID: winner, SideBID: loser, Status: state.MatchStatusCompleted, Winner: winner, WinnerID: winner}
		n++
		return m
	}
	// Round 1: A beats everyone; B, C and D go in a circle.
	round1 := []state.MatchResult{won("a", "b"), won("a", "c"), won("a", "d"), won("b", "c"), won("c", "d"), won("d", "b")}
	v := judgeTieBreakRounds(group, round1, IsTiebreakerMatchID)
	assert.False(t, v.settled)
	assert.Equal(t, []string{"A", "B", "C", "D"}, standingNames(v.order), "A first; the circle keeps its order")
	require.Len(t, v.level, 1)
	assert.Equal(t, []string{"B", "C", "D"}, standingNames(v.level[0]), "only the circle is left level")

	// Round 2 among B, C, D: B wins both (3 wins in all, as many as A).
	round2 := append(append([]state.MatchResult(nil), round1...), won("b", "c"), won("b", "d"), won("d", "c"))
	v = judgeTieBreakRounds(group, round2, IsTiebreakerMatchID)
	assert.True(t, v.settled, "the second round settled the circle")
	assert.Empty(t, v.level)
	assert.Equal(t, []string{"A", "B", "D", "C"}, standingNames(v.order), "round 1 first, then the circle's own round")
	assert.True(t, tieSettledByBouts(group, round2, false))

	// A drawn bout in the second round finishes nothing: no run is left level.
	drawn := append(append([]state.MatchResult(nil), round1...), won("b", "c"), won("b", "d"),
		state.MatchResult{ID: "Pool A-TB-99", SideAID: "d", SideBID: "c", Status: state.MatchStatusCompleted})
	v = judgeTieBreakRounds(group, drawn, IsTiebreakerMatchID)
	assert.False(t, v.settled)
	assert.Empty(t, v.level, "a round with a drawn bout is not finished, so no lots are offered over it")
}

// The injector never reuses a bout number a row still holds, even after an
// earlier row was removed.
func TestGenerateTiebreakerMatches_NumbersPastEveryRow(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{ID: "a", Name: "A"}},
		{Player: domain.Player{ID: "b", Name: "B"}},
		{Player: domain.Player{ID: "c", Name: "C"}},
	}
	existing := []state.MatchResult{{ID: "Pool A-TB-4", SideAID: "a", SideBID: "b"}}
	got := generateTiebreakerMatches("Pool A", group, len(existing), "A", existing)
	require.Len(t, got, 2)
	assert.Equal(t, "Pool A-TB-5", got[0].ID)
	assert.Equal(t, "Pool A-TB-6", got[1].ID)
}
