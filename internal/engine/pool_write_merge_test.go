package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyPoolWrite is the single merge-and-overwrite shared by every pool write
// closure. These tests pin the two policies AGAINST each other rather than
// testing one shape: a difference that is deliberate must be visible here, and
// a field that silently reaches only one policy fails.
func TestApplyPoolWrite_Policies(t *testing.T) {
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
		for _, policy := range []matchWritePolicy{matchWriteForward, matchWriteRestore} {
			in, st := silentPayload(), storedMatch()
			require.False(t, applyPoolWrite(st, in, policy), "policy %d", policy)
			assert.Equal(t, "A", in.Court, "policy %d", policy)
			assert.Equal(t, "09:00", in.ScheduledAt, "policy %d", policy)
			assert.Equal(t, 2, in.Round, "policy %d", policy)
		}
	})

	t.Run("both policies backfill an omitted side", func(t *testing.T) {
		// reconcileSides backfills as a SIDE EFFECT and only reports the
		// mismatch, so it has to run under both policies. Asserted for restore
		// too: folding the call into a short-circuit once made it possible for a
		// reorder to drop the backfill on this path alone, writing empty sides
		// into the stored match.
		for _, policy := range []matchWritePolicy{matchWriteForward, matchWriteRestore} {
			in, st := silentPayload(), storedMatch()
			in.SideA, in.SideB = "", ""
			require.False(t, applyPoolWrite(st, in, policy), "policy %d", policy)
			assert.Equal(t, "Kyoto", in.SideA, "policy %d", policy)
			assert.Equal(t, "Osaka", in.SideB, "policy %d", policy)
		}
	})

	t.Run("forward inherits the correction reason, restore clears it", func(t *testing.T) {
		// Forward: the kachinuki reopen persisted the operator's audit
		// justification and the next plain score write carries none of its own,
		// so the overwrite must not blank it.
		fwd := silentPayload()
		require.False(t, applyPoolWrite(storedMatch(), fwd, matchWriteForward))
		assert.Equal(t, "reopened after a mis-scored bout", fwd.CorrectionReason)

		// Restore: an empty field in a trusted snapshot means "this was empty",
		// so it must NOT pick up the rejected partial write's reason.
		res := silentPayload()
		require.False(t, applyPoolWrite(storedMatch(), res, matchWriteRestore))
		assert.Empty(t, res.CorrectionReason)
	})

	t.Run("an explicit correction reason always wins", func(t *testing.T) {
		in := silentPayload()
		in.CorrectionReason = "this write's own reason"
		require.False(t, applyPoolWrite(storedMatch(), in, matchWriteForward))
		assert.Equal(t, "this write's own reason", in.CorrectionReason)
	})

	t.Run("forward restores a verdict-silent daihyosen, restore replays as captured", func(t *testing.T) {
		fwd := silentPayload()
		require.False(t, applyPoolWrite(storedMatch(), fwd, matchWriteForward))
		require.True(t, fwd.SubResults[0].HanteiDecided())
		assert.Equal(t, "Kyoto", fwd.SubResults[0].Winner)
		// ...and the encounter records the winner the verdict names.
		assert.Equal(t, "Kyoto", fwd.Winner)

		// The rollback replays a snapshot verbatim: it must not derive a winner
		// onto a match whose captured state had none. Before the policy split
		// this path ran the forward merge and could do exactly that.
		res := silentPayload()
		require.False(t, applyPoolWrite(storedMatch(), res, matchWriteRestore))
		assert.False(t, res.SubResults[0].HanteiDecided())
		assert.Empty(t, res.SubResults[0].Winner)
		assert.Empty(t, res.Winner)
	})

	t.Run("the merged result is written through to the stored match", func(t *testing.T) {
		// applyPoolWrite owns the overwrite so that it is unreachable without
		// the merge; a caller can no longer do one and forget the other.
		in, st := silentPayload(), storedMatch()
		in.Winner = "Kyoto"
		require.False(t, applyPoolWrite(st, in, matchWriteForward))
		assert.Equal(t, "Kyoto", st.Winner)
		assert.Equal(t, "reopened after a mis-scored bout", st.CorrectionReason)
		assert.True(t, st.SubResults[0].HanteiDecided())
	})

	t.Run("forward abandons a mismatched pairing and leaves the stored match alone", func(t *testing.T) {
		in, st := silentPayload(), storedMatch()
		in.SideB = "Nara" // stored says Osaka
		in.Winner = "Nara"
		assert.True(t, applyPoolWrite(st, in, matchWriteForward),
			"a client payload naming the wrong pairing must be rejected")
		assert.Empty(t, st.Winner, "an abandoned write must not touch the stored match")
		assert.Equal(t, "Osaka", st.SideB)

		// A snapshot's sides came from this same match, so a mismatch is not a
		// client error; a rollback that silently did nothing would be worse.
		in2, st2 := silentPayload(), storedMatch()
		in2.SideB = "Nara"
		assert.False(t, applyPoolWrite(st2, in2, matchWriteRestore))
		assert.Equal(t, "Nara", st2.SideB, "restore replays the snapshot as captured")
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
	createTestCompetition(t, store, compID, "pools", 3, func(c *state.Competition) {
		c.Name = "QS"
		c.Kind = "team"
		c.TeamSize = 3
	})
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

// A pool match can now hold a hantei that survives a restart, so the merge has
// to protect the match-level verdict the way it already protects a daihyosen
// sub-bout: a second editor that never saw the verdict must not erase it.
func TestPoolWrite_MatchLevelHanteiSurvivesASilentRescore(t *testing.T) {
	stored := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			DecidedByHantei: state.HanteiExplicit(true),
		}
	}

	t.Run("a verdict-silent forward write keeps it", func(t *testing.T) {
		s := stored()
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		require.NotNil(t, s.DecidedByHantei, "nil means the writer said nothing")
		assert.True(t, *s.DecidedByHantei)
	})

	t.Run("an explicit false still withdraws it", func(t *testing.T) {
		s := stored()
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: state.HanteiExplicit(false),
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		require.NotNil(t, s.DecidedByHantei)
		assert.False(t, *s.DecidedByHantei, "an operator withdrawal must apply")
	})

	t.Run("restore inherits nothing", func(t *testing.T) {
		// The K3 rollback replays a snapshot: a nil there means the match HAD
		// no verdict, so preserving would re-apply the write being undone.
		s := stored()
		snapshot := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusScheduled,
		}
		require.False(t, applyPoolWrite(s, snapshot, matchWriteRestore))
		assert.Nil(t, s.DecidedByHantei, "the rolled-back verdict must not survive")
	})
}

