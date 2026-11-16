package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A tie-breaker drawn because both its sides withdrew earlier offers the
// organiser both ways on (operator ruling 2026-11-16: "They can continue with
// tie break matches or do lots. We should not be enforcing a path."): fight it
// again (the reopen; the bout waits on the court while both are barred) or
// enter the lots. Until one of them decides the place, the bout settles nothing,
// in a league and in pools + knockout alike, as a pools + knockout team's
// representative bout always did.

// barredTiebreakMixed is an individual pools + knockout competition, two pools
// of three each sending two, whose Pool A has A1 and A2 level at 1st (they drew
// each other and each beat A3), so the tie decides who is seated 1st and who
// 2nd. The scan adds their ippon-shobu bout; both are then barred (each
// withdrew on another match) and the bout is recorded as the draw "Record as
// drawn (neither can fight)" writes. Pool B is played out with a clear order.
func barredTiebreakMixed(t *testing.T, compID string) (*rqFix, string) {
	t.Helper()
	f := newRQFixture(t, compID, 2, [][]string{{"A1", "A2", "A3"}, {"B1", "B2", "B3"}})
	require.NoError(t, f.write("Pool A-0", &state.MatchResult{
		SideA: "A1", SideB: "A2", SideAID: rqID("A1"), SideBID: rqID("A2"),
		Status: state.MatchStatusCompleted, Decision: "hikiwake",
	}))
	f.scorePool("Pool A-1", "A1")
	f.scorePool("Pool A-2", "A2")
	f.scorePool("Pool B-0", "B1")
	f.scorePool("Pool B-1", "B1")
	f.scorePool("Pool B-2", "B2")
	_, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	tbs := tiebreakBoutsOf(t, f.store, f.compID)
	require.Len(t, tbs, 1, "the tie at 1st is owed its ippon-shobu bout")

	barCompetitor(t, f.store, f.compID, rqID("A1"), true)
	barCompetitor(t, f.store, f.compID, rqID("A2"), true)
	drawnByDecision(t, f.eng, f.compID, tbs[0].ID)
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	return f, tbs[0].ID
}

// seatedAt is the name seated in the knockout slot of a pool place label.
func (f *rqFix) seatedAt(label string) string {
	f.t.Helper()
	m, side := f.slot(label)
	name, _ := sideOf(m, side)
	return name
}

// poolADone reports whether Pool A counts as complete (poolCompletion).
func (f *rqFix) poolADone() bool {
	f.t.Helper()
	done, err := f.eng.completedPoolNames(f.compID, loadComp(f.t, f.store, f.compID))
	require.NoError(f.t, err)
	return done["Pool A"]
}

// A pools + knockout ippon-shobu bout between two barred competitors, recorded
// as the draw nobody fought, does not settle the pool: the lots are offered
// (with no round of their own: the other way on is fighting the bout again),
// and the knockout is seeded from the pool only once the organiser has entered
// them, in the order they gave.
func TestMixedPool_BothBarredTiebreakDrawWaitsForTheLots(t *testing.T) {
	f, tbID := barredTiebreakMixed(t, "mixed-barred-tb")

	assert.False(t, f.poolADone(), "the drawn bout settles nothing until the lots are entered or it is fought")
	assert.Equal(t, "Pool A-1st", f.seatedAt("Pool A-1st"), "no knockout place is seeded from the tie")
	assert.Equal(t, "Pool A-2nd", f.seatedAt("Pool A-2nd"))
	assert.Equal(t, PoolsBarredDrawOpen, poolsVerdictNow(t, f.eng, f.store, f.compID))

	report, err := f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the lots are offered")
	assert.Equal(t, "Pool A", report.Pending[0].PoolName)
	assert.Equal(t, 1, report.Pending[0].MinPosition)
	assert.Nil(t, report.Pending[0].StillLevel, "no round of its own: fighting the drawn bout again is the reopen")
	assert.ElementsMatch(t, []string{"A1", "A2"}, chusenTeamNames(report.Pending[0]))

	_, err = f.eng.OverridePoolRanks(f.compID, "Pool A", []RankOverride{{PlayerID: rqID("A2"), Rank: 1}, {PlayerID: rqID("A1"), Rank: 2}})
	require.NoError(t, err, "the lots are accepted for the pair nobody can fight")
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)

	assert.True(t, f.poolADone())
	assert.Equal(t, PoolsSettled, poolsVerdictNow(t, f.eng, f.store, f.compID))
	assert.Equal(t, "A2", f.seatedAt("Pool A-1st"), "the lots put A2 1st")
	assert.Equal(t, "A1", f.seatedAt("Pool A-2nd"))
	report, err = f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	require.Len(t, report.Recorded, 1, "the lots stay correctable")
	assert.ElementsMatch(t, []string{"A1", "A2"}, chusenTeamNames(report.Recorded[0]))

	// A side reinstated afterwards does not reopen the bout: the lots the
	// organiser entered stay in force and stay correctable.
	barCompetitor(t, f.store, f.compID, rqID("A1"), false)
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	assert.Equal(t, "A2", f.seatedAt("Pool A-1st"))
	report, err = f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	require.Len(t, report.Recorded, 1)
	assert.Equal(t, state.MatchStatusCompleted, poolMatchStatus(t, f.store, f.compID, tbID))
}

