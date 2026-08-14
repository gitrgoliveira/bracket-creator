package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergeStoredPoolMatch replaced four hand-copied blocks, one per pool write
// closure. The copies had drifted twice in the direction that hurts, so these
// tests pin the two policies AGAINST each other rather than testing one shape:
// a difference that is deliberate must be visible here, and a field that
// silently reaches only one policy fails.
func TestMergeStoredPoolMatch_Policies(t *testing.T) {
	dh := state.DaihyosenSubPosition

	storedMatch := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "P1-1", SideA: "Kyoto", SideB: "Osaka",
			Court: "A", ScheduledAt: "09:00", Round: 2,
			CorrectionReason: "reopened after a mis-scored bout",
			SubResults: []state.SubMatchResult{{
				Position: dh, SideA: "Kyoto", SideB: "Osaka",
				Winner: "Kyoto", Decision: "daihyosen",
				DecidedByHantei: state.HanteiPtr(true),
			}},
		}
	}

	// A payload that says nothing about the reason, the verdict or the schedule.
	silentPayload := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "P1-1", SideA: "Kyoto", SideB: "Osaka",
			SubResults: []state.SubMatchResult{
				{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"},
			},
		}
	}

	t.Run("both policies inherit the schedule context a payload omits", func(t *testing.T) {
		for name, policy := range map[string]poolWritePolicy{
			"forward": poolWriteForward, "restore": poolWriteRestore,
		} {
			t.Run(name, func(t *testing.T) {
				in := silentPayload()
				require.False(t, mergeStoredPoolMatch(in, storedMatch(), policy))
				assert.Equal(t, "A", in.Court)
				assert.Equal(t, "09:00", in.ScheduledAt)
				assert.Equal(t, 2, in.Round)
			})
		}
	})

	t.Run("forward inherits the correction reason, restore clears it", func(t *testing.T) {
		// Forward: the kachinuki reopen persisted the operator's audit
		// justification and the next plain score write carries none of its own,
		// so the whole-struct overwrite must not blank it.
		fwd := silentPayload()
		require.False(t, mergeStoredPoolMatch(fwd, storedMatch(), poolWriteForward))
		assert.Equal(t, "reopened after a mis-scored bout", fwd.CorrectionReason)

		// Restore: an empty field in a trusted snapshot means "this was empty",
		// so it must NOT pick up the rejected partial write's reason.
		res := silentPayload()
		require.False(t, mergeStoredPoolMatch(res, storedMatch(), poolWriteRestore))
		assert.Empty(t, res.CorrectionReason)
	})

	t.Run("an explicit correction reason always wins", func(t *testing.T) {
		in := silentPayload()
		in.CorrectionReason = "this write's own reason"
		require.False(t, mergeStoredPoolMatch(in, storedMatch(), poolWriteForward))
		assert.Equal(t, "this write's own reason", in.CorrectionReason)
	})

	t.Run("forward restores a verdict-silent daihyosen, restore replays as captured", func(t *testing.T) {
		fwd := silentPayload()
		require.False(t, mergeStoredPoolMatch(fwd, storedMatch(), poolWriteForward))
		require.True(t, fwd.SubResults[0].HanteiDecided())
		assert.Equal(t, "Kyoto", fwd.SubResults[0].Winner)
		// ...and the encounter records the winner the verdict names.
		assert.Equal(t, "Kyoto", fwd.Winner)

		// The rollback replays a snapshot verbatim: it must not derive a winner
		// onto a match whose captured state had none. Before the policy split
		// this path ran the forward merge and could do exactly that.
		res := silentPayload()
		require.False(t, mergeStoredPoolMatch(res, storedMatch(), poolWriteRestore))
		assert.False(t, res.SubResults[0].HanteiDecided())
		assert.Empty(t, res.SubResults[0].Winner)
		assert.Empty(t, res.Winner)
	})

	t.Run("forward abandons a mismatched pairing, restore proceeds", func(t *testing.T) {
		fwd := silentPayload()
		fwd.SideB = "Nara" // stored says Osaka
		assert.True(t, mergeStoredPoolMatch(fwd, storedMatch(), poolWriteForward),
			"a client payload naming the wrong pairing must be rejected")

		// A snapshot's sides came from this same match, so a mismatch is not a
		// client error; a rollback that silently did nothing would be worse.
		res := silentPayload()
		res.SideB = "Nara"
		assert.False(t, mergeStoredPoolMatch(res, storedMatch(), poolWriteRestore))
	})

	t.Run("an omitted side is backfilled from the stored pairing", func(t *testing.T) {
		in := silentPayload()
		in.SideA, in.SideB = "", ""
		require.False(t, mergeStoredPoolMatch(in, storedMatch(), poolWriteForward))
		assert.Equal(t, "Kyoto", in.SideA)
		assert.Equal(t, "Osaka", in.SideB)
	})
}

// End-to-end through the non-tx forward path: the team quick-score handler
// (handlers_match.go) calls RecordMatchResult, which reaches writeMatchResult.
// That writer had no CorrectionReason preservation while its Tx twin did, so a
// quick-score landing after a kachinuki reopen blanked the operator's audit
// justification. Pinned end-to-end because the drift was invisible at unit level.
func TestQuickScoreKeepsCorrectionReason(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	compID := "qs1"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "QS", Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-1", SideA: "Kyoto", SideB: "Osaka", Status: state.MatchStatusRunning,
		CorrectionReason: "reopened: bout 2 recorded on the wrong side",
	}}))

	// The first "Record bout" after a reopen carries no reason of its own.
	require.NoError(t, eng.RecordMatchResult(compID, "P1-1", &state.MatchResult{
		SideA: "Kyoto", SideB: "Osaka", Winner: "Kyoto",
		Status: state.MatchStatusCompleted,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M"}, Winner: "K1", Decision: "fought"},
		},
	}))

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "reopened: bout 2 recorded on the wrong side", stored[0].CorrectionReason,
		"the audit justification must survive a plain score write")
}
