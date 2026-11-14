package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Engi pools and leagues follow the same tie rule as fought ones (operator
// ruling: "the tie rules are the same ... It's just like any other match").
// Two pairs are level on equal wins AND equal flags (engiStandingPoints); a
// level place that counts gets a tie-break bout scored by flags, which cannot
// end level (engiValidTotal), and a circle of three goes to another round or
// the lots. Name order never decides a place that counts.

// engiTBRows is the competition's tie-break ("-TB-") rows.
func engiTBRows(t *testing.T, store *state.Store, compID string) []state.MatchResult {
	t.Helper()
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var out []state.MatchResult
	for _, m := range all {
		if IsTiebreakerMatchID(m.ID) {
			out = append(out, m)
		}
	}
	return out
}

// recordEngiTB finishes a tie-break bout through the public write door, the
// pair with id winnerID taking it 2-1 on flags.
func recordEngiTB(t *testing.T, eng *Engine, compID string, m state.MatchResult, winnerID string) {
	t.Helper()
	result := &state.MatchResult{FlagsA: 1, FlagsB: 2, Status: state.MatchStatusCompleted}
	if m.SideAID == winnerID {
		result.FlagsA, result.FlagsB = 2, 1
	}
	_, err := eng.RecordMatchResultWithIneligibility(compID, m.ID, result)
	require.NoError(t, err)
}

// engiStandingByID is the pool's standing row of the pair with id.
func engiStandingByID(t *testing.T, rows []state.PlayerStanding, id string) state.PlayerStanding {
	t.Helper()
	for _, r := range rows {
		if r.Player.ID == id {
			return r
		}
	}
	t.Fatalf("no standing for %s", id)
	return state.PlayerStanding{}
}

func TestEngiLeague_TieAtFirstGetsABoutAndTheFlagWinnerTakesFirst(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-league-tie-first"
	ann, ben, cas, dee := engiPlayer("Ann"), engiPlayer("Ben"), engiPlayer("Cas"), engiPlayer("Dee")
	setupEngiComp(t, store, compID, state.CompFormatLeague)
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{ann, ben, cas, dee}},
	}))
	// Ann and Ben: 2 wins and 6 flags each, level at 1st. Dee (1 win, 5
	// flags) is 3rd clear of Cas (1 win, 3 flags).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		engiBout("Pool A-0", ann, ben, 0, 3),
		engiBout("Pool A-1", ann, cas, 3, 0),
		engiBout("Pool A-2", ann, dee, 3, 0),
		engiBout("Pool A-3", ben, cas, 3, 0),
		engiBout("Pool A-4", ben, dee, 0, 3),
		engiBout("Pool A-5", cas, dee, 3, 2),
	}))

	st, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	assert.True(t, engiStandingByID(t, st["Pool A"], ann.ID).Tied, "a level 1st is a tie to break, never settled by name")
	assert.True(t, engiStandingByID(t, st["Pool A"], ben.ID).Tied)

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome)
	tb := engiTBRows(t, store, compID)
	require.Len(t, tb, 1, "one tie-break bout for the two pairs level at 1st")
	assert.ElementsMatch(t, []string{ann.ID, ben.ID}, []string{tb[0].SideAID, tb[0].SideBID})

	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "the league waits for its tie-break bout")

	// Ben takes the bout on flags, so Ben is 1st although Ann comes first by
	// name.
	recordEngiTB(t, eng, compID, tb[0], ben.ID)

	st, err = eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	rows := st["Pool A"]
	assert.Equal(t, 1, engiStandingByID(t, rows, ben.ID).Rank)
	assert.Equal(t, 2, engiStandingByID(t, rows, ann.ID).Rank)
	assert.Equal(t, 3, engiStandingByID(t, rows, dee.ID).Rank)
	assert.Equal(t, 4, engiStandingByID(t, rows, cas.ID).Rank)
	for _, r := range rows {
		assert.False(t, r.Tied, "the bout settled the tie: %s", r.Player.Name)
	}

	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)
}