// The other way on: the organiser fights the drawn bout again. The reopen is
// allowed while both are barred; the bout then waits on the court (scheduled,
// the console shows the barred notice) and the pool waits for it. No lots are
// offered for the reopened bout until it is drawn again (barredDrawRuns needs a
// drawn bout): accepted. Once both are reinstated it is fought, and its winner
// takes the place.
func TestMixedPool_BarredTiebreakDrawCanBeFoughtAgain(t *testing.T) {
	f, tbID := barredTiebreakMixed(t, "mixed-barred-tb-refight")

	_, err := f.eng.ReopenMatch(f.compID, tbID, "fight it again")
	require.NoError(t, err, "the reopen is the organiser's choice")
	assert.Equal(t, state.MatchStatusScheduled, poolMatchStatus(t, f.store, f.compID, tbID), "both still barred: the bout waits")
	assert.False(t, f.poolADone(), "the pool waits for the bout")
	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, f.eng, f.store, f.compID))
	report, err := f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending, "a bout still to fight owes no lots")

	barCompetitor(t, f.store, f.compID, rqID("A1"), false)
	barCompetitor(t, f.store, f.compID, rqID("A2"), false)
	f.scorePool(tbID, "A2")
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	assert.True(t, f.poolADone())
	assert.Equal(t, "A2", f.seatedAt("Pool A-1st"), "the bout's winner takes 1st")
	assert.Equal(t, "A1", f.seatedAt("Pool A-2nd"))
}

// Before the lots, a side reinstated makes the bout one to fight again, as for
// any drawn ippon-shobu bout in pools + knockout: no lots are owed for it.
func TestMixedPool_BarredTiebreakDrawReinstatedBeforeTheLotsIsFoughtAgain(t *testing.T) {
	f, _ := barredTiebreakMixed(t, "mixed-barred-tb-reinstate")
	barCompetitor(t, f.store, f.compID, rqID("A1"), false)

	report, err := f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending, "the bout can be fought again")
	assert.False(t, f.poolADone(), "the drawn bout still settles nothing")
	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, f.eng, f.store, f.compID))
}

// A tie a correction has moved below the qualifying places decides nothing: the
// drawn bout between its two barred sides is moot, no lots are owed and the
// pool is seeded as it stands.
func TestMixedPool_BarredTiebreakDrawOutsideTheBandIsMoot(t *testing.T) {
	const compID = "mixed-barred-tb-moot"
	f := newRQFixture(t, compID, 2, [][]string{{"A1", "A2", "A3", "A4"}, {"B1", "B2", "B3"}})
	// Pool A: A1 beats all; A2 and A3 draw and each beat A4, level at 2nd.
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool A-1", "A1")
	f.scorePool("Pool A-2", "A1")
	require.NoError(t, f.write("Pool A-3", &state.MatchResult{
		SideA: "A2", SideB: "A3", SideAID: rqID("A2"), SideBID: rqID("A3"),
		Status: state.MatchStatusCompleted, Decision: "hikiwake",
	}))
	f.scorePool("Pool A-4", "A2")
	f.scorePool("Pool A-5", "A3")
	f.scorePool("Pool B-0", "B1")
	f.scorePool("Pool B-1", "B1")
	f.scorePool("Pool B-2", "B2")
	_, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	tbs := tiebreakBoutsOf(t, f.store, f.compID)
	require.Len(t, tbs, 1, "the tie at 2nd is owed its ippon-shobu bout")
	barCompetitor(t, f.store, f.compID, rqID("A2"), true)
	barCompetitor(t, f.store, f.compID, rqID("A3"), true)
	drawnByDecision(t, f.eng, f.compID, tbs[0].ID)
	assert.False(t, f.poolADone(), "inside the band the draw waits")

	// A correction: A4 beat A2 and A3 after all, so A4 is 2nd alone and the pair
	// is level at 3rd, which sends nobody.
	all, err := f.store.LoadPoolMatches(f.compID)
	require.NoError(t, err)
	for i := range all {
		if all[i].ID == "Pool A-4" || all[i].ID == "Pool A-5" {
			all[i].Winner, all[i].WinnerID = "A4", rqID("A4")
			all[i].IpponsA, all[i].IpponsB = nil, []string{"M"}
		}
	}
	require.NoError(t, f.store.SavePoolMatches(f.compID, all))
	f.eng.standingsCache.Delete(f.compID)
	f.eng.standingsFlight.Delete(f.compID)

	report, err := f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending, "a tie below the qualifying places owes no lots")
	assert.True(t, f.poolADone(), "the moot draw settles as it stands")
	f.resolve()
	assert.Equal(t, "A4", f.seatedAt("Pool A-2nd"))
}

