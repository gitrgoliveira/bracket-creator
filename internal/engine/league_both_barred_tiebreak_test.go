package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// barredTiebreakLeague is an individual league whose top two, Ayu and Ben, drew
// each other and each beat Cal, so an ippon-shobu bout between them is owed. It
// returns the engine, the store and that bout's id once the scan has added it.
func barredTiebreakLeague(t *testing.T, compID string) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	players := []helper.Player{
		{ID: "ayu-id", Name: "Ayu", Dojo: "Dojo A"},
		{ID: "ben-id", Name: "Ben", Dojo: "Dojo B"},
		{ID: "cal-id", Name: "Cal", Dojo: "Dojo C"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ayu", SideAID: "ayu-id", SideB: "Ben", SideBID: "ben-id", Court: "A",
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
		{ID: "Pool A-1", SideA: "Ayu", SideAID: "ayu-id", SideB: "Cal", SideBID: "cal-id", Court: "A",
			Status: state.MatchStatusCompleted, Winner: "Ayu", WinnerID: "ayu-id", IpponsA: []string{"M"}},
		{ID: "Pool A-2", SideA: "Ben", SideAID: "ben-id", SideB: "Cal", SideBID: "cal-id", Court: "A",
			Status: state.MatchStatusCompleted, Winner: "Ben", WinnerID: "ben-id", IpponsA: []string{"M"}},
	}))
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome, "the tie owes its ippon-shobu bout")
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range all {
		if IsTiebreakerMatchID(m.ID) {
			return eng, store, m.ID
		}
	}
	t.Fatal("no tie-break bout was added")
	return nil, nil, ""
}

func barCompetitor(t *testing.T, store *state.Store, compID, id string, barred bool) {
	t.Helper()
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: id, Eligible: !barred, Reason: "kiken-voluntary: withdrew", MatchID: "Pool A-1", RecordedAt: time.Now(),
	}))
}

// drawnByDecision records a supplementary bout as the draw "Record as drawn
// (neither can fight)" writes for it through the decision door.
func drawnByDecision(t *testing.T, eng *Engine, compID, matchID string) {
	t.Helper()
	_, err := eng.RecordMatchResultWithIneligibility(compID, matchID, &state.MatchResult{
		Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
		Changed: []string{state.GroupResult}, WriteDoor: DoorDecision,
	})
	require.NoError(t, err, "the draw nobody can fight is not refused")
}

// enterLots records the lots over the one pending group in the order given (by
// participant id) through the chusen door, and returns the league's standings.
func enterLots(t *testing.T, eng *Engine, compID string, order []string) []state.PlayerStanding {
	t.Helper()
	ranks := make([]RankOverride, len(order))
	for i, id := range order {
		ranks[i] = RankOverride{PlayerID: id, Rank: i + 1}
	}
	_, err := eng.OverridePoolRanks(compID, "Pool A", ranks)
	require.NoError(t, err)
	standings, err := eng.LeagueStandings(compID)
	require.NoError(t, err)
	return standings
}

// assertDistinctRanks fails when two rows of the table share a place.
func assertDistinctRanks(t *testing.T, standings []state.PlayerStanding) {
	t.Helper()
	seen := map[int]string{}
	for _, s := range standings {
		other, dup := seen[s.Rank]
		assert.False(t, dup, "%s and %s share rank %d", s.Player.Name, other, s.Rank)
		seen[s.Rank] = s.Player.Name
		assert.False(t, s.Tied, "%s is no longer marked tied", s.Player.Name)
	}
}