func TestEngiLeague_TieAtFourthShares(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-league-tie-fourth"
	ari, bea, cy, dov, eve := engiPlayer("Ari"), engiPlayer("Bea"), engiPlayer("Cy"), engiPlayer("Dov"), engiPlayer("Eve")
	setupEngiComp(t, store, compID, state.CompFormatLeague)
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{ari, bea, cy, dov, eve}},
	}))
	// Ari 4 wins; Bea 2 wins 8 flags; Cy 2 wins 6 flags; Dov and Eve 1 win and
	// 3 flags each, level at 4th.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		engiBout("Pool A-0", ari, bea, 3, 0),
		engiBout("Pool A-1", ari, cy, 3, 0),
		engiBout("Pool A-2", ari, dov, 3, 0),
		engiBout("Pool A-3", ari, eve, 3, 0),
		engiBout("Pool A-4", bea, cy, 2, 3),
		engiBout("Pool A-5", bea, dov, 3, 0),
		engiBout("Pool A-6", bea, eve, 3, 0),
		engiBout("Pool A-7", cy, dov, 3, 0),
		engiBout("Pool A-8", cy, eve, 0, 3),
		engiBout("Pool A-9", dov, eve, 3, 0),
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "a tie at 4th shares its place: no bout, the league finishes")
	assert.Empty(t, engiTBRows(t, store, compID))

	st, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	rows := st["Pool A"]
	assert.Equal(t, 1, engiStandingByID(t, rows, ari.ID).Rank)
	assert.Equal(t, 2, engiStandingByID(t, rows, bea.ID).Rank)
	assert.Equal(t, 3, engiStandingByID(t, rows, cy.ID).Rank)
	assert.Equal(t, 4, engiStandingByID(t, rows, dov.ID).Rank, "the pairs level at 4th share it")
	assert.Equal(t, 4, engiStandingByID(t, rows, eve.ID).Rank, "the pairs level at 4th share it")
}

func TestEngiPool_TieThatDecidesAdvancementGetsABout(t *testing.T) {
	ann, ben, cas := engiPlayer("Ann"), engiPlayer("Ben"), engiPlayer("Cas")
	// A circle with flags: Ann 1 win 5 flags is 1st; Ben and Cas 1 win and 3
	// flags each, level at 2nd.
	bouts := []state.MatchResult{
		engiBout("Pool A-0", ann, ben, 3, 0),
		engiBout("Pool A-1", ben, cas, 3, 0),
		engiBout("Pool A-2", cas, ann, 3, 2),
	}
	setup := func(t *testing.T, compID string, winners int) (*Engine, *state.Store) {
		t.Helper()
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "Engi Pools", Kind: "individual", Format: state.CompFormatMixed,
			PoolSize: 3, PoolSizeMode: "min", PoolWinners: winners, RoundRobin: true,
			Courts: []string{"A"}, StartTime: "09:00", Status: state.CompStatusPools, Engi: true,
		}))
		require.NoError(t, store.SavePools(compID, []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{ann, ben, cas}},
		}))
		require.NoError(t, store.SavePoolMatches(compID, bouts))
		return eng, store
	}

	t.Run("2nd goes through: the tie gets a bout", func(t *testing.T) {
		eng, store := setup(t, "engi-pool-tie-advances", 2)
		injected, err := eng.InjectTiebreakerMatches("engi-pool-tie-advances")
		require.NoError(t, err)
		require.Len(t, injected, 1)
		assert.ElementsMatch(t, []string{ben.ID, cas.ID}, []string{injected[0].SideAID, injected[0].SideBID})

		recordEngiTB(t, eng, "engi-pool-tie-advances", engiTBRows(t, store, "engi-pool-tie-advances")[0], cas.ID)
		st, err := eng.CalculatePoolStandings("engi-pool-tie-advances")
		require.NoError(t, err)
		rows := st["Pool A"]
		assert.Equal(t, 1, engiStandingByID(t, rows, ann.ID).Rank)
		assert.Equal(t, 2, engiStandingByID(t, rows, cas.ID).Rank, "the flag winner of the bout takes 2nd")
		assert.Equal(t, 3, engiStandingByID(t, rows, ben.ID).Rank)
	})

	t.Run("only 1st goes through: the tie at 2nd shares", func(t *testing.T) {
		eng, store := setup(t, "engi-pool-tie-out", 1)
		injected, err := eng.InjectTiebreakerMatches("engi-pool-tie-out")
		require.NoError(t, err)
		assert.Nil(t, injected, "a tie below the qualifying places decides nothing")
		assert.Empty(t, engiTBRows(t, store, "engi-pool-tie-out"))
	})
}

