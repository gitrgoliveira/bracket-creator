package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
				IpponsA: []string{domain.HanteiMark}, IpponsB: []string{},
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

// The verdict is the domain.HanteiMark entry in the winner's ippon slice, so
// it travels WITH the scoreline: a writer that supplies ippons has addressed
// the verdict (mark present = it stands, mark absent = it does not), and the
// old flag-carry machinery has nothing left to carry. The one write shape
// that loses a verdict it arguably "did not address" is a stale pre-ruling
// client re-scoring a hantei match with markless ippons inside the offline
// queue's replay window; accepted and documented in state/legacy_hantei.go.
func TestPoolWrite_HanteiTravelsWithTheScoreline(t *testing.T) {
	stored := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"K"},
		}
	}

	t.Run("a re-score echoing the mark keeps the verdict", func(t *testing.T) {
		// The real SPA shape: clients echo the ippons they were served, mark
		// included, so an untouched verdict round-trips by construction.
		s := stored()
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"K"},
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		assert.True(t, s.HanteiDecided())
	})

	t.Run("a markless re-score clears it", func(t *testing.T) {
		s := stored()
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		assert.False(t, s.HanteiDecided(),
			"the ippons are the verdict channel; a writer that replaces them has spoken")
	})

	t.Run("restore replays the snapshot's scoreline verbatim", func(t *testing.T) {
		s := stored()
		snapshot := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusScheduled,
		}
		require.False(t, applyPoolWrite(s, snapshot, matchWriteRestore))
		assert.False(t, s.HanteiDecided(), "the rolled-back verdict must not survive")
	})
}

// A mark that reaches a merge must still be VALID for the write carrying it.
// RecordDecision builds its MatchResult from scratch and inherits stored
// ippons through preserveLoserScore, so a kiken recorded over a stored hantei
// arrives with the old winner's marked slice on what is now the LOSER's side
// — which export.SideMarks would print as "Ht" and "Kiken" on one encounter.
// stripInvalidHantei (both branches, forward only) strips a mark whose
// preconditions fail or that sits on a non-winner's side: points stay, the
// verdict goes.
func TestStripInvalidHantei_GuardsTheNonValidatedPaths(t *testing.T) {
	t.Run("kiken over a stored hantei drops the mark, keeps the points", func(t *testing.T) {
		s := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"K"},
		}
		// Alice (the hantei winner) withdraws: the decision path names Bob the
		// winner, fills his maru, and preserves Alice's struck score - which
		// still carries the now-contradictory mark.
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Bob",
			Status: state.MatchStatusCompleted, Decision: "kiken-voluntary",
			DecisionBy: "shiro", DecisionReason: "injured shoulder",
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"○", "○"},
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		assert.False(t, s.HanteiDecided(), "a withdrawal is not a judges' decision")
		assert.Equal(t, []string{"M"}, s.IpponsA, "the withdrawer's point remains valid (FIK Art. 32)")
	})

	t.Run("a mark that lands untied is dropped", func(t *testing.T) {
		s := &state.MatchResult{ID: "Pool A-1", SideA: "Alice", SideB: "Bob"}
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{},
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		assert.False(t, s.HanteiDecided(), "a verdict rests on a tied scoreline (FIK 7-5 / 29-6)")
		assert.Equal(t, []string{"M"}, s.IpponsA)
	})

	t.Run("a mark on the loser's side is dropped from that side only", func(t *testing.T) {
		s := &state.MatchResult{ID: "Pool A-1", SideA: "Alice", SideB: "Bob"}
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M"}, IpponsB: []string{"K", domain.HanteiMark},
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		assert.False(t, s.HanteiDecided(), "the mark names the winner; it cannot sit on the loser")
		assert.Equal(t, []string{"K"}, s.IpponsB)
	})

	t.Run("a winner-less reopen shape cannot keep a verdict", func(t *testing.T) {
		s := &state.MatchResult{ID: "Pool A-1", SideA: "Alice", SideB: "Bob"}
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob",
			Status:  state.MatchStatusRunning,
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"K"},
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		assert.False(t, s.HanteiDecided(), "a hantei declares a winner; a running match has none")
	})

	t.Run("a valid mark passes untouched", func(t *testing.T) {
		s := &state.MatchResult{ID: "Pool A-1", SideA: "Alice", SideB: "Bob"}
		incoming := &state.MatchResult{
			ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"K"},
		}
		require.False(t, applyPoolWrite(s, incoming, matchWriteForward))
		assert.True(t, s.HanteiDecided())
	})

	t.Run("the bracket twin applies the same guard", func(t *testing.T) {
		bm := &state.BracketMatch{
			ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusRunning,
		}
		in := &state.MatchResult{
			ID: "m-r1-0", SideA: "Alice", SideB: "Bob", Winner: "Bob",
			Status: state.MatchStatusCompleted, Decision: "kiken-voluntary",
			DecisionBy: "shiro", DecisionReason: "injured shoulder",
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"○", "○"},
		}
		_, err := applyBracketMatchResult(bm, in, matchWriteForward)
		require.NoError(t, err)
		assert.Equal(t, "M", bm.ScoreA, "the mark must not be rendered into the score string")
		assert.NotContains(t, bm.ScoreB, domain.HanteiMark)
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

	t.Run("the K3 restore bypasses the guard even when the snapshot is stamped", func(t *testing.T) {
		// STAMPED on purpose, and older than what is stored: that is the real
		// shape on this branch. The pool rollback snapshot comes from
		// lookupExistingResult, which returns a straight COPY of the stored
		// MatchResult, so it carries the persisted ModifiedAt — unlike the
		// bracket's, which bracketMatchAsResult deliberately leaves at 0.
		//
		// So the restore is by definition older than the write it is undoing, and
		// without the exemption in applyMatchWrite the rollback loses to that
		// write every time and the rejected result stays on disk. An unstamped
		// snapshot here would pass through ApplyByTimestamp's bypass and pin
		// nothing: the guard would be mutable away undetected, which it was.
		p := poolStored()
		snap := incoming(older)
		snap.Winner = "Kyoto"
		snap.Status = state.MatchStatusScheduled
		require.False(t, applyPoolWrite(p, snap, matchWriteRestore))
		assert.Equal(t, state.MatchStatusScheduled, p.Status, "the rollback landed")
		assert.Equal(t, "Kyoto", p.Winner)
	})
}
