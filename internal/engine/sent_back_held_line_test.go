package engine

import (
	"testing"
	"time"

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
	assert.False(t, check("m4",
		line("m4", doorRequeue, state.HistoryOutcomeApplied),
		line("m4", "score", state.HistoryOutcomeApplied),
		line("m4", "score", state.HistoryOutcomeUnchanged)),
		"a second device's start echoing the first (unchanged) leaves the match started, not sent back")

	// The same through the engine: a send back, a start, the same start again.
	eng2, store2, _ := slwSetup(t)
	require.NoError(t, eng2.RevertMatchToQueue(slwComp, slwMatch))
	start := func() {
		require.NoError(t, eng2.RecordMatchResult(slwComp, slwMatch, &state.MatchResult{
			SideA: "Aka", SideB: "Shiro", Status: state.MatchStatusRunning,
			ModifiedAt: time.Now().UnixMilli(), Changed: []string{state.GroupResult}, WriteDoor: DoorScore,
		}))
	}
	start()
	start()
	history, err := store2.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)
	require.Equal(t, state.HistoryOutcomeUnchanged, history[len(history)-1].Outcomes[state.GroupResult], "the second start echoes the first")
	var sentBack bool
	require.NoError(t, store2.WithTransaction(slwComp, func(tx state.StoreTx) error {
		sentBack = eng2.sentBackToQueue(tx, slwComp, slwMatch)
		return nil
	}))
	assert.False(t, sentBack, "the echoed start still reads as a start")
}
