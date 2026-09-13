package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadPoolMatch reloads a single pool-matches.csv row by id, straight from
// the store -- never trusting an in-memory return value alone -- for the
// invariant test below to assert against.
func loadPoolMatch(t *testing.T, store *state.Store, compID, matchID string) state.MatchResult {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		if m.ID == matchID {
			return m
		}
	}
	t.Fatalf("match %s not found in competition %s", matchID, compID)
	return state.MatchResult{}
}

// assertWinnerIDMatchesASide is the ONE assertion every subtest below drives
// its write path toward: a stored row with a recorded Winner must carry a
// WinnerID equal to one of its OWN side ids (SideAID or SideBID). This is
// the operator ruling bc-pnum's central invariant -- every id-only
// standings/scoring/eligibility reader downstream depends on it holding for
// every producer, not just the ones covered by other tests incidentally.
func assertWinnerIDMatchesASide(t *testing.T, m state.MatchResult) {
	t.Helper()
	require.NotEmpty(t, m.Winner, "sanity: the row must actually record a winner")
	require.NotEmpty(t, m.WinnerID, "match %s: Winner %q is recorded but WinnerID is empty", m.ID, m.Winner)
	require.NotEmpty(t, m.SideAID, "match %s: SideAID must be stamped for this invariant to mean anything", m.ID)
	require.NotEmpty(t, m.SideBID, "match %s: SideBID must be stamped for this invariant to mean anything", m.ID)
	assert.Truef(t, m.WinnerID == m.SideAID || m.WinnerID == m.SideBID,
		"match %s: WinnerID %q must equal SideAID %q or SideBID %q", m.ID, m.WinnerID, m.SideAID, m.SideBID)
}

