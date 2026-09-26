package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-sbq (operator ruling 2026-09-26): a later knockout match sent back to the
// queue keeps its score. When the earlier match that feeds it is corrected so
// a different competitor goes through, the later match takes the new name and
// KEEPS its points; nothing is asked when it is started, the operator clears
// or finishes it themselves. A queued match is not "fought": only running and
// completed ones are.

// seedQueuedFinal is a two-round knockout: m-r1-0 decided (by decision, so a
// reopen is allowed), and the final queued with a point kept on Alice's side.
func seedQueuedFinal(t *testing.T, store *state.Store, compID, decision string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: compID, Status: state.CompStatusKnockout}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
			Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted, IpponsA: []string{"M", "M"},
			Decision: decision, MatchNumber: 1, DisplayRound: 2}},
		{{ID: "m-r2-0", SideA: "Alice", SideB: "Charlie", SideAID: "alice", SideBID: "charlie",
			Status: state.MatchStatusScheduled, IpponsA: []string{"M"}, HansokuB: 1,
			MatchNumber: 2, DisplayRound: 1}},
	}}))
}

func TestCorrection_ReseatsAQueuedLaterMatchKeepingItsPoints(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "queued-final-correction"
	seedQueuedFinal(t, store, compID, "")

	_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", &state.MatchResult{
		SideA: "Alice", SideB: "Bob", Winner: "Bob", IpponsA: []string{}, IpponsB: []string{"M", "M"},
		Status: state.MatchStatusCompleted, CorrectionReason: "wrong winner",
	})
	require.NoError(t, err)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	final := b.Rounds[1][0]
	assert.Equal(t, "Bob", final.SideA, "the new name goes through")
	assert.Equal(t, "bob", final.SideAID)
	assert.Equal(t, state.MatchStatusScheduled, final.Status, "still waiting in the queue")
	assert.Equal(t, []string{"M"}, final.IpponsA, "its kept point stays")
	assert.Equal(t, 1, final.HansokuB, "its kept penalty stays")
}

func TestReopen_UnseatsFromAQueuedLaterMatchKeepingItsPoints(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "queued-final-reopen"
	seedQueuedFinal(t, store, compID, "kiken-voluntary")

	_, err := eng.ReopenMatch(compID, "m-r1-0", "wrong withdrawal")
	require.NoError(t, err, "a queued later match holding kept points is not a fought one")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	final := b.Rounds[1][0]
	assert.Equal(t, winnerOfPlaceholder(2, 0), final.SideA, "waits for the reopened match's winner")
	assert.Empty(t, final.SideAID)
	assert.Equal(t, []string{"M"}, final.IpponsA, "its kept point stays")
	assert.Equal(t, 1, final.HansokuB, "its kept penalty stays")
}