// A league's both-barred draw follows the same rule: it settles nothing, the
// lots are offered, and the organiser may fight the bout again instead; the
// reopen is allowed and the league then waits for the bout.
func TestIndividualLeague_BarredTiebreakDrawCanBeFoughtAgain(t *testing.T) {
	const compID = "ind-barred-tb-refight"
	eng, store, tbID := barredTiebreakLeague(t, compID)
	barCompetitor(t, store, compID, "ayu-id", true)
	barCompetitor(t, store, compID, "ben-id", true)
	drawnByDecision(t, eng, compID, tbID)
	require.Equal(t, PoolsBarredDrawOpen, poolsVerdictNow(t, eng, store, compID))

	_, err := eng.ReopenMatch(compID, tbID, "fight it again")
	require.NoError(t, err, "the reopen is the organiser's choice")
	assert.Equal(t, state.MatchStatusScheduled, poolMatchStatus(t, store, compID, tbID), "both still barred: the bout waits")
	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, eng, store, compID), "the league waits for the bout")
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.NotEqual(t, AutoCompleteTransitioned, outcome)
}

// A both-barred draw whose two sides the fought bouts already put each in a run
// of its own decides no place: neither can fight it, so its result cannot move
// them, and no lots are owed for it. It settles as it stands
// (drawnBarredDecidesNoPlace). Four level, then a round: Ayu beats Cal, Dee
// beats Ayu, Cal beats Ben, Dee beats Ben, Cal beats Dee, and Ayu v Ben is drawn
// with both barred: Cal and Dee 2 wins (a run of two, owed its lots or another
// round), Ayu 1 (3rd alone), Ben 0 (4th alone).
var aloneRound = map[string]bool{"ayu|cal": true, "dee|ayu": true, "cal|ben": true, "dee|ben": true, "cal|dee": true}

// scoreAloneRound scores the tie-break round as aloneRound says, key(id) giving
// each side's name in it, and records Ayu v Ben as the draw nobody could fight,
// once bar has barred both.
func scoreAloneRound(t *testing.T, eng *Engine, store *state.Store, compID string, key func(id string) string, bar func(id string)) {
	t.Helper()
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var drawn string
	for i := range all {
		m := &all[i]
		if !IsTiebreakerMatchID(m.ID) {
			continue
		}
		a, b := key(m.SideAID), key(m.SideBID)
		switch {
		case aloneRound[a+"|"+b]:
			m.Status, m.Winner, m.WinnerID, m.IpponsA = state.MatchStatusCompleted, m.SideA, m.SideAID, []string{"M"}
		case aloneRound[b+"|"+a]:
			m.Status, m.Winner, m.WinnerID, m.IpponsB = state.MatchStatusCompleted, m.SideB, m.SideBID, []string{"M"}
		default:
			require.ElementsMatch(t, []string{"ayu", "ben"}, []string{a, b}, "the one bout left is Ayu v Ben")
			drawn = m.ID
			bar(m.SideAID)
			bar(m.SideBID)
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)
	require.NotEmpty(t, drawn)
	drawnByDecision(t, eng, compID, drawn)
}

func TestIndividualLeague_BarredDrawWhoseSidesStandAloneDecidesNoPlace(t *testing.T) {
	const compID = "ind-barred-alone"
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	players := []helper.Player{
		{ID: "ayu", Name: "Ayu", Dojo: "Dojo A"}, {ID: "ben", Name: "Ben", Dojo: "Dojo B"},
		{ID: "cal", Name: "Cal", Dojo: "Dojo C"}, {ID: "dee", Name: "Dee", Dojo: "Dojo D"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	var regular []state.MatchResult
	for i := range players {
		for j := i + 1; j < len(players); j++ {
			regular = append(regular, state.MatchResult{
				ID: "Pool A-" + string(rune('0'+len(regular))), SideA: players[i].Name, SideAID: players[i].ID,
				SideB: players[j].Name, SideBID: players[j].ID, Court: "A",
				Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
			})
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, regular))
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome, "four level at 1st owe a round")
	scoreAloneRound(t, eng, store, compID, func(id string) string { return id }, func(id string) { barCompetitor(t, store, compID, id, true) })

	assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID), "only Cal and Dee's tie is pending, not the barred draw")
	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1)
	assert.ElementsMatch(t, []string{"Cal", "Dee"}, chusenTeamNames(report.Pending[0]))

	standings := enterLots(t, eng, compID, []string{"cal", "dee"})
	assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID), "the barred draw decides no place once the run of two has its lots")
	rank := map[string]int{}
	for _, s := range standings {
		rank[s.Player.Name] = s.Rank
	}
	assert.Equal(t, map[string]int{"Cal": 1, "Dee": 2, "Ayu": 3, "Ben": 4}, rank)
	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "the league finishes")

	// Reinstated, Ayu can fight the bout again, and a win would put Ayu level
	// with Cal and Dee: the bout decides a place again, so the finished league
	// goes back into match play.
	barCompetitor(t, store, compID, "ayu", false)
	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, eng, store, compID))
	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteStarted, outcome, "the league reopens for the bout")
}