// An individual league's ippon-shobu bout for 1st between two barred
// competitors is recorded as the draw nobody fought. That never shares 1st: it
// counts as played while both are barred, the lots are then offered (with no
// further round to fight), and once the organiser has entered the order the lots
// gave, the league finishes with every place distinct. A side reinstated
// afterwards changes nothing: the app never overturns a recorded lots result.
func TestIndividualLeague_BothBarredTiebreakDrawOwesTheLots(t *testing.T) {
	const compID = "ind-barred-tb"
	eng, store, tbID := barredTiebreakLeague(t, compID)
	drawnByDecision(t, eng, compID, tbID)

	barCompetitor(t, store, compID, "ayu-id", true)
	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, eng, store, compID), "one side still able to fight: the draw settles nothing")

	barCompetitor(t, store, compID, "ben-id", true)
	assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID), "both sides barred: the lots decide 1st")
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.NotEqual(t, AutoCompleteTransitioned, outcome, "1st is never shared")

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the lots are offered")
	assert.Equal(t, 1, report.Pending[0].MinPosition)
	assert.Nil(t, report.Pending[0].StillLevel, "nobody can fight another round")
	assert.ElementsMatch(t, []string{"Ayu", "Ben"}, chusenTeamNames(report.Pending[0]))

	standings := enterLots(t, eng, compID, []string{"ben-id", "ayu-id"})
	assertDistinctRanks(t, standings)
	assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID))
	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "the league finishes once the lots are entered")
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, compID).Status)

	barCompetitor(t, store, compID, "ben-id", false)
	assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID), "a side reinstated does not undo the lots the organiser entered")
}

// A team league's representative bout for 1st between two barred teams is
// recorded as the draw nobody fought, as an individual league's ippon-shobu
// bout is: the lots are offered and, once entered, finish the league with every
// place distinct.
func TestTeamLeague_BothBarredRepresentativeBoutOwesTheLots(t *testing.T) {
	const compID = "team-barred-dh"
	eng, store := setupTeamLeagueComp(t, compID, "topTie")
	alpha := leagueTeamID(t, store, compID, "Alpha")
	beta := leagueTeamID(t, store, compID, "Beta")
	injected, err := eng.GenerateLeagueTiebreakMatches(compID, []string{alpha, beta})
	require.NoError(t, err)
	require.Len(t, injected, 1)

	barCompetitor(t, store, compID, alpha, true)
	barCompetitor(t, store, compID, beta, true)
	drawnByDecision(t, eng, compID, injected[0].ID)

	assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID), "both teams barred: the lots decide 1st")
	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the lots are offered")
	assert.Equal(t, 1, report.Pending[0].MinPosition)
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, chusenTeamNames(report.Pending[0]))

	standings := enterLots(t, eng, compID, []string{beta, alpha})
	assertDistinctRanks(t, standings)
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "the league finishes once the lots are entered")
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, compID).Status)
	assert.Equal(t, "Beta", standings[0].Player.Name, "the lots put Beta 1st")
}

// An ordinary draw on the same bout stays refused, bars or not.
func TestIndividualLeague_PlainTiebreakDrawStaysRefused(t *testing.T) {
	const compID = "ind-plain-tb"
	eng, store, tbID := barredTiebreakLeague(t, compID)
	barCompetitor(t, store, compID, "ayu-id", true)
	barCompetitor(t, store, compID, "ben-id", true)

	_, err := eng.RecordMatchResultWithIneligibility(compID, tbID, &state.MatchResult{
		Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
	})
	require.ErrorIs(t, err, ErrTiebreakNeedsWinner)
}

// A drawn tie-breaker between two barred sides, team or individual, is the
// draw nobody can fight.
func TestDrawnTiebreakOfBarredSides(t *testing.T) {
	statuses := map[string]domain.CompetitorStatus{
		"a": {PlayerID: "a", Eligible: false, Reason: "kiken-voluntary: x", MatchID: "Pool A-0"},
		"b": {PlayerID: "b", Eligible: false, Reason: "kiken-voluntary: x", MatchID: "Pool A-0"},
	}
	drawn := func(id string) *state.MatchResult {
		return &state.MatchResult{ID: id, SideAID: "a", SideBID: "b", Status: state.MatchStatusCompleted}
	}
	assert.True(t, drawnTiebreakOfBarredSides(drawn("Pool A-TB-0"), statuses))
	assert.True(t, drawnTiebreakOfBarredSides(drawn("Pool A-DH-0"), statuses), "a representative bout too")
	assert.False(t, drawnTiebreakOfBarredSides(drawn("Pool A-0"), statuses), "a regular match is not a tie-breaker")
	won := drawn("Pool A-TB-0")
	won.Winner = "a"
	assert.False(t, drawnTiebreakOfBarredSides(won, statuses), "a bout with a winner is not drawn")
	assert.False(t, drawnTiebreakOfBarredSides(drawn("Pool A-TB-0"), nil), "no statuses, nobody barred")
}

