package engine

// bc-mrgc phase 3 (operator ruling 2026-10-03: "Nothing should be dropped.
// All events must be ordered."): the drop sites phases 1-2 left open, each
// pinned against the old behaviour.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// mmEngi is mmIndividual in an engi (flag-scored) competition.
func mmEngi(t *testing.T, knockout bool) mmHome {
	t.Helper()
	h := mmIndividual(t, knockout)
	comp, err := h.store.LoadCompetition(h.compID)
	require.NoError(t, err)
	comp.Engi = true
	require.NoError(t, h.store.SaveCompetition(comp))
	return h
}

func mmEngiFinish(h mmHome, at int64, flagsA, flagsB int) *state.MatchResult {
	return &state.MatchResult{
		ID: h.matchID, SideA: wrTeamA, SideB: wrTeamB,
		Status: state.MatchStatusCompleted, FlagsA: flagsA, FlagsB: flagsB,
		ModifiedAt: at, WriteDoor: DoorScore,
		Changed: []string{state.GroupResult, state.GroupFlags},
	}
}

// The engi finish carries the stamp of when the operator made it, like every
// other write, and its two groups take that stamp (it used to be stamped
// with the server's clock on arrival, so it could not be ordered).
func TestMerge_EngiFinishTakesItsOwnStamp(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmEngi(t, knockout)
		require.NoError(t, h.write(mmEngiFinish(h, mmT2, 3, 0)))
		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status)
		assert.Equal(t, wrTeamA, m.Winner)
		assert.Equal(t, mmT2, m.GroupStamp(state.GroupResult), "the result group takes the finish's own stamp")
		assert.Equal(t, mmT2, m.GroupStamp(state.GroupFlags), "and so do the flags")
	})
}

// A finish made before a newer flag count is stored (a queued Save replayed
// after the panel recounted on another device) is ordered by its stamp: held
// whole, kept in the match's history, the newer count standing.
func TestMerge_EngiStaleFinishIsHeldInHistory(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmEngi(t, knockout)
		count := mmRunning(h, mmT3, state.GroupFlags)
		count.FlagsA, count.FlagsB = 1, 2
		require.NoError(t, h.write(count))

		err := h.write(mmEngiFinish(h, mmT2, 3, 0))
		require.ErrorIs(t, err, ErrMatchSuperseded, "the finish is older than the stored count")
		assert.Contains(t, HeldGroupsOf(err), state.GroupFlags)

		m := h.load(t)
		assert.Equal(t, state.MatchStatusRunning, m.Status, "the newer count stands; no winner was set over it")
		assert.Equal(t, 1, m.FlagsA)
		assert.Equal(t, 2, m.FlagsB)
		entries := h.history(t)
		last := entries[len(entries)-1]
		assert.Equal(t, doorEngi, last.Door)
		assert.Equal(t, mmT2, last.Stamp)
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupFlags])
		assert.Contains(t, string(last.Held[state.GroupFlags]), `"flagsA":3`, "the held count is kept with its values")
		// The flags and the winner they decide are one change: the result is
		// held WITH the flags even though its own stored stamp is older, and
		// the entry says why (engiFinishHeld's atomic re-merge).
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupResult], "the finish's winner is held with its count")
		assert.Contains(t, string(last.Held[state.GroupResult]), `"winner":"`+wrTeamA+`"`)
		assert.Equal(t, HoldReasonEngiAtomic, last.Reason)
	})
}

