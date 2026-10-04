package engine

// bc-mrgc phase 3: the read-modify-write windows that lost a match write.
// Each test lands a real store write at the seam where the writer has read
// what it will save back (Engine.afterMatchRead). On the old code that seam
// sat between two separate lock acquisitions (or before an unchecked locked
// write), so the landed write was overwritten or contradicted. Now the read
// and the write are one transaction (the landed write waits for it and goes
// on top), or the locked write re-checks what it read and stands down.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
)

// landWriteAtTheSeam arms the engine's read seam to land write once, from
// another goroutine, the way a second device's score lands: it waits for the
// write for a moment, which it can only finish when nothing holds the
// competition's lock. The returned wait blocks until the write has landed.
func landWriteAtTheSeam(t *testing.T, eng *Engine, compID string, write func() error) (wait func()) {
	t.Helper()
	done := make(chan struct{})
	fired := false
	eng.afterMatchRead = func(id string) {
		if fired || id != compID {
			return
		}
		fired = true
		go func() {
			defer close(done)
			assert.NoError(t, write())
		}()
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
		}
	}
	return func() {
		t.Helper()
		require.True(t, fired, "the seam was reached")
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the landed write never finished")
		}
		eng.afterMatchRead = nil
	}
}

func scorePoolMatch(store *state.Store, compID, matchID string, ippons ...string) func() error {
	return func() error {
		_, err := store.UpdatePoolMatchByID(compID, matchID, func(m *state.MatchResult) error {
			m.IpponsA = ippons
			return nil
		})
		return err
	}
}

func storedPoolMatch(t *testing.T, store *state.Store, compID, matchID string) state.MatchResult {
	t.Helper()
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == matchID {
			return m
		}
	}
	t.Fatalf("match %s not found", matchID)
	return state.MatchResult{}
}

func TestLostUpdate_TiebreakerInjectionKeepsALandedScore(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "lu-tiebreak"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "LU", Format: state.CompFormatMixed, Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	players := []helper.Player{{Name: "Alice", Dojo: "DA"}, {Name: "Bob", Dojo: "DB"}, {Name: "Charlie", Dojo: "DC"}}
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A"},
		{ID: "Pool A-2", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake), Court: "A"},
	}
	bctest.StampIDs(players, matches)
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, matches))

	wait := landWriteAtTheSeam(t, eng, compID, scorePoolMatch(store, compID, "Pool A-0", "M"))
	injected, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	wait()
	require.Len(t, injected, 1)
	assert.Equal(t, []string{"M"}, storedPoolMatch(t, store, compID, "Pool A-0").IpponsA, "the score that landed during the injection is kept")
	storedPoolMatch(t, store, compID, injected[0].ID)
}

func TestLostUpdate_DaihyosenInjectionKeepsALandedScore(t *testing.T) {
	compID := "lu-dh"
	eng, store := setupTeamPoolWinners(t, compID, []string{"Alpha", "Beta", "Gamma", "Delta"}, 4, fourTeamOneTiedPair())

	wait := landWriteAtTheSeam(t, eng, compID, scorePoolMatch(store, compID, "Pool A-0", "M"))
	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	wait()
	require.Len(t, injected, 1)
	assert.Equal(t, []string{"M"}, storedPoolMatch(t, store, compID, "Pool A-0").IpponsA, "the score that landed during the injection is kept")
	storedPoolMatch(t, store, compID, injected[0].ID)
}

func TestLostUpdate_LeagueTiebreakKeepsALandedScore(t *testing.T) {
	compID := "lu-league"
	eng, store := setupTwoTiedGroupLeague(t, compID)
	teamIDs := teamIDsByName(t, store, compID, []string{"Alpha", "Beta"})

	wait := landWriteAtTheSeam(t, eng, compID, scorePoolMatch(store, compID, "Pool A-1", "M"))
	injected, err := eng.GenerateLeagueTiebreakMatches(compID, teamIDs)
	require.NoError(t, err)
	wait()
	require.NotEmpty(t, injected)
	assert.Equal(t, []string{"M"}, storedPoolMatch(t, store, compID, "Pool A-1").IpponsA, "the score that landed during the generation is kept")
	storedPoolMatch(t, store, compID, injected[0].ID)
}

func TestLostUpdate_SwissAppendKeepsALandedCorrection(t *testing.T) {
	eng, store, compID, _ := setupSwissCompetition(t, []string{"A", "B", "C", "D"}, nil, 3)
	ms, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	require.NoError(t, store.SavePoolMatches(compID, ms))
	_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
		c.SwissCurrentRound = 1
		return c, nil
	})
	require.NoError(t, err)
	for _, m := range ms {
		completeSwissMatch(t, store, compID, m.ID, m.SideA)
	}

	wait := landWriteAtTheSeam(t, eng, compID, scorePoolMatch(store, compID, ms[0].ID, "M", "K"))
	newMatches, _, err := eng.AdvanceSwissRound(compID)
	require.NoError(t, err)
	wait()
	require.NotEmpty(t, newMatches)
	assert.Equal(t, []string{"M", "K"}, storedPoolMatch(t, store, compID, ms[0].ID).IpponsA, "the correction that landed during the append is kept")
	storedPoolMatch(t, store, compID, newMatches[0].ID)
}