// finishedBarredTiebreakLeague is barredTiebreakLeague with its bout drawn,
// both sides barred, the lots entered and the league finished on them.
func finishedBarredTiebreakLeague(t *testing.T, compID string) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, tbID := barredTiebreakLeague(t, compID)
	drawnByDecision(t, eng, compID, tbID)
	barCompetitor(t, store, compID, "ayu-id", true)
	barCompetitor(t, store, compID, "ben-id", true)
	enterLots(t, eng, compID, []string{"ayu-id", "ben-id"})
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTransitioned, outcome)
	return eng, store, tbID
}

// A side reinstated after the league finished on the barred draw changes
// nothing, as for a team league: the lots were drawn outside the app and the
// organiser entered them, so the app does not reopen the league, the lots stay
// listed as recorded and the organiser can still correct them.
func TestIndividualLeague_ReinstatingABarredSideKeepsTheLotsInForce(t *testing.T) {
	const compID = "ind-barred-reinstate"
	eng, store, _ := finishedBarredTiebreakLeague(t, compID)

	barCompetitor(t, store, compID, "ben-id", false)
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.NotEqual(t, AutoCompleteStarted, outcome, "the league is not put back into play")
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, compID).Status)

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	require.Len(t, report.Recorded, 1, "the lots that decide 1st can still be corrected")
	assert.ElementsMatch(t, []string{"Ayu", "Ben"}, chusenTeamNames(report.Recorded[0]))
}

// lotsReplacedSentenceForAyuAndBen is what the organiser is told before the
// bout the lots settled is fought again: the place and the order they entered.
const lotsReplacedSentenceForAyuAndBen = "This replaces the lots you entered for 1st: Ayu 1st, Ben 2nd. The bout's result will decide the place."

// Fighting the drawn tie-breaker again is allowed once a side is reinstated, but
// only after a warning that the bout's result replaces the lots the organiser
// entered: without the confirmation nothing changes, with it the bout is open
// again and the lots stay stored.
func TestIndividualLeague_ReopeningTheBoutTheLotsSettledNeedsAConfirm(t *testing.T) {
	const compID = "ind-barred-reopen-confirm"
	eng, store, tbID := finishedBarredTiebreakLeague(t, compID)
	barCompetitor(t, store, compID, "ben-id", false)

	_, err := eng.ReopenMatch(compID, tbID, "fight it again")
	var replaced *LotsReplacedError
	require.ErrorAs(t, err, &replaced)
	assert.Equal(t, lotsReplacedSentenceForAyuAndBen, replaced.Error())
	assert.Equal(t, tbID, replaced.MatchID)
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, compID).Status, "the refused reopen changed nothing")

	_, err = eng.ReopenMatch(compID, tbID, "fight it again", ForceOptions{ConfirmLots: true})
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusScheduled, poolMatchStatus(t, store, compID, tbID),
		"Ayu is still barred, so the bout is scheduled (the console shows the barred notice), not running")
	overrides, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"id:ayu-id": 1, "id:ben-id": 2}, overrides.PoolRanks["Pool A"], "the lots stay stored")
}