// TestWinnerIDInvariant_EveryWritePathStampsASideID pins the operator ruling
// bc-pnum's central invariant end to end: every engine write path that stores
// a Winner on a pool-match row also stores a WinnerID equal to one of that
// row's own side ids (SideAID or SideBID). This is what makes the whole
// id-only standings/scoring/eligibility resolution correct -- if any producer
// ever wrote a Winner without a matching WinnerID, every id-only reader
// downstream would silently credit nobody, which is exactly the failure mode
// the ruling exists to prevent.
//
// Each subtest drives a DIFFERENT production write path through its real,
// public entry point on a store-backed engine, then reloads the persisted
// row from disk and checks the invariant against what actually landed.
func TestWinnerIDInvariant_EveryWritePathStampsASideID(t *testing.T) {
	t.Run("score write (RecordMatchResult, the applyPoolWrite/backfillMatchIdentity path)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "invariant-score-write"
		createTestCompetition(t, store, compID, "mixed", 2)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
		require.NoError(t, eng.StartCompetition(compID))

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.NotEmpty(t, matches)
		m := matches[0]

		// The client sends names + WinnerSide, never a WinnerID directly
		// (mirrors the real score editor's wire shape).
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, &state.MatchResult{
			ID: m.ID, SideA: m.SideA, SideB: m.SideB,
			Winner: m.SideA, WinnerSide: "A", Status: state.MatchStatusCompleted,
		}))

		assertWinnerIDMatchesASide(t, loadPoolMatch(t, store, compID, m.ID))
	})

	t.Run("RecordDecision / RecordDecisionTx (kiken)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "invariant-decision"
		createTestCompetition(t, store, compID, "league", 4)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
		require.NoError(t, eng.StartCompetition(compID))

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.NotEmpty(t, matches)
		m := matches[0]

		// decisionBy "aka" withdraws SideA; SideB survives as the winner.
		_, _, err = eng.RecordDecision(compID, m.ID, "kiken", "aka", "voluntary", nil, false)
		require.NoError(t, err)

		assertWinnerIDMatchesASide(t, loadPoolMatch(t, store, compID, m.ID))
	})

	t.Run("applyEngiToMatchResult (engi flag-scored dispatch seam)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "invariant-engi"
		createTestCompetition(t, store, compID, "mixed", 2, func(c *state.Competition) {
			c.Engi = true
		})
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
		require.NoError(t, eng.StartCompetition(compID))

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.NotEmpty(t, matches)
		m := matches[0]

		_, engErr := eng.RecordMatchResultWithIneligibility(compID, m.ID, &state.MatchResult{
			ID: m.ID, SideA: m.SideA, SideB: m.SideB,
			FlagsA: 3, FlagsB: 2,
		})
		require.NoError(t, engErr)

		assertWinnerIDMatchesASide(t, loadPoolMatch(t, store, compID, m.ID))
	})

	t.Run("Swiss bye (buildSwissMatches)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "invariant-swiss-bye"
		createTestCompetition(t, store, compID, "swiss", 0, func(c *state.Competition) {
			c.Format = state.CompFormatSwiss
			c.SwissRounds = 1
			c.Status = state.CompStatusSetup
		})
		// Odd number of participants forces a bye in round 1.
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})

		round1, err := eng.GenerateSwissRound(compID, 1)
		require.NoError(t, err)
		require.NoError(t, store.SavePoolMatches(compID, round1))

		var bye *state.MatchResult
		for i := range round1 {
			if round1[i].SideB == "" {
				bye = &round1[i]
			}
		}
		require.NotNil(t, bye, "an odd-sized round 1 must produce a bye")
		assert.Equal(t, state.MatchStatusCompleted, bye.Status, "a bye is auto-completed")

		got := loadPoolMatch(t, store, compID, bye.ID)
		require.NotEmpty(t, got.WinnerID, "a bye's WinnerID must be stamped")
		assert.Equal(t, got.SideAID, got.WinnerID, "a bye's sole side (SideA) is the winner")
	})

	t.Run("kachinuki completion (a kachinuki pool match scored to completion)", func(t *testing.T) {
		eng, store, comp := setupKachinukiComp(t, "invariant-kachinuki", 3)
		comp.Format = state.CompFormatMixed
		comp.Kind = "team"
		require.NoError(t, store.SaveCompetition(comp))
		aID, bID := "team-a-id", "team-b-id"
		require.NoError(t, store.SaveParticipants(comp.ID, []domain.Player{
			{ID: aID, Name: "Team A", Dojo: "Dojo A"},
			{ID: bID, Name: "Team B", Dojo: "Dojo B"},
		}))
		require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{
			{ID: "Pool A-0", SideA: "Team A", SideAID: aID, SideB: "Team B", SideBID: bID,
				Status: state.MatchStatusRunning, Court: "A"},
		}))

		// The encounter ends only on an explicit status:completed score write
		// (CLAUDE.md's kachinuki section); the client sends names, not ids.
		require.NoError(t, eng.RecordMatchResult(comp.ID, "Pool A-0", &state.MatchResult{
			ID: "Pool A-0", SideA: "Team A", SideB: "Team B",
			Winner: "Team A", WinnerSide: "A", Status: state.MatchStatusCompleted,
			Decision: "kachinuki-exhaustion",
		}))

		assertWinnerIDMatchesASide(t, loadPoolMatch(t, store, comp.ID, "Pool A-0"))
	})

	t.Run("tie-break row, once scored (InjectTiebreakerMatches then RecordMatchResult)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "invariant-tb"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "TB Invariant", Format: state.CompFormatMixed,
			Status: state.CompStatusPools, Courts: []string{"A"},
		}))
		require.NoError(t, store.SavePools(compID, []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{
				{ID: "alice-id", Name: "Alice", Dojo: "Dojo Alice"},
				{ID: "bob-id", Name: "Bob", Dojo: "Dojo Bob"},
				{ID: "charlie-id", Name: "Charlie", Dojo: "Dojo Charlie"},
			}},
		}))
		// Alice beats both; Bob and Charlie draw each other -> tied for 2nd/3rd.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "alice-id", SideB: "Bob", SideBID: "bob-id",
				Winner: "Alice", WinnerID: "alice-id", Status: state.MatchStatusCompleted},
			{ID: "Pool A-1", SideA: "Alice", SideAID: "alice-id", SideB: "Charlie", SideBID: "charlie-id",
				Winner: "Alice", WinnerID: "alice-id", Status: state.MatchStatusCompleted},
			{ID: "Pool A-2", SideA: "Bob", SideAID: "bob-id", SideB: "Charlie", SideBID: "charlie-id",
				Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
		}))

		injected, err := eng.InjectTiebreakerMatches(compID)
		require.NoError(t, err)
		require.Len(t, injected, 1, "one TB match expected for the Bob/Charlie tie")
		tb := injected[0]
		require.NotEmpty(t, tb.SideAID)
		require.NotEmpty(t, tb.SideBID)

		require.NoError(t, eng.RecordMatchResult(compID, tb.ID, &state.MatchResult{
			ID: tb.ID, SideA: tb.SideA, SideB: tb.SideB,
			Winner: tb.SideA, WinnerSide: "A", Status: state.MatchStatusCompleted,
		}))

		assertWinnerIDMatchesASide(t, loadPoolMatch(t, store, compID, tb.ID))
	})

	t.Run("daihyosen row, once scored (InjectPoolDaihyosenMatches then RecordMatchResult)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "invariant-dh"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "DH Invariant", Kind: "team", TeamSize: 3,
			Format: state.CompFormatMixed, Status: state.CompStatusPools, Courts: []string{"A"},
		}))
		require.NoError(t, store.SavePools(compID, []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{
				{ID: "team-a-id", Name: "Team A", Dojo: "Dojo A"},
				{ID: "team-b-id", Name: "Team B", Dojo: "Dojo B"},
			}},
		}))
		// A single team match, fully tied on all 8 criteria (W=L=T=IV=IL=IT=PW=PL=0
		// for both, since the only match between them is itself a draw).
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool A-0", SideA: "Team A", SideAID: "team-a-id", SideB: "Team B", SideBID: "team-b-id",
				Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
		}))

		injected, err := eng.InjectPoolDaihyosenMatches(compID)
		require.NoError(t, err)
		require.Len(t, injected, 1, "one DH match expected for the fully-tied pair")
		dh := injected[0]
		require.NotEmpty(t, dh.SideAID)
		require.NotEmpty(t, dh.SideBID)

		require.NoError(t, eng.RecordMatchResult(compID, dh.ID, &state.MatchResult{
			ID: dh.ID, SideA: dh.SideA, SideB: dh.SideB,
			Winner: dh.SideA, WinnerSide: "A", Status: state.MatchStatusCompleted,
		}))

		assertWinnerIDMatchesASide(t, loadPoolMatch(t, store, compID, dh.ID))
	})
}
