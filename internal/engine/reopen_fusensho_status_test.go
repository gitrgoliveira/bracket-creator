package engine

// bc-cse item 9: reopening a MATCH-LEVEL fusensho (the default win recorded
// for a barred competitor's remaining match, via POST /decision -- see
// domain.IsWithdrawalDecisionStr's doc) must not blindly return the match to
// RUNNING when the competitor it named as loser is STILL barred by an
// earlier, unrelated withdrawal: RUNNING would let every later write on
// this match be refused by StartMatchTx's eligibility gate, with no way for
// the operator to get out. It must go to SCHEDULED instead, which re-shows
// the barred-match notice, exactly as it did before the fusensho was
// recorded. Only once the earlier bar is itself cleared does the SAME
// reopen go to RUNNING.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fusenshoBarredFixture builds a pool competition where Alice is barred by
// a kiken-voluntary on "Pool A-0" (Alice vs Bob), then a match-level
// fusensho closes "Pool A-1" (Alice vs Carol) crediting Carol -- the shape
// an operator reaches by recording a default win for each of a barred
// competitor's remaining matches. Real participant ids throughout: BarredSides
// keys statuses by PlayerID, so a match row with no stamped SideAID/SideBID
// and no roster to resolve a name against would record no bar at all.
func fusenshoBarredFixture(t *testing.T, compID string) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Status: state.CompStatusPools,
	}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Status: state.MatchStatusScheduled},
	}))
	// Alice (aka/SideA) withdraws from Pool A-0; Bob credited.
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "aka", "no-show", nil, false)
	require.NoError(t, err)
	// Alice (aka/SideA) cannot fight Pool A-1 either; Carol credited by
	// match-level fusensho. Not a withdrawal decision, so this write bars
	// nobody itself (domain.IsWithdrawalDecisionStr excludes fusensho) --
	// Alice's only bar is still the one Pool A-0 recorded.
	_, _, err = eng.RecordDecision(compID, "Pool A-1", "fusensho", "aka", "already ineligible", nil, false)
	require.NoError(t, err)
	return eng, store
}

func TestReopenMatch_FusenshoMatch_StillBarred_GoesToScheduled(t *testing.T) {
	eng, store := fusenshoBarredFixture(t, "reopen-fusensho-barred")

	restored, err := eng.ReopenMatch("reopen-fusensho-barred", "Pool A-1", "operator error")
	require.NoError(t, err)
	assert.Nil(t, restored, "fusensho recorded no CompetitorStatus, so reopening it restores nobody's eligibility")

	matches, err := store.LoadPoolMatches("reopen-fusensho-barred")
	require.NoError(t, err)
	var m1 *state.MatchResult
	for i := range matches {
		if matches[i].ID == "Pool A-1" {
			m1 = &matches[i]
		}
	}
	require.NotNil(t, m1)
	assert.Equal(t, state.MatchStatusScheduled, m1.Status,
		"the loser is still barred by the unrelated Pool A-0 withdrawal; RUNNING would strand the operator behind StartMatchTx")
	assert.Empty(t, m1.Winner)
	assert.Empty(t, m1.Decision)
	assert.Empty(t, m1.IpponsA)
	assert.Empty(t, m1.IpponsB)

	statuses, err := store.LoadCompetitorStatus("reopen-fusensho-barred")
	require.NoError(t, err)
	for _, st := range statuses {
		if st.PlayerID != "" {
			assert.False(t, st.Eligible, "Alice's Pool A-0 bar must be untouched by reopening the unrelated fusensho match")
		}
	}
}

func TestReopenMatch_FusenshoMatch_NoLongerBarred_GoesToRunning(t *testing.T) {
	eng, store := fusenshoBarredFixture(t, "reopen-fusensho-clear")

	// Clear Alice's ONLY bar by reopening the withdrawal that recorded it;
	// restoreIfWithdrawalRemoved restores her eligibility in the same
	// transaction (operator ruling 2026-09-24).
	_, err := eng.ReopenMatch("reopen-fusensho-clear", "Pool A-0", "withdrawal recorded by mistake")
	require.NoError(t, err)

	statuses, err := store.LoadCompetitorStatus("reopen-fusensho-clear")
	require.NoError(t, err)
	for _, st := range statuses {
		require.True(t, st.Eligible, "Alice's only bar must already be cleared before this test's real assertion")
	}

	restored, err := eng.ReopenMatch("reopen-fusensho-clear", "Pool A-1", "operator error")
	require.NoError(t, err)
	assert.Nil(t, restored, "fusensho still restores nobody's eligibility, whichever status it goes to")

	matches, err := store.LoadPoolMatches("reopen-fusensho-clear")
	require.NoError(t, err)
	var m1 *state.MatchResult
	for i := range matches {
		if matches[i].ID == "Pool A-1" {
			m1 = &matches[i]
		}
	}
	require.NotNil(t, m1)
	assert.Equal(t, state.MatchStatusRunning, m1.Status,
		"Alice is no longer barred by anything, so this reopen behaves like any other")
}

// bc-kfup: a FUSENPAI chained onto an earlier bar (alreadyBarredRefusal)
// records the default loss and no CompetitorStatus of its own, the same
// shape as the match-level fusensho above. Reopening it restores nobody, so
// while the earlier bar holds it must go to SCHEDULED, not RUNNING.
func TestReopenMatch_ChainedFusenpai_StillBarred_GoesToScheduled(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "reopen-chained-fusenpai"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: compID, Status: state.CompStatusPools}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Status: state.MatchStatusScheduled},
	}))
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "aka", "withdrew", nil, false)
	require.NoError(t, err)
	_, _, err = eng.RecordDecision(compID, "Pool A-1", "fusenpai", "aka", "did not appear", nil, false)
	require.NoError(t, err)

	_, err = eng.ReopenMatch(compID, "Pool A-1", "")
	require.NoError(t, err)

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		if m.ID == "Pool A-1" {
			assert.Equal(t, state.MatchStatusScheduled, m.Status,
				"Alice is still barred by Pool A-0; RUNNING would strand the operator behind StartMatchTx")
			assert.Empty(t, m.Decision)
		}
	}
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.False(t, statuses[aliceID].Eligible)
	assert.Equal(t, "Pool A-0", statuses[aliceID].MatchID, "the reopen of the chained match leaves the Pool A-0 bar as it was")
}

// An ordinary fusenpai (the loser's own bar, recorded by THIS match) still
// reopens to RUNNING: BarredSides ignores a status recorded by the match
// being checked, and the reopen restores it.
func TestReopenMatch_OrdinaryFusenpai_GoesToRunning(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "reopen-ordinary-fusenpai"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: compID, Status: state.CompStatusPools}))
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
	}))
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "fusenpai", "aka", "did not appear", nil, false)
	require.NoError(t, err)

	_, err = eng.ReopenMatch(compID, "Pool A-0", "")
	require.NoError(t, err)

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status)
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	if st, ok := statuses[aliceID]; ok {
		assert.True(t, st.Eligible, "the reopen restores the competitor this match barred")
	}
}