// A fusensho on the drawn tie-breaker, after a side is reinstated, is warned about
// the same way. Confirmed, the bout's result decides 1st: the lots stop applying
// to it, though they stay stored, and the league stays finished on the result.
func TestIndividualLeague_DecisionOverTheLotsNeedsAConfirm(t *testing.T) {
	const compID = "ind-barred-fusensho-confirm"
	eng, store, tbID := finishedBarredTiebreakLeague(t, compID)
	barCompetitor(t, store, compID, "ben-id", false)

	_, _, err := eng.RecordDecision(compID, tbID, string(domain.DecisionFusensho), "shiro", "", nil)
	var replaced *LotsReplacedError
	require.ErrorAs(t, err, &replaced)
	assert.Equal(t, lotsReplacedSentenceForAyuAndBen, replaced.Error())
	assert.Empty(t, poolMatchWinner(t, store, compID, tbID), "the bout stays drawn")
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, compID).Status)

	_, _, err = eng.RecordDecisionWithOptions(compID, tbID, string(domain.DecisionFusensho), "shiro", "", nil, ForceOptions{ConfirmLots: true})
	require.NoError(t, err)
	assert.NotEmpty(t, poolMatchWinner(t, store, compID, tbID), "the bout's result decides the place")
	overrides, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, overrides.PoolRanks["Pool A"], "the lots stay stored")
}

// A score write that gives the drawn bout a winner is warned about by the same
// owner, and lands with the confirmation.
func TestIndividualLeague_ScoreOverTheLotsNeedsAConfirm(t *testing.T) {
	const compID = "ind-barred-score-confirm"
	eng, store, tbID := finishedBarredTiebreakLeague(t, compID)
	barCompetitor(t, store, compID, "ben-id", false)
	barCompetitor(t, store, compID, "ayu-id", false)
	fought := func() *state.MatchResult {
		return &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "Ben", WinnerID: "ben-id", IpponsB: []string{"M"},
			CorrectionReason: "fought it",
		}
	}

	_, err := eng.RecordMatchResultWithIneligibility(compID, tbID, fought())
	var replaced *LotsReplacedError
	require.ErrorAs(t, err, &replaced)
	assert.Empty(t, poolMatchWinner(t, store, compID, tbID))

	_, err = eng.RecordMatchResultWithIneligibility(compID, tbID, fought(), ForceOptions{ConfirmLots: true})
	require.NoError(t, err)
	assert.Equal(t, "Ben", poolMatchWinner(t, store, compID, tbID))
}

func poolMatchStatus(t *testing.T, store *state.Store, compID, matchID string) state.MatchStatus {
	t.Helper()
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range all {
		if m.ID == matchID {
			return m.Status
		}
	}
	t.Fatalf("no match %s", matchID)
	return ""
}

func poolMatchWinner(t *testing.T, store *state.Store, compID, matchID string) string {
	t.Helper()
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range all {
		if m.ID == matchID {
			return m.Winner
		}
	}
	t.Fatalf("no match %s", matchID)
	return ""
}

// A drawn tie-break bout reopened on a finished league (its row no longer
// completed) puts the league back into match play, as a regular match does.
func TestIndividualLeague_ReopenedTiebreakBoutReopensTheFinishedLeague(t *testing.T) {
	const compID = "ind-tb-reopen"
	eng, store, tbID := finishedBarredTiebreakLeague(t, compID)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range all {
		if all[i].ID == tbID {
			all[i].Status = state.MatchStatusScheduled
			all[i].Decision = ""
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteStarted, outcome)
	assert.Equal(t, state.CompStatusPools, loadComp(t, store, compID).Status)
}

// A pools + knockout competition keeps its rule: a representative bout between
// two barred teams, recorded as drawn, leaves the tie level, so the lots are
// still offered for it (daihyosenLeftTied), exactly as before the league rule
// changed, and no knockout is seeded from the pool until they are entered.
func TestMixedPool_BothBarredRepresentativeBoutStillOwesTheLots(t *testing.T) {
	const compID = "mixed-barred-dh"
	eng, store := setupTeamPoolComp(t, compID, true)
	asMixed(t, store, compID)
	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 3)
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		barCompetitor(t, store, compID, teamID(name), true)
	}
	for _, m := range injected {
		_, err := eng.RecordMatchResultWithIneligibility(compID, m.ID, &state.MatchResult{
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
			Changed: []string{state.GroupResult}, WriteDoor: DoorDecision,
		})
		require.NoError(t, err)
	}

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the lots are offered")
	assert.ElementsMatch(t, []string{"Alpha", "Beta", "Gamma"}, chusenTeamNames(report.Pending[0]))
	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, eng, store, compID),
		"the drawn round holds the pool until the lots are entered")
	for _, m := range injected {
		all, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for i := range all {
			if all[i].ID == m.ID {
				statuses, err := store.LoadCompetitorStatus(compID)
				require.NoError(t, err)
				assert.False(t, poolMatchSettled(&all[i], poolLots{statuses: statuses}), "%s is not played in pools + knockout", m.ID)
				assert.True(t, poolMatchSettled(&all[i], poolLots{statuses: statuses, league: true}), "%s would be in a league", m.ID)
			}
		}
	}
}

