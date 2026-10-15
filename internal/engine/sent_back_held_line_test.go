package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A history line whose result group was held (a stale running autosave) moved
// nothing, so it must not hide the send back to the queue behind it from the
// tap that follows (bc-evts review round 3).
func TestSentBackToQueue_SkipsALineWhoseResultWasHeld(t *testing.T) {
	const compID = "sbq-held"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "P", Format: state.CompFormatKnockout, Courts: []string{"A"}})
	line := func(matchID, door, outcome string) state.MatchHistoryEntry {
		return state.MatchHistoryEntry{MatchID: matchID, Door: door, Changed: []string{state.GroupResult},
			Outcomes: map[string]string{state.GroupResult: outcome}}
	}
	check := func(matchID string, entries ...state.MatchHistoryEntry) bool {
		var got bool
		require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
			for _, en := range entries {
				require.NoError(t, tx.AppendMatchHistory(compID, en))
			}
			got = eng.sentBackToQueue(tx, compID, matchID)
			return nil
		}))
		return got
	}
	assert.True(t, check("m1", line("m1", doorRequeue, state.HistoryOutcomeApplied)), "a lone requeue")
	assert.True(t, check("m2",
		line("m2", doorRequeue, state.HistoryOutcomeApplied),
		line("m2", "score", state.HistoryOutcomeHeld)),
		"requeue, then a stale held running autosave line: still sent back to the queue")
	assert.False(t, check("m3",
		line("m3", doorRequeue, state.HistoryOutcomeApplied),
		line("m3", "score", state.HistoryOutcomeApplied)),
		"an applied score line after the requeue is the latest status change")
}