func TestMixedPool_BarredDrawWhoseSidesStandAloneDecidesNoPlace(t *testing.T) {
	const compID = "mixed-barred-alone"
	f := newRQFixture(t, compID, 2, [][]string{{"A1", "A2", "A3", "A4"}, {"B1", "B2", "B3"}})
	all, err := f.store.LoadPoolMatches(f.compID)
	require.NoError(t, err)
	for _, m := range all {
		if pn, _ := poolNameFromMatchID(m.ID); pn != "Pool A" {
			continue
		}
		require.NoError(t, f.write(m.ID, &state.MatchResult{
			SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID,
			Status: state.MatchStatusCompleted, Decision: "hikiwake",
		}))
	}
	f.scorePool("Pool B-0", "B1")
	f.scorePool("Pool B-1", "B1")
	f.scorePool("Pool B-2", "B2")
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	require.Len(t, tiebreakBoutsOf(t, f.store, f.compID), 6, "four level owe a round of six bouts")
	key := map[string]string{rqID("A1"): "ayu", rqID("A2"): "ben", rqID("A3"): "cal", rqID("A4"): "dee"}
	scoreAloneRound(t, f.eng, f.store, f.compID, func(id string) string { return key[id] }, func(id string) { barCompetitor(t, f.store, f.compID, id, true) })
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)

	assert.False(t, f.poolADone(), "Cal and Dee's places wait for their lots or another round")
	report, err := f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1)
	assert.ElementsMatch(t, []string{"A3", "A4"}, chusenTeamNames(report.Pending[0]))

	_, err = f.eng.OverridePoolRanks(f.compID, "Pool A", []RankOverride{{PlayerID: rqID("A3"), Rank: 1}, {PlayerID: rqID("A4"), Rank: 2}})
	require.NoError(t, err)
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	assert.True(t, f.poolADone(), "the barred draw decides no place: the pool completes")
	assert.Equal(t, PoolsSettled, poolsVerdictNow(t, f.eng, f.store, f.compID))
	assert.Equal(t, "A3", f.seatedAt("Pool A-1st"))
	assert.Equal(t, "A4", f.seatedAt("Pool A-2nd"))
}

// A league with a circle owed beside a both-barred draw names the circle first,
// so POST /complete says what is pending (the barred pair stand at 1st and 2nd,
// the circle at 3rd to 5th): once the circle's lots are in, the barred draw is
// what is left, and once its own lots are in, the league is settled.
func TestIndividualLeague_CircleBesideABarredDrawIsNamedFirst(t *testing.T) {
	const compID = "ind-barred-pair-top-verdict"
	eng, store := barredDrawLeague(t, compID, true)

	assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID), "the circle is pending beside the barred draw")
	_, err := eng.OverridePoolRanks(compID, "Pool A", []RankOverride{{PlayerID: "cal-id", Rank: 3}, {PlayerID: "ayu-id", Rank: 4}, {PlayerID: "ben-id", Rank: 5}})
	require.NoError(t, err)
	assert.Equal(t, PoolsBarredDrawOpen, poolsVerdictNow(t, eng, store, compID), "the barred draw is what is left")
	_, err = eng.OverridePoolRanks(compID, "Pool A", []RankOverride{{PlayerID: "eli-id", Rank: 1}, {PlayerID: "dee-id", Rank: 2}})
	require.NoError(t, err)
	assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID))
}
