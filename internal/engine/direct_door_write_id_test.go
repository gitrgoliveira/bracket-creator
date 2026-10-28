package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The direct doors (reopen, send back to the queue, the requeue-and-reopen
// remedy, a kachinuki bout's removal) take the optional
// writeId and basis every rule-bound door takes (bc-evts F1): the write's
// baseline carries them, the log begins with the match if it had none, and a
// resend of the id is answered as recorded, nothing written and no history
// line (Q6).

func historyLen(t *testing.T, store *state.Store, compID, matchID string) int {
	t.Helper()
	h, err := store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	return len(h)
}

func loggedTag(t *testing.T, store *state.Store, compID, matchID, writeID string) *state.ScoreBaseline {
	t.Helper()
	lines, err := store.LoadScoreLog(compID, matchID)
	require.NoError(t, err)
	for _, l := range lines {
		if l.Baseline != nil && l.Baseline.WriteID == writeID {
			return l.Baseline
		}
	}
	return nil
}

func TestDirectDoors_WriteIDResendIsRecorded(t *testing.T) {
	basis := &state.ScoreBasis{Seen: 1}
	t.Run("send back to the queue", func(t *testing.T) {
		eng, store, _ := slwSetup(t)
		tag := ForceOptions{WriteID: "w-requeue", Basis: basis}
		require.NoError(t, eng.RevertMatchToQueue(slwComp, slwMatch, tag))
		b := loggedTag(t, store, slwComp, slwMatch, "w-requeue")
		require.NotNil(t, b, "the log begins with the match and the send back's baseline carries its id")
		assert.Equal(t, *basis, b.Basis)
		assertLogIsTheMatch(t, eng, store, slwMatch)
		lines := historyLen(t, store, slwComp, slwMatch)

		require.NoError(t, eng.RevertMatchToQueue(slwComp, slwMatch, tag), "the resend is answered as recorded")
		assert.Equal(t, lines, historyLen(t, store, slwComp, slwMatch), "no history line for a quiet resend")
	})
	t.Run("reopen", func(t *testing.T) {
		eng, store, compID, _ := seedPoolWithdrawal(t, "kiken-voluntary")
		tag := ForceOptions{WriteID: "w-reopen"}
		_, err := eng.ReopenMatch(compID, "Pool A-0", "mistake", tag)
		require.NoError(t, err)
		require.NotNil(t, loggedTag(t, store, compID, "Pool A-0", "w-reopen"))
		lines := historyLen(t, store, compID, "Pool A-0")

		_, err = eng.ReopenMatch(compID, "Pool A-0", "mistake", tag)
		require.NoError(t, err, "a resend is not refused for a match no longer completed")
		assert.Equal(t, lines, historyLen(t, store, compID, "Pool A-0"))
		_, err = eng.ReopenMatch(compID, "Pool A-0", "mistake")
		require.Error(t, err, "without the id the same reopen is refused, as before")
	})
	t.Run("a kachinuki bout's removal", func(t *testing.T) {
		eng, store, comp := setupKachinukiComp(t, "dd-kachinuki", 2)
		rt := seedRosterTeams(t, store, comp.ID, 2, []string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
		won := rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false)
		won.IpponsA = []string{"M"}
		matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{won, rosterBout(2, rt.a[0], rt.b[1], nil, false)}, false)
		tag := ForceOptions{WriteID: "w-remove"}
		updated, err := eng.RemoveTrailingKachinukiBout(comp.ID, matchID, tag)
		require.NoError(t, err)
		require.Len(t, updated.SubResults, 1)
		require.NotNil(t, loggedTag(t, store, comp.ID, matchID, "w-remove"))
		lines := historyLen(t, store, comp.ID, matchID)

		again, err := eng.RemoveTrailingKachinukiBout(comp.ID, matchID, tag)
		require.NoError(t, err, "a resend is not refused for a bout already gone")
		assert.Len(t, again.SubResults, 1, "answered with the match as stored")
		assert.Equal(t, lines, historyLen(t, store, comp.ID, matchID))
	})
}
