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

// hanteiStillHolds is the ENGINE-side re-application of ScoreRequest.Validate's
// DecidedByHantei block, and it must enforce that block in full: it runs after
// validation and nothing re-checks what it stamps. Each case below is a state
// the wire validator refuses, which the engine could nonetheless create by
// inheriting a stored verdict onto a verdict-silent write.
func TestPreserveMatchHanteiOnlyKeepsAVerdictTheValidatorWouldAccept(t *testing.T) {
	// The shape of a forward re-score that says nothing about the verdict.
	silent := func(mutate func(*state.MatchResult)) *state.MatchResult {
		r := &state.MatchResult{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
		}
		mutate(r)
		return r
	}

	kept := func(t *testing.T, r *state.MatchResult) {
		t.Helper()
		preserveMatchHantei(true, r)
		require.NotNil(t, r.DecidedByHantei)
		assert.True(t, *r.DecidedByHantei)
	}
	cleared := func(t *testing.T, r *state.MatchResult, why string) {
		t.Helper()
		preserveMatchHantei(true, r)
		require.NotNil(t, r.DecidedByHantei, "a stored verdict that cannot stand is cleared EXPLICITLY: nil means inherit")
		assert.False(t, *r.DecidedByHantei, why)
	}

	t.Run("kept when every condition still holds", func(t *testing.T) {
		kept(t, silent(func(*state.MatchResult) {}))
	})
	t.Run("kept when the status is unstated (defaults to completed)", func(t *testing.T) {
		kept(t, silent(func(r *state.MatchResult) { r.Status = "" }))
	})

	t.Run("cleared without a winner", func(t *testing.T) {
		// The reopen shape: {status:"running", winner:null}. Keeping it here was
		// worse than losing it — encodeHanteiIntoIppons cannot attribute a
		// winner-less verdict to a side, so the flag lived in the cache and
		// never reached pool-matches.csv: hantei until restart, none after.
		cleared(t, silent(func(r *state.MatchResult) { r.Winner = "" }),
			"a hantei declares a winner")
	})
	t.Run("cleared on a match that is not completed", func(t *testing.T) {
		cleared(t, silent(func(r *state.MatchResult) { r.Status = state.MatchStatusRunning }),
			"a running match has not been decided by anyone")
	})
	t.Run("cleared on an untied scoreline", func(t *testing.T) {
		cleared(t, silent(func(r *state.MatchResult) { r.IpponsA = []string{"M", "K"} }),
			"a verdict rests on a tied scoreline (FIK 7-5 / 29-6)")
	})
	t.Run("cleared on a daihyosen decision", func(t *testing.T) {
		// The MATCH level is the sub-bout allow-list MINUS daihyosen: the rep
		// bout is where that verdict rides, and claiming it at match level says
		// the encounter itself was judged.
		cleared(t, silent(func(r *state.MatchResult) { r.Decision = "daihyosen" }),
			"daihyosen is a sub-bout decision, never a match-level one")
	})
	t.Run("cleared on a withdrawal", func(t *testing.T) {
		cleared(t, silent(func(r *state.MatchResult) { r.Decision = "kiken-voluntary" }),
			"export.SideMarks marks Ht unconditionally, so this would print Ht AND Kiken")
	})
}

// matchWritePolicy governs BOTH branches of a match write, and a side mismatch
// is one of the things it governs: a forward payload naming a pairing that is
// not this match's is a client error (409), while the K3 restore replays sides
// captured from this same match and must land regardless.
//
// applyPoolWrite implemented that; applyBracketMatchResult returned
// ErrMatchSideMismatch under either policy, so one policy-driven write had two
// behaviours. It mattered because rollbackMatchResultTx only LOGS the error it
// gets back: a bracket rollback that bailed out here left the rejected partial
// write on disk, where the identical pool case completed the restore.
func TestSideMismatchIsAForwardOnlyErrorInBothBranches(t *testing.T) {
	drifted := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "m-r1-0", SideA: "Nara", SideB: "Kobe", // NOT this match's pairing
			Status: state.MatchStatusScheduled,
		}
	}

	t.Run("bracket branch", func(t *testing.T) {
		bm := func() *state.BracketMatch {
			return &state.BracketMatch{
				ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
				Winner: "Kyoto", Status: state.MatchStatusCompleted,
			}
		}
		_, err := applyBracketMatchResult(bm(), drifted(), matchWriteForward)
		assert.ErrorIs(t, err, ErrMatchSideMismatch, "a client may not rewrite the pairing")

		target := bm()
		applied, err := applyBracketMatchResult(target, drifted(), matchWriteRestore)
		require.NoError(t, err, "the restore replays captured sides; a mismatch is not a client error")
		require.True(t, applied)
		assert.Equal(t, state.MatchStatusScheduled, target.Status, "the rollback actually landed")
		assert.Empty(t, target.Winner)
	})

	t.Run("pool branch, for the parity this is about", func(t *testing.T) {
		stored := func() *state.MatchResult {
			return &state.MatchResult{
				ID: "Pool A-1", SideA: "Kyoto", SideB: "Osaka",
				Winner: "Kyoto", Status: state.MatchStatusCompleted,
			}
		}
		assert.True(t, applyPoolWrite(stored(), drifted(), matchWriteForward),
			"forward: abandoned, and the caller maps that to 409")

		target := stored()
		require.False(t, applyPoolWrite(target, drifted(), matchWriteRestore))
		assert.Equal(t, state.MatchStatusScheduled, target.Status, "the rollback actually landed")
	})
}