func TestEngiLeague_ThreeWayCircleGoesToAnotherRoundOrLots(t *testing.T) {
	ann, ben, cas := engiPlayer("Ann"), engiPlayer("Ben"), engiPlayer("Cas")
	// Each beats the next 3-2: 1 win and 5 flags each, a circle at 1st.
	beats := map[string]string{ann.ID: ben.ID, ben.ID: cas.ID, cas.ID: ann.ID}
	setup := func(t *testing.T, id string) (*Engine, *state.Store) {
		t.Helper()
		eng, store, _ := setupTestEngine(t)
		setupEngiComp(t, store, id, state.CompFormatLeague)
		require.NoError(t, store.SavePools(id, []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{ann, ben, cas}},
		}))
		require.NoError(t, store.SavePoolMatches(id, []state.MatchResult{
			engiBout("Pool A-0", ann, ben, 3, 2),
			engiBout("Pool A-1", ben, cas, 3, 2),
			engiBout("Pool A-2", cas, ann, 3, 2),
		}))
		outcome, err := eng.MaybeAutoCompletePools(id)
		require.NoError(t, err)
		require.Equal(t, AutoCompleteTiebreakInjected, outcome)
		tb := engiTBRows(t, store, id)
		require.Len(t, tb, 3, "a bout for every pair of the circle")
		// The tie-break round goes in the same circle.
		for _, m := range tb {
			winner := m.SideBID
			if beats[m.SideAID] == m.SideBID {
				winner = m.SideAID
			}
			recordEngiTB(t, eng, id, m, winner)
		}
		outcome, err = eng.MaybeAutoCompletePools(id)
		require.NoError(t, err)
		require.Equal(t, AutoCompleteNoChange, outcome, "a circle the round left level holds the league open")

		report, err := eng.ChusenStatus(id)
		require.NoError(t, err)
		require.Len(t, report.Pending, 1, "the lots are offered for the circle")
		require.Len(t, report.Pending[0].StillLevel, 1, "and another round among the three")
		assert.Len(t, report.Pending[0].StillLevel[0], 3)
		return eng, store
	}

	t.Run("another round", func(t *testing.T) {
		eng, _ := setup(t, "engi-circle-round")
		added, err := eng.RunTiebreakerRound("engi-circle-round", "Pool A", []string{ann.ID, ben.ID, cas.ID})
		require.NoError(t, err, "an engi circle gets another round like any other format's")
		assert.Len(t, added, 3)
	})

	t.Run("the lots", func(t *testing.T) {
		eng, store := setup(t, "engi-circle-lots")
		_, err := eng.OverridePoolRanks("engi-circle-lots", "Pool A", []RankOverride{
			{PlayerID: cas.ID, Rank: 1}, {PlayerID: ann.ID, Rank: 2}, {PlayerID: ben.ID, Rank: 3},
		})
		require.NoError(t, err)
		outcome, err := eng.MaybeAutoCompletePools("engi-circle-lots")
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteTransitioned, outcome)
		st, err := eng.CalculatePoolStandings("engi-circle-lots")
		require.NoError(t, err)
		rows := st["Pool A"]
		assert.Equal(t, 1, engiStandingByID(t, rows, cas.ID).Rank)
		assert.Equal(t, 2, engiStandingByID(t, rows, ann.ID).Rank)
		assert.Equal(t, 3, engiStandingByID(t, rows, ben.ID).Rank)
		comp, err := store.LoadCompetition("engi-circle-lots")
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusComplete, comp.Status)
	})
}

// TestEngiHeal_KeepsTheBoutTheTieRuleHolds pins the narrowed self-heal: a
// winnerless TB row is removed only when the tie rule does not hold it
// (orphanedEngiTBRow). A pending bout for the pairs level at 1st stays; one an
// earlier engine left between two pairs a result already ordered goes.
func TestEngiHeal_KeepsTheBoutTheTieRuleHolds(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	id := "engi-heal-narrow"
	ann, ben, cas, dee := engiPlayer("Ann"), engiPlayer("Ben"), engiPlayer("Cas"), engiPlayer("Dee")
	setupEngiComp(t, store, id, state.CompFormatLeague)
	require.NoError(t, store.SavePools(id, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{ann, ben, cas, dee}},
	}))
	tb := func(rowID string, a, b helper.Player) state.MatchResult {
		return state.MatchResult{ID: rowID, SideA: a.Name, SideB: b.Name, SideAID: a.ID, SideBID: b.ID, Status: state.MatchStatusScheduled, Court: "A"}
	}
	require.NoError(t, store.SavePoolMatches(id, []state.MatchResult{
		engiBout("Pool A-0", ann, ben, 0, 3),
		engiBout("Pool A-1", ann, cas, 3, 0),
		engiBout("Pool A-2", ann, dee, 3, 0),
		engiBout("Pool A-3", ben, cas, 3, 0),
		engiBout("Pool A-4", ben, dee, 0, 3),
		engiBout("Pool A-5", cas, dee, 3, 2),
		tb("Pool A-TB-0", ann, ben),
		tb("Pool A-TB-1", cas, dee),
	}))

	injected, err := eng.InjectTiebreakerMatches(id)
	require.NoError(t, err)
	assert.Nil(t, injected, "the level pairs already hold their bout")
	rows := engiTBRows(t, store, id)
	require.Len(t, rows, 1)
	assert.Equal(t, "Pool A-TB-0", rows[0].ID, "the bout for the tie at 1st is kept, the orphan removed")

	outcome, err := eng.MaybeAutoCompletePools(id)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "the league waits for the kept bout")
	assert.Len(t, engiTBRows(t, store, id), 1)
}