// barredPairLeague is an individual league of four players level on Points
// (every regular match drawn) whose tie-break round has been fought: each pair
// is won by the player placed better in place, and the two players placed
// equal (Ayu and Ben) drew their bout nobody could fight. Both are then barred.
// So Ayu and Ben are the only run the bouts left level, wherever place puts
// them.
func barredPairLeague(t *testing.T, compID string, jointThird bool, place map[string]int) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools, Courts: []string{"A"},
		LeagueTwoThirdPlaces: jointThird,
	}))
	players := []helper.Player{
		{ID: "ayu-id", Name: "Ayu", Dojo: "Dojo A"}, {ID: "ben-id", Name: "Ben", Dojo: "Dojo B"},
		{ID: "cal-id", Name: "Cal", Dojo: "Dojo C"}, {ID: "dee-id", Name: "Dee", Dojo: "Dojo D"},
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
	require.Equal(t, AutoCompleteTiebreakInjected, outcome, "four players level at 1st owe a round")

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var pairBout string
	for i := range all {
		m := &all[i]
		if !IsTiebreakerMatchID(m.ID) {
			continue
		}
		a, b := place[m.SideAID], place[m.SideBID]
		if a == b {
			pairBout = m.ID
			continue
		}
		m.Status = state.MatchStatusCompleted
		if a < b {
			m.Winner, m.WinnerID, m.IpponsA = m.SideA, m.SideAID, []string{"M"}
		} else {
			m.Winner, m.WinnerID, m.IpponsB = m.SideB, m.SideBID, []string{"M"}
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))
	require.NotEmpty(t, pairBout, "the equal pair has its bout")
	drawnByDecision(t, eng, compID, pairBout)
	barCompetitor(t, store, compID, "ayu-id", true)
	barCompetitor(t, store, compID, "ben-id", true)
	return eng, store
}

// A both-barred draw owes the lots only to the run the fought bouts left level:
// with Ayu and Ben level at 1st and Cal and Dee separated below them, the lots
// are Ayu's and Ben's alone, Cal keeps 3rd and Dee 4th, and lots typed over a
// player the bouts placed are refused.
func TestIndividualLeague_BarredDrawOwesLotsOnlyToItsRun(t *testing.T) {
	const compID = "ind-barred-run-top"
	eng, store := barredPairLeague(t, compID, false, map[string]int{"ayu-id": 0, "ben-id": 0, "cal-id": 1, "dee-id": 2})

	assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID))
	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1)
	assert.Equal(t, 1, report.Pending[0].MinPosition)
	assert.ElementsMatch(t, []string{"Ayu", "Ben"}, chusenTeamNames(report.Pending[0]), "the bouts placed Cal and Dee")
	assert.Nil(t, report.Pending[0].StillLevel)

	_, err = eng.OverridePoolRanks(compID, "Pool A", []RankOverride{{PlayerID: "cal-id", Rank: 1}, {PlayerID: "ayu-id", Rank: 2}, {PlayerID: "ben-id", Rank: 3}})
	require.ErrorIs(t, err, ErrRankOverrideNotLevel, "lots never overrule a place the bouts gave")

	standings := enterLots(t, eng, compID, []string{"ben-id", "ayu-id"})
	assertDistinctRanks(t, standings)
	var order []string
	for _, s := range standings {
		order = append(order, s.Player.Name)
	}
	assert.Equal(t, []string{"Ben", "Ayu", "Cal", "Dee"}, order)
	assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID))

	report, err = eng.ChusenStatus(compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	require.Len(t, report.Recorded, 1, "the recorded lots stay correctable")
	assert.ElementsMatch(t, []string{"Ayu", "Ben"}, chusenTeamNames(report.Recorded[0]))
}