// A carried verdict must still be VALID for the write that carries it. An
// unguarded inherit is worse than none: RecordDecision builds its MatchResult
// from scratch and never sets DecidedByHantei, and the decision handler skips
// ScoreRequest.Validate, so a withdrawal arrives verdict-silent and a bare
// carry stamps it as a judges' decision — which export.SideMarks then prints as
// "Ht" and "Kiken" on one encounter.
func TestPreserveMatchHantei_OnlyCarriesAVerdictThatStillHolds(t *testing.T) {
	stored := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			DecidedByHantei: state.HanteiExplicit(true),
		}
	}
	silent := func(dec string, a, b []string) *state.MatchResult {
		return &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status: state.MatchStatusCompleted, Decision: dec,
			IpponsA: a, IpponsB: b,
		}
	}
	hantei := func(m *state.MatchResult) bool {
		return m.DecidedByHantei != nil && *m.DecidedByHantei
	}

	t.Run("a withdrawal does not inherit the verdict", func(t *testing.T) {
		for _, dec := range []string{"kiken-injury", "kiken-voluntary", "fusenpai", "fusensho", "hikiwake"} {
			s := stored()
			require.False(t, applyPoolWrite(s, silent(dec, []string{"○", "○"}, []string{"K"}), matchWriteForward))
			assert.Falsef(t, hantei(s), "decision %q must not be recorded as a judges' decision", dec)
		}
	})

	t.Run("an untied re-score drops it", func(t *testing.T) {
		s := stored()
		require.False(t, applyPoolWrite(s, silent("", []string{"M", "K"}, []string{"D"}), matchWriteForward))
		assert.False(t, hantei(s), "a hantei rests on a tied scoreline")
	})

	t.Run("a still-tied ordinary re-score keeps it", func(t *testing.T) {
		s := stored()
		require.False(t, applyPoolWrite(s, silent("fought", []string{"D"}, []string{"T"}), matchWriteForward))
		assert.True(t, hantei(s), "the verdict still holds, so a silent writer must not erase it")
	})

	t.Run("an explicit true that no longer holds is refused", func(t *testing.T) {
		s := stored()
		in := silent("kiken-injury", []string{"○", "○"}, []string{"K"})
		in.DecidedByHantei = state.HanteiExplicit(true)
		require.False(t, applyPoolWrite(s, in, matchWriteForward))
		assert.False(t, hantei(s))
	})

	t.Run("the bracket twin applies the same guard", func(t *testing.T) {
		// Pre-existing there: the nil branch kept bm.DecidedByHantei with no
		// compatibility test at all.
		bm := &state.BracketMatch{
			ID: "m-r1-0", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status: state.MatchStatusCompleted, DecidedByHantei: true,
		}
		_, err := applyBracketMatchResult(bm, silent("kiken-injury", []string{"○", "○"}, []string{"K"}), matchWriteForward)
		require.NoError(t, err)
		assert.False(t, bm.DecidedByHantei, "a withdrawal is not a judges' decision on a knockout match either")
	})
}