// A finish whose only "held" flags are an echo of the stored count (the same
// total a board already autosaved, under an older stamp) is no loss, so it
// is NOT held with the result (bc-mrgc, HeldEcho is not a hold): the finish
// applies, the winner is set from the flags, and nothing is kept in history.
// Before the fix, engiFinishHeld treated len(Held)+len(HeldEcho) > 0 as a
// reason to hold the whole finish, which answered this write superseded and
// left the match running with no winner, even though re-entering it could
// never have overwritten anything newer.
func TestMerge_EngiFinishEchoingTheStoredFlagsApplies(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmEngi(t, knockout)
		// Board A autosaves the count while the match is still running.
		count := mmRunning(h, mmT2, state.GroupFlags)
		count.FlagsA, count.FlagsB = 3, 2
		require.NoError(t, h.write(count))

		// Board B, which never saw that autosave, taps Save result at an
		// earlier stamp with the same 3-2 count.
		require.NoError(t, h.write(mmEngiFinish(h, mmT1, 3, 2)), "an echoed count is no loss; the finish applies")

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "the finish is not held")
		assert.Equal(t, wrTeamA, m.Winner, "3 flags beats 2")
		assert.Equal(t, 3, m.FlagsA)
		assert.Equal(t, 2, m.FlagsB)

		last := h.history(t)[len(h.history(t))-1]
		assert.Equal(t, doorEngi, last.Door)
		assert.NotEqual(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupFlags], "the echo is not held")
		assert.NotEqual(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupResult], "the result applied")
		assert.Empty(t, last.Reason, "nothing was held atomically")
	})
}

// bc-mrgc Finding 2 (a regression from letting an echoed finish apply,
// above): recordEngiMatch stamps BOTH groups the finish names with its own
// stamp via StampGroups, unconditionally. When the flags group was merely
// echoed (its real, newer stamp already on record), that unconditional
// stamp dragged the flags group's stamp BACKWARDS to the stale finish's
// own, older stamp -- so a genuinely later recount, timestamped between the
// two, then read as newer than the flags group's (corrupted) stamp and
// applied, flipping the winner on a count that never actually changed.
// stampGroups must never lower a group's stamp (the fix), so the flags
// group keeps the real T3 stamp of the count that set it, and the T2
// recount -- genuinely older than that -- is held.
func TestMerge_EngiFinishEchoNeverLowersTheFlagsStamp(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmEngi(t, knockout)
		// The board's own, real count: 3-2.
		count := mmRunning(h, mmT3, state.GroupFlags)
		count.FlagsA, count.FlagsB = 3, 2
		require.NoError(t, h.write(count))

		// A stale Save result from another device, made before that count
		// but delivered after it: an echo of the SAME 3-2, so it applies
		// (Finding 2's own fix), but must not touch the flags group's
		// already-newer T3 stamp.
		require.NoError(t, h.write(mmEngiFinish(h, mmT1, 3, 2)))
		afterFinish := h.load(t)
		require.Equal(t, mmT3, afterFinish.GroupStamp(state.GroupFlags), "precondition: the echoed finish did not lower the flags' stamp")

		// A third device's own recount, made between the two writes above
		// (T2), arrives last. In real time it is OLDER than the T3 count
		// actually on record, so it must be held, never applied.
		late := mmRunning(h, mmT2, state.GroupFlags)
		late.FlagsA, late.FlagsB = 2, 3
		err := h.write(late)
		require.ErrorIs(t, err, ErrMatchSuperseded, "T2 is older than the flags group's real T3 stamp")

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status)
		assert.Equal(t, wrTeamA, m.Winner, "the flags never actually moved off 3-2")
		assert.Equal(t, 3, m.FlagsA)
		assert.Equal(t, 2, m.FlagsB)
		assert.Equal(t, mmT3, m.GroupStamp(state.GroupFlags), "the stale finish's older stamp never lowered it")

		last := h.history(t)[len(h.history(t))-1]
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupFlags], "the stale recount is held, in the history")
	})
}