// The kachinuki advance read the match without the lock and its locked write
// set the match running and cleared its verdict without looking again. A
// finish landing in between was reopened by it. Now the locked write
// re-checks and reads again, and a finish that landed is final on that read
// too, so nothing is appended over it.
func TestLostUpdate_KachinukiAdvanceDoesNotReopenAFinishThatLanded(t *testing.T) {
	finish := func(m *state.MatchResult) {
		m.Status = state.MatchStatusCompleted
		m.Winner = "RedTeam"
		m.Decision = string(domain.DecisionKachinukiExhaustion)
		m.ModifiedAt = time.Now().UnixMilli()
	}
	subs := func() []state.SubMatchResult {
		return []state.SubMatchResult{
			{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought"},
			{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "A-Jiho", Decision: "fought"},
		}
	}
	t.Run("pool", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "lu-kachinuki-pool"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 5}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning, SubResults: subs(),
		}}))
		wait := landWriteAtTheSeam(t, eng, compID, func() error {
			_, err := store.UpdatePoolMatchByID(compID, "P1-0", func(m *state.MatchResult) error { finish(m); return nil })
			return err
		})
		advanced, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
		require.NoError(t, err)
		wait()
		assert.False(t, advanced, "the advance stands down")
		m := storedPoolMatch(t, store, compID, "P1-0")
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "the finish that landed is not reopened")
		assert.Equal(t, "RedTeam", m.Winner)
		assert.Len(t, m.SubResults, 2, "no bout appended over it")
	})
	t.Run("knockout", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "lu-kachinuki-ko"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 5}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
			ID: "B1", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning, SubResults: subs(),
		}}}}))
		wait := landWriteAtTheSeam(t, eng, compID, func() error {
			_, err := store.UpdateBracketMatchByID(compID, "B1", func(bm *state.BracketMatch) {
				bm.Status = state.MatchStatusCompleted
				bm.Winner = "RedTeam"
				bm.Decision = string(domain.DecisionKachinukiExhaustion)
				bm.ModifiedAt = time.Now().UnixMilli()
			})
			return err
		})
		advanced, _, err := eng.MaybeAdvanceKachinuki(compID, "B1")
		require.NoError(t, err)
		wait()
		assert.False(t, advanced, "the advance stands down")
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		bm := b.MatchByID("B1")
		require.NotNil(t, bm)
		assert.Equal(t, state.MatchStatusCompleted, bm.Status, "the finish that landed is not reopened")
		assert.Equal(t, "RedTeam", bm.Winner)
		assert.Len(t, bm.SubResults, 2, "no bout appended over it")
	})
}

// A correction landing between the advance's read and its locked write (the
// other device fixing bout 1's winner) trips the re-check. The advance then
// reads the match again and works the pairing out from the corrected bout,
// rather than standing down with nothing appended and the operator's Record
// bout answered with no next pairing.
func TestLostUpdate_KachinukiAdvanceReadsAgainAfterALandedCorrection(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "lu-kachinuki-reread", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	redID, whiteID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(comp.ID, []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "D"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "D"},
	}))
	lineup := func(teamID string, names ...string) map[string]string {
		ids := map[string]string{}
		positions := map[domain.Position]string{}
		memberIDs := map[domain.Position]string{}
		for i, name := range names {
			m, err := store.AddTeamMember(comp.ID, teamID, name)
			require.NoError(t, err)
			ids[name] = m.ID
			positions[domain.PositionNumbered(i+1)] = name
			memberIDs[domain.PositionNumbered(i+1)] = m.ID
		}
		require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
			TeamID: teamID, Round: 0, Positions: positions, MemberIDs: memberIDs,
		}, 2))
		return ids
	}
	red := lineup(redID, "R1", "R2")
	white := lineup(whiteID, "W1", "W2")
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "P1-0", SideA: "RedTeam", SideAID: redID, SideB: "WhiteTeam", SideBID: whiteID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{
			Position: 1, SideA: "R1", SideAMemberID: red["R1"], SideB: "W1", SideBMemberID: white["W1"],
			Winner: "W1", Decision: "fought",
		}},
	}}))

	// The other device corrects bout 1 while the advance works from the
	// uncorrected read: R1 won it, and stays on. The winner's member id goes
	// with the name, as every writer re-derives it (ids decide who stayed
	// on, bc-pnum).
	wait := landWriteAtTheSeam(t, eng, comp.ID, func() error {
		_, err := store.UpdatePoolMatchByID(comp.ID, "P1-0", func(m *state.MatchResult) error {
			m.SubResults[0].Winner, m.SubResults[0].WinnerMemberID = "R1", red["R1"]
			m.ModifiedAt = time.Now().UnixMilli()
			return nil
		})
		return err
	})
	advanced, post, err := eng.MaybeAdvanceKachinuki(comp.ID, "P1-0")
	require.NoError(t, err)
	wait()
	require.True(t, advanced, "the advance reads the corrected match again rather than standing down")
	require.Len(t, post.BoutLog, 2)
	assert.Equal(t, "R1", post.BoutLog[1].SideA, "the corrected winner stays on")
	assert.Equal(t, "W2", post.BoutLog[1].SideB, "against White's next fighter")
	m := storedPoolMatch(t, store, comp.ID, "P1-0")
	assert.Equal(t, "R1", m.SubResults[0].Winner, "the landed correction is kept")
	assert.Equal(t, post.BoutLog, m.SubResults, "the echo is the stored log")
	assert.Equal(t, m.ModifiedAt, post.ModifiedAt, "the echo carries the stamp the advance left")
}
