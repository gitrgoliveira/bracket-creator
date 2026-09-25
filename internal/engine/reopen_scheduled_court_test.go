package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A reopen that goes back to the QUEUE takes no court, so a bout running on
// that court must not refuse it. The one such reopen is a match-level
// fusensho whose barred competitor is still barred (reopenTargetStatus). It
// used to be refused court_busy whenever anything else was being fought on its
// court, and the remedy the operator was then offered (requeue the court's
// occupant) wiped the live bout to free a court the reopen never needed.

// busyCourtFusenshoFixture builds a pool competition on court A where Alice
// withdrew injured from "Pool A-0" and a match-level fusensho then closed
// "Pool A-1" (Alice vs Carol) for Carol. reinstate clears Alice before the
// test reopens. The court is then made busy: by "Pool A-2" of the same
// competition (crossComp false), or by a match of a second competition sharing
// the court (crossComp true).
func busyCourtFusenshoFixture(t *testing.T, compID string, reinstate, crossComp bool) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	daveID, erinID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
		{ID: daveID, Name: "Dave", Dojo: "D"},
		{ID: erinID, Name: "Erin", Dojo: "E"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Court: "A", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Court: "A", Status: state.MatchStatusScheduled},
		{ID: "Pool A-2", SideA: "Dave", SideB: "Erin", SideAID: daveID, SideBID: erinID, Court: "A", Status: state.MatchStatusScheduled},
	}))
	_, st, err := eng.RecordDecision(compID, "Pool A-0", "kiken-injury", "aka", "injured", nil, false)
	require.NoError(t, err)
	require.Equal(t, aliceID, st.PlayerID)
	_, _, err = eng.RecordDecision(compID, "Pool A-1", "fusensho", "aka", "already ineligible", nil, false)
	require.NoError(t, err)
	if reinstate {
		_, err = eng.ReinstateCompetitor(compID, aliceID)
		require.NoError(t, err)
	}

	if !crossComp {
		setPoolMatchStatus(t, store, compID, "Pool A-2", state.MatchStatusRunning)
		return eng, store
	}
	other := compID + "-other"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: other, Name: other, Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePoolMatches(other, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Frank", SideB: "Gina", Court: "A", Status: state.MatchStatusRunning},
	}))
	return eng, store
}

func setPoolMatchStatus(t *testing.T, store *state.Store, compID, matchID string, status state.MatchStatus) {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range matches {
		if matches[i].ID == matchID {
			matches[i].Status = status
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
}

func TestReopenToScheduledIgnoresABusyCourt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		crossComp bool
	}{
		{"same competition", false},
		{"another competition on the same court", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("still barred: reopens to the queue", func(t *testing.T) {
				compID := "reopen-busy-barred"
				eng, store := busyCourtFusenshoFixture(t, compID, false, tc.crossComp)

				_, err := eng.ReopenMatch(compID, "Pool A-1", "operator error")
				require.NoError(t, err, "a reopen to the queue takes no court, so a busy court cannot refuse it")
				m := loadPoolMatchByID(t, store, compID, "Pool A-1")
				assert.Equal(t, state.MatchStatusScheduled, m.Status)
				assert.Empty(t, m.Decision)
			})

			t.Run("reinstated: reopens onto the court, so a busy court refuses it", func(t *testing.T) {
				compID := "reopen-busy-clear"
				eng, store := busyCourtFusenshoFixture(t, compID, true, tc.crossComp)

				_, err := eng.ReopenMatch(compID, "Pool A-1", "operator error")
				var busy *CourtBusyError
				require.ErrorAs(t, err, &busy)
				assert.Equal(t, "A", busy.Court)
				m := loadPoolMatchByID(t, store, compID, "Pool A-1")
				assert.Equal(t, state.MatchStatusCompleted, m.Status, "a refused reopen changes nothing")
				assert.Equal(t, "fusensho", m.Decision)
			})
		})
	}
}

// The requeue door exists to free a court for a reopen that needs one. For a
// reopen to the queue it would wipe the court occupant's live score for
// nothing, so it is refused before the requeue, naming the plain reopen.
func TestRequeueBlockerAndReopenRefusesAReopenThatNeedsNoCourt(t *testing.T) {
	compID := "requeue-no-court"
	eng, store := busyCourtFusenshoFixture(t, compID, false, false)

	_, err := eng.RequeueBlockerAndReopen(compID, "Pool A-1", compID, "Pool A-2", "operator error")
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Contains(t, verr.Error(), "Reopen it directly")
	assert.Equal(t, state.MatchStatusRunning, loadPoolMatchByID(t, store, compID, "Pool A-2").Status,
		"the court's occupant keeps its bout")
	assert.Equal(t, state.MatchStatusCompleted, loadPoolMatchByID(t, store, compID, "Pool A-1").Status)
}

// The pre-read that the reopen lands scheduled is HELD inside the tx: the
// cross-competition gate was skipped on its word, so a reinstatement landing
// between the pre-read and the tx must not turn the reopen into a running
// match on a court another competition may be using.
func TestReopenHeldToScheduledWhenThePreReadSaidSo(t *testing.T) {
	compID := "reopen-held"
	// Reinstated, so the tx on its own would reopen onto the court, and the
	// court is free, so nothing but the held pre-read keeps it scheduled.
	eng, store := busyCourtFusenshoFixture(t, compID, true, false)
	setPoolMatchStatus(t, store, compID, "Pool A-2", state.MatchStatusScheduled)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)

	_, err = eng.reopenUnderCourtLock(compID, comp, "Pool A-1", "operator error", ForceOptions{}, true)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusScheduled, loadPoolMatchByID(t, store, compID, "Pool A-1").Status)
}