// R3 on an engi match: a count changed after the finish applies to the
// finished match, and the winner is worked out again. An engi match is never
// a draw, and (R4 revised, operator ruling 2026-10-04) a recount that cannot
// decide (an even total) is not applied either: it is held in the history
// with the reason, the match keeps its recorded finish, in a pool as in a
// knockout, and never goes back to running.
func TestMerge_EngiRecountAfterTheFinishIsNeverADraw(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmEngi(t, knockout)
		require.NoError(t, h.write(mmEngiFinish(h, mmT1, 3, 0)))

		recount := mmRunning(h, mmT2, state.GroupFlags)
		recount.FlagsA, recount.FlagsB = 1, 1
		err := h.write(recount)
		require.ErrorIs(t, err, ErrMatchSuperseded, "its one change is held")
		assert.Equal(t, []string{state.GroupFlags}, HeldGroupsOf(err))
		assert.Equal(t, state.HeldReasonNeedsWinner, HeldReasonOf(err), "the operator is told the match needs a winner")

		m := h.load(t)
		assert.NotEqual(t, state.DecisionDraw, m.Decision, "an engi match cannot be drawn")
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "the recorded finish stands")
		assert.Equal(t, wrTeamA, m.Winner)
		assert.Equal(t, 3, m.FlagsA)
		assert.Equal(t, 0, m.FlagsB)
		assert.Equal(t, mmT1, m.GroupStamp(state.GroupFlags), "the held count moves no stamp")
		last := h.history(t)[len(h.history(t))-1]
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupFlags])
		assert.Contains(t, string(last.Held[state.GroupFlags]), `"flagsA":1`)
		assert.Equal(t, HoldReasonEngiNeedsValidCount, last.Reason)
	})
}

// A write made before a participant rename (queued offline, replayed after)
// carries the old name. Its side ids match the stored ids, so it is the same
// competitor: the write is accepted and the stored, current name is kept,
// winner included. Only a genuine identity disagreement is refused.
func TestMerge_WriteAfterARenameIsAcceptedByItsIDs(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		queued := &state.MatchResult{
			ID: h.matchID, SideA: "Ryu (old name)", SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Winner: "Ryu (old name)", WinnerID: wrTeamAID, IpponsA: []string{"M", "K"},
			Status: state.MatchStatusCompleted, ModifiedAt: mmT2, WriteDoor: DoorScore,
		}
		require.NoError(t, h.write(queued), "the same competitors by id: not a side mismatch")
		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status)
		assert.Equal(t, wrTeamA, m.SideA, "the current name is kept")
		assert.Equal(t, wrTeamA, m.Winner, "the winner is named by the current name")
		assert.Equal(t, wrTeamAID, m.WinnerID)
		assert.Equal(t, []string{"M", "K"}, m.IpponsA)

		t.Run("a different competitor by id is still refused", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			other := mmRunning(h, mmT2, state.GroupPoints)
			other.SideA, other.SideAID = "Somebody", wrTeamCID
			require.ErrorIs(t, h.write(other), ErrMatchSideMismatch)
		})
		t.Run("a different name with no id to settle it is still refused", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			other := mmRunning(h, mmT2, state.GroupPoints)
			other.SideA = "Somebody"
			require.ErrorIs(t, h.write(other), ErrMatchSideMismatch)
		})
	})
}

// A queued decision whose first send landed but whose answer was lost is sent
// again by the offline queue with the same stamp. That replay is the same
// write landing twice: it answers as recorded, never with the T103 lock's
// decision_locked (which the queue now reports to the operator as a refusal
// instead of swallowing it as success).
func TestMerge_ExactDecisionReplayAnswersAsRecorded(t *testing.T) {
	h := mmIndividual(t, false)
	ms, err := h.store.LoadPoolMatches(h.compID)
	require.NoError(t, err)
	ms = append(ms, state.MatchResult{
		ID: "Pool A-1", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamC, SideBID: wrTeamCID,
		Status: state.MatchStatusScheduled,
	})
	require.NoError(t, h.store.SavePoolMatches(h.compID, ms))

	_, _, err = h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, false, mmT2)
	require.NoError(t, err)
	// The withdrawer's later match is put under way (a fusenpai, say, was
	// not recorded yet), which arms the T103 lock against an UNDO.
	ms, err = h.store.LoadPoolMatches(h.compID)
	require.NoError(t, err)
	for i := range ms {
		if ms[i].ID == "Pool A-1" {
			ms[i].Status = state.MatchStatusRunning
		}
	}
	require.NoError(t, h.store.SavePoolMatches(h.compID, ms))

	got, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, false, mmT2)
	require.NoError(t, err, "an exact replay is the recorded write, not an undo")
	require.NotNil(t, got)
	assert.Equal(t, "kiken-voluntary", got.Decision)

	t.Run("a different decision is still locked", func(t *testing.T) {
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "fusenpai", "aka", "", nil, false, mmT3)
		require.ErrorIs(t, err, ErrDecisionLocked)
	})
}