// Timestamp last-write-wins is a property of a MATCH, not of the store its
// phase happens to live in. It was bracket-only for one reason: the guard needs
// a stored stamp, and pool-matches.csv had no column for one, so a reconnecting
// court's stale change was discarded in the knockout and applied in the pool.
// Both branches now go through applyMatchWrite.
func TestTimestampGuardAppliesToBothBranches(t *testing.T) {
	const stored, older, newer = 2_000, 1_000, 3_000

	poolStored := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "Pool A-1", SideA: "Kyoto", SideB: "Osaka", Winner: "Kyoto",
			Status: state.MatchStatusCompleted, ModifiedAt: stored,
		}
	}
	bracketStored := func() *state.BracketMatch {
		return &state.BracketMatch{
			ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka", Winner: "Kyoto",
			Status: state.MatchStatusCompleted, ModifiedAt: stored,
		}
	}
	incoming := func(at int64) *state.MatchResult {
		return &state.MatchResult{
			ID: "x", SideA: "Kyoto", SideB: "Osaka", Winner: "Osaka",
			Status: state.MatchStatusCompleted, ModifiedAt: at,
		}
	}

	t.Run("a strictly older write is dropped, in the pool too", func(t *testing.T) {
		p := poolStored()
		require.False(t, applyPoolWrite(p, incoming(older), matchWriteForward))
		assert.Equal(t, "Kyoto", p.Winner, "the newer stored result stands")
		assert.EqualValues(t, stored, p.ModifiedAt)

		b := bracketStored()
		applied, err := applyBracketMatchResult(b, incoming(older), matchWriteForward)
		require.NoError(t, err)
		assert.False(t, applied)
		assert.Equal(t, "Kyoto", b.Winner, "same answer on the other branch")
	})

	t.Run("a newer write applies on both", func(t *testing.T) {
		p := poolStored()
		require.False(t, applyPoolWrite(p, incoming(newer), matchWriteForward))
		assert.Equal(t, "Osaka", p.Winner)

		b := bracketStored()
		applied, err := applyBracketMatchResult(b, incoming(newer), matchWriteForward)
		require.NoError(t, err)
		assert.True(t, applied)
		assert.Equal(t, "Osaka", b.Winner)
	})

	t.Run("an unstamped write still applies, so legacy clients are unaffected", func(t *testing.T) {
		p := poolStored()
		require.False(t, applyPoolWrite(p, incoming(0), matchWriteForward))
		assert.Equal(t, "Osaka", p.Winner, "0 means unstamped, which never loses")
		assert.EqualValues(t, stored, p.ModifiedAt,
			"and it must not reset the stored stamp, or the match reopens to stale writes")
	})

	t.Run("an unstamped STORED value accepts anything, so legacy files are unaffected", func(t *testing.T) {
		p := poolStored()
		p.ModifiedAt = 0 // a row written before the column existed
		require.False(t, applyPoolWrite(p, incoming(older), matchWriteForward))
		assert.Equal(t, "Osaka", p.Winner)
	})

	t.Run("a deliberate correction is never dropped as stale", func(t *testing.T) {
		p := poolStored()
		corr := incoming(older)
		corr.CorrectionReason = "scoreboard misread"
		require.False(t, applyPoolWrite(p, corr, matchWriteForward))
		assert.Equal(t, "Osaka", p.Winner,
			"an explicit operator correction outranks the timestamp on both branches")
	})

	t.Run("the K3 restore bypasses the guard", func(t *testing.T) {
		// The rollback snapshot deliberately carries no stamp so it cannot lose
		// to the very write it is undoing; the restore policy skips the guard for
		// the same reason it inherits nothing else.
		p := poolStored()
		snap := incoming(0)
		snap.Winner = "Kyoto"
		snap.Status = state.MatchStatusScheduled
		require.False(t, applyPoolWrite(p, snap, matchWriteRestore))
		assert.Equal(t, state.MatchStatusScheduled, p.Status, "the rollback landed")
	})
}