// TestEngiHeal_KeepsABoutBeingFought pins that the self-heal never deletes a row
// that is on a court: a tie-break bout whose pair a later correction made no
// longer level is kept while it runs (a kendo row in the same state is kept),
// and goes only while it is still scheduled with nothing scored.
func TestEngiHeal_KeepsABoutBeingFought(t *testing.T) {
	ann, ben, cas, dee := engiPlayer("Ann"), engiPlayer("Ben"), engiPlayer("Cas"), engiPlayer("Dee")
	tb := func(status state.MatchStatus, flagsA int) state.MatchResult {
		return state.MatchResult{ID: "Pool A-TB-1", SideA: cas.Name, SideB: dee.Name, SideAID: cas.ID, SideBID: dee.ID,
			Status: status, Court: "A", FlagsA: flagsA}
	}
	cases := []struct {
		name string
		row  state.MatchResult
		kept bool
	}{
		{"running", tb(state.MatchStatusRunning, 1), true},
		{"scheduled with a score", tb(state.MatchStatusScheduled, 1), true},
		{"scheduled, nothing scored", tb(state.MatchStatusScheduled, 0), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			id := "engi-heal-fought"
			setupEngiComp(t, store, id, state.CompFormatLeague)
			require.NoError(t, store.SavePools(id, []helper.Pool{
				{PoolName: "Pool A", Players: []helper.Player{ann, ben, cas, dee}},
			}))
			// Cas and Dee are not level: the tie rule holds only Ann and Ben.
			require.NoError(t, store.SavePoolMatches(id, []state.MatchResult{
				engiBout("Pool A-0", ann, ben, 0, 3),
				engiBout("Pool A-1", ann, cas, 3, 0),
				engiBout("Pool A-2", ann, dee, 3, 0),
				engiBout("Pool A-3", ben, cas, 3, 0),
				engiBout("Pool A-4", ben, dee, 0, 3),
				engiBout("Pool A-5", cas, dee, 3, 2),
				tc.row,
			}))

			stored, err := store.LoadPoolMatches(id)
			require.NoError(t, err)
			owed, err := eng.engiHealOwed(id, loadComp(t, store, id), stored)
			require.NoError(t, err)
			assert.Equal(t, !tc.kept, owed)
			_, err = eng.InjectTiebreakerMatches(id)
			require.NoError(t, err)
			var ids []string
			for _, m := range engiTBRows(t, store, id) {
				ids = append(ids, m.ID)
			}
			if tc.kept {
				assert.Contains(t, ids, "Pool A-TB-1", "a bout on a court is never deleted")
			} else {
				assert.NotContains(t, ids, "Pool A-TB-1")
			}
		})
	}
}

// TestEngiLeague_FinishedCorrectionThatMakesATieReopens pins that a finished
// engi league is reconciled like any other (leagueStateBeforeWrite): a
// correction that leaves a level place that counts puts the league back into
// match play, so the tie is broken.
func TestEngiLeague_FinishedCorrectionThatMakesATieReopens(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	id := "engi-league-reopen"
	ann, ben, cas := engiPlayer("Ann"), engiPlayer("Ben"), engiPlayer("Cas")
	setupEngiComp(t, store, id, state.CompFormatLeague)
	require.NoError(t, store.SavePools(id, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{ann, ben, cas}},
	}))
	require.NoError(t, store.SavePoolMatches(id, []state.MatchResult{
		engiBout("Pool A-0", ann, ben, 3, 2),
		engiBout("Pool A-1", ann, cas, 3, 2),
		engiBout("Pool A-2", ben, cas, 3, 2),
	}))
	outcome, err := eng.MaybeAutoCompletePools(id)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTransitioned, outcome)

	// Corrected: Cas took Ann's bout 3-2, so each pair has 1 win and 5 flags.
	_, err = eng.RecordMatchResultWithIneligibility(id, "Pool A-1", &state.MatchResult{FlagsA: 2, FlagsB: 3, Status: state.MatchStatusCompleted})
	require.NoError(t, err)

	comp, err := store.LoadCompetition(id)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "the circle at 1st puts the league back into match play")
	outcome, err = eng.MaybeAutoCompletePools(id)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteStarted, outcome, "the door's pass reports the reopen")
	assert.Len(t, engiTBRows(t, store, id), 3, "and the circle at 1st gets its tie-break bouts")
}