// The barred pair level at 3rd and 4th shares 3rd where joint 3rd places are
// awarded (no lots), and owes the lots for 3rd, over the pair alone, where they
// are not.
func TestIndividualLeague_BarredDrawBelowThePodiumBand(t *testing.T) {
	place := map[string]int{"cal-id": 0, "dee-id": 1, "ayu-id": 2, "ben-id": 2}

	t.Run("joint 3rd places awarded: the pair shares 3rd", func(t *testing.T) {
		const compID = "ind-barred-run-joint"
		eng, store := barredPairLeague(t, compID, true, place)
		assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID), "no lots for a pair that shares 3rd")
		report, err := eng.ChusenStatus(compID)
		require.NoError(t, err)
		assert.Empty(t, report.Pending)
		standings, err := eng.LeagueStandings(compID)
		require.NoError(t, err)
		for _, s := range standings {
			if s.Player.Name == "Ayu" || s.Player.Name == "Ben" {
				assert.Equal(t, 3, s.Rank, s.Player.Name)
			}
		}
	})

	t.Run("one 3rd place: the lots are the pair's alone", func(t *testing.T) {
		const compID = "ind-barred-run-single"
		eng, store := barredPairLeague(t, compID, false, place)
		assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID))
		report, err := eng.ChusenStatus(compID)
		require.NoError(t, err)
		require.Len(t, report.Pending, 1)
		assert.Equal(t, 3, report.Pending[0].MinPosition)
		assert.ElementsMatch(t, []string{"Ayu", "Ben"}, chusenTeamNames(report.Pending[0]))
	})
}

// Lots that settled a team league's both-barred representative bout stay
// listed, and so correctable, when one of the two teams is reinstated: the bout
// stays settled on those lots (drawnSupplementaryRuledByChusen), so the league stays
// finished, and the place is still the lots' (decision F2).
func TestTeamLeague_ReinstatedBarredDrawKeepsItsLotsCorrectable(t *testing.T) {
	const compID = "team-barred-dh-reinstated"
	eng, store := setupTeamLeagueComp(t, compID, "topTie")
	alpha := leagueTeamID(t, store, compID, "Alpha")
	beta := leagueTeamID(t, store, compID, "Beta")
	injected, err := eng.GenerateLeagueTiebreakMatches(compID, []string{alpha, beta})
	require.NoError(t, err)
	require.Len(t, injected, 1)
	barCompetitor(t, store, compID, alpha, true)
	barCompetitor(t, store, compID, beta, true)
	drawnByDecision(t, eng, compID, injected[0].ID)
	enterLots(t, eng, compID, []string{beta, alpha})
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTransitioned, outcome)

	barCompetitor(t, store, compID, beta, false)
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, compID).Status, "the lots still rule the drawn bout")
	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	require.Len(t, report.Recorded, 1, "the lots that decide 1st can still be corrected")
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, chusenTeamNames(report.Recorded[0]))
	assert.Equal(t, []int{1, 2}, report.Recorded[0].Ranks, "Beta 1st by the lots, Alpha 2nd, in table order")
}
