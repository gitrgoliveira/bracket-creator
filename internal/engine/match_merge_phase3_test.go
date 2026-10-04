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
// A finish made before a newer, VALID flag count is stored (a queued Save
// replayed after the panel recounted on another device) is ordered by its
// stamp like any other write: the newer count already decided the match, so
// the finish's own result is derived from THAT count rather than held beside
// it (operator ruling 2026-10-04, "the newest count decides"). Before the
// fix, engiFinishHeld treated any held group -- even one mergeMatchWrite's
// own S2 block had already turned into a winner -- as reason to hold the
// WHOLE finish atomically (HoldReasonEngiAtomic), which left the match
// running with no winner even though the stored count alone already settled
// it; see TestMerge_EngiNewestValidCountDecidesInBothOrders for the other
// arrival order, which already reached this same state.
func TestMerge_EngiStaleFinishIsHeldInHistory(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmEngi(t, knockout)
		count := mmRunning(h, mmT3, state.GroupFlags)
		count.FlagsA, count.FlagsB = 1, 2
		require.NoError(t, h.write(count))

		require.NoError(t, h.write(mmEngiFinish(h, mmT2, 3, 0)), "the newer, valid count already decides it")

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "the stored count gives the match its winner")
		assert.Equal(t, wrTeamB, m.Winner, "1-2: Tora's count, not the finish's own 3-0")
		assert.Equal(t, 1, m.FlagsA)
		assert.Equal(t, 2, m.FlagsB)
		assert.Equal(t, mmT3, m.GroupStamp(state.GroupFlags))
		assert.Equal(t, mmT3, m.GroupStamp(state.GroupResult), "the result moved because of the newer count, so it is as new as that count")

		entries := h.history(t)
		last := entries[len(entries)-1]
		assert.Equal(t, doorEngi, last.Door)
		assert.Equal(t, mmT2, last.Stamp, "the finish's own stamp")
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupFlags])
		assert.Contains(t, string(last.Held[state.GroupFlags]), `"flagsA":3`, "the finish's own attempted count is kept with its values")
		// The result is no longer held WITH the flags: mergeMatchWrite's own
		// S2 block already turned the stored, newer count into a winner, so
		// the result applied rather than being held atomically beside it.
		assert.NotEqual(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupResult], "the result applied, derived from the stored count")
		assert.Empty(t, last.Reason, "nothing was held atomically")
	})
}

// S2, generalized to engi (operator ruling 2026-10-04: "the newest count
// decides" in either arrival order). A finish made at T2 (3-0, Ryu, offline,
// sent late) and a later, VALID recount made at T3 (1-2, Tora) on a board
// that never saw the finish must reach the SAME final state whichever
// arrives first:
//   - recount first, then the finish: before the fix, engiFinishHeld saw the
//     finish's flags held and forced a second, atomic re-merge
//     (HoldReasonEngiAtomic) that discarded the winner mergeMatchWrite's own
//     S2 block had already derived from the stored count, leaving the match
//     running with no winner;
//   - finish first, then the recount: R3 already applied the recount to the
//     finished match correctly.
//
// Both orders now end completed, Tora winning 1-2, with the flags and result
// groups stamped at the recount's T3 and the finish's own 3-0 kept in the
// match's history as held. On the knockout branch, Tora (not Ryu) advances.
func TestMerge_EngiNewestValidCountDecidesInBothOrders(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		type out struct {
			Status         state.MatchStatus
			Winner         string
			WinnerID       string
			FlagsA, FlagsB int
			FlagsStamp     int64
			ResultStamp    int64
			ModifiedAt     int64
		}
		run := func(t *testing.T, finishFirst bool) (out, mmHome) {
			h := mmEngi(t, knockout)
			finish := mmEngiFinish(h, mmT2, 3, 0)
			recount := mmRunning(h, mmT3, state.GroupFlags)
			recount.FlagsA, recount.FlagsB = 1, 2
			if finishFirst {
				// The finish lands first: nothing is stored yet, so its own
				// 3-0 applies cleanly. R3 then applies the later, valid
				// recount to the finished match, exactly as it already did
				// before this fix -- there is nothing to hold here.
				require.NoError(t, h.write(finish))
				require.NoError(t, h.write(recount))
			} else {
				// The recount lands first and is stored as the newer count.
				// The finish arrives after it: its own 3-0 is held (this is
				// the arrival order the fix changes), and the winner is
				// derived from the stored, newer count instead.
				require.NoError(t, h.write(recount))
				require.NoError(t, h.write(finish), "the newer, valid count already decides it")

				var heldAtFinishStamp bool
				for _, e := range h.history(t) {
					if e.Stamp == mmT2 && e.Outcomes[state.GroupFlags] == state.HistoryOutcomeHeld {
						heldAtFinishStamp = true
						assert.Contains(t, string(e.Held[state.GroupFlags]), `"flagsA":3`, "the finish's own attempted count is kept with its values")
					}
				}
				assert.True(t, heldAtFinishStamp, "the finish's own flags are kept in the match's history as held")
			}

			m := h.load(t)
			return out{
				Status: m.Status, Winner: m.Winner, WinnerID: m.WinnerID,
				FlagsA: m.FlagsA, FlagsB: m.FlagsB,
				FlagsStamp: m.GroupStamp(state.GroupFlags), ResultStamp: m.GroupStamp(state.GroupResult),
				ModifiedAt: m.ModifiedAt,
			}, h
		}
		inOrder, h1 := run(t, true)
		reversed, h2 := run(t, false)
		want := out{
			Status: state.MatchStatusCompleted, Winner: wrTeamB, WinnerID: wrTeamBID,
			FlagsA: 1, FlagsB: 2, FlagsStamp: mmT3, ResultStamp: mmT3, ModifiedAt: mmT3,
		}
		assert.Equal(t, want, inOrder, "finish first, then the recount: Tora's later, valid count decides it")
		assert.Equal(t, want, reversed, "recount first, then the finish: no longer held atomically beside it")

		if knockout {
			b1, err := h1.store.LoadBracket(h1.compID)
			require.NoError(t, err)
			b2, err := h2.store.LoadBracket(h2.compID)
			require.NoError(t, err)
			assert.Equal(t, wrTeamB, b1.Rounds[1][0].SideA, "Tora, not Ryu, advances")
			assert.Equal(t, wrTeamB, b2.Rounds[1][0].SideA, "the same in either arrival order")
		}
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

// bc-mrgc fix: the engi recorder used to re-stamp the flags group with
// StampGroups' never-lower guard after the merge had already decided its
// stamp, which could not express S2's displaceNewerScoring moving a group's
// stamp BACK to the finish's own, older time (it replaces a newer but
// INVALID stored count with the finish's own scoreline and moves that count
// to the history). The flags group was left stuck at the displaced count's
// later stamp, so a further, genuinely later, VALID recount compared itself
// against that wrong stamp and was wrongly held. Both arrival orders of the
// same three writes -- an invalid partial count, a finish made before it but
// delivered after, and a later valid recount -- must reach the same state.
func TestMerge_EngiFinishDisplacementStampsTheFlagsAtTheFinishesOwnTime(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		t.Run("the invalid partial count arrives before the stale finish", func(t *testing.T) {
			h := mmEngi(t, knockout)
			partial := mmRunning(h, mmT4, state.GroupFlags)
			partial.FlagsA, partial.FlagsB = 1, 1 // not a valid engi total: held for nobody to see
			require.NoError(t, h.write(partial))

			// A queued Save result made before the partial count but
			// delivered after it: the merge cannot apply the stored (invalid)
			// count, so it displaces it to the history and applies the
			// finish on its own, older scoreline instead.
			require.NoError(t, h.write(mmEngiFinish(h, mmT2, 3, 0)))

			recount := mmRunning(h, mmT3, state.GroupFlags)
			recount.FlagsA, recount.FlagsB = 1, 2
			require.NoError(t, h.write(recount), "T3 is after the finish's T2, so the valid recount applies")

			m := h.load(t)
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
			assert.Equal(t, wrTeamB, m.Winner, "the later, valid recount decides it")
			assert.Equal(t, 1, m.FlagsA)
			assert.Equal(t, 2, m.FlagsB)
			assert.Equal(t, mmT3, m.GroupStamp(state.GroupFlags))
			assert.Equal(t, mmT3, m.GroupStamp(state.GroupResult))

			entries := h.history(t)
			var sawDisplaced bool
			for _, e := range entries {
				if e.Door == DoorDisplaced && e.Stamp == mmT4 {
					sawDisplaced = true
					assert.Equal(t, state.HistoryOutcomeHeld, e.Outcomes[state.GroupFlags])
					assert.Contains(t, string(e.Held[state.GroupFlags]), `"flagsA":1`)
				}
			}
			assert.True(t, sawDisplaced, "the invalid partial count is kept in the history as displaced")
		})
		t.Run("the finish lands first, then the same valid recount", func(t *testing.T) {
			h := mmEngi(t, knockout)
			require.NoError(t, h.write(mmEngiFinish(h, mmT2, 3, 0)))

			recount := mmRunning(h, mmT3, state.GroupFlags)
			recount.FlagsA, recount.FlagsB = 1, 2
			require.NoError(t, h.write(recount))

			m := h.load(t)
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
			assert.Equal(t, wrTeamB, m.Winner, "the later, valid recount decides it")
			assert.Equal(t, 1, m.FlagsA)
			assert.Equal(t, 2, m.FlagsB)
			assert.Equal(t, mmT3, m.GroupStamp(state.GroupFlags))
			assert.Equal(t, mmT3, m.GroupStamp(state.GroupResult))
		})
	})
}

// A stale finish arriving after the match is already completed by a LATER
// finish is held WHOLE, never allowed to move the winner back: this is
// mergeMatchWrite's own finish-atomicity rule (finishAtomic,
// HoldReasonFinishAtomic, bc-mrgc generalized well beyond engi), not
// engiFinishHeld's engi-specific atomic re-merge (HoldReasonEngiAtomic).
// Both groups the stale finish changes are older than what is stored, so
// the first merge pass inside engiFinishHeld already holds everything
// (rep.Superseded() is true on that first pass); engiFinishHeld's own
// second, atomic re-merge only exists for the case where the first pass
// left something APPLIED while still holding the flags (a genuine partial
// apply that must be undone atomically), which never happens here.
func TestMerge_EngiStaleFinishOverACompletedMatchIsHeldWhole(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		t.Run("a different winner is held whole, not just the losing group", func(t *testing.T) {
			h := mmEngi(t, knockout)
			require.NoError(t, h.write(mmEngiFinish(h, mmT3, 3, 0)), "Ryu's finish completes the match")

			// A queued finish made BEFORE that one, delivered late (an
			// offline board's Save result), with a different, also-valid
			// count that would make Tora the winner instead.
			err := h.write(mmEngiFinish(h, mmT2, 1, 2))
			require.ErrorIs(t, err, ErrMatchSuperseded, "every group the stale finish changes is held")

			m := h.load(t)
			assert.Equal(t, state.MatchStatusCompleted, m.Status, "the recorded finish stands")
			assert.Equal(t, wrTeamA, m.Winner, "Ryu's 3-0, not the stale finish's 1-2")
			assert.Equal(t, 3, m.FlagsA)
			assert.Equal(t, 0, m.FlagsB)
			assert.Equal(t, mmT3, m.GroupStamp(state.GroupResult), "the stale finish moves no stamp")
			assert.Equal(t, mmT3, m.GroupStamp(state.GroupFlags))

			entries := h.history(t)
			last := entries[len(entries)-1]
			assert.Equal(t, doorEngi, last.Door)
			assert.Equal(t, mmT2, last.Stamp, "the stale finish's own stamp")
			assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupResult], "the result is held, not just the flags")
			assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupFlags])
			assert.Contains(t, string(last.Held[state.GroupFlags]), `"flagsA":1`, "the stale finish's own attempted count is kept with its values")
			// HoldReasonFinishAtomic, not HoldReasonEngiAtomic: the generic
			// rule fired on the FIRST merge pass inside engiFinishHeld
			// (stored.Status already completed, the stale finish's own
			// result held by its stamp, and GroupDiffers true because Tora
			// != Ryu), so rep.Superseded() was already true and
			// engiFinishHeld returned that report untouched -- its own
			// `if !rep.Superseded()` guard never runs the second, atomic
			// re-merge that would have used HoldReasonEngiAtomic.
			assert.Equal(t, HoldReasonFinishAtomic, last.Reason)

			if knockout {
				b, err := h.store.LoadBracket(h.compID)
				require.NoError(t, err)
				assert.Equal(t, wrTeamA, b.Rounds[1][0].SideA, "Ryu stays advanced; the stale finish never reaches the bracket")
			}
		})

		// The variant where the stale finish carries the SAME count already
		// stored: no winner moves, so finishAtomic's GroupDiffers check on
		// the result group is false and it never fires. Each group the
		// stale finish names is then an echo of what mergeMatchWrite already
		// has (reportHeld records an unchanged value as HeldEcho, not Held),
		// so engiFinishHeld's own `len(rep.Held) == 0` gate reads this as
		// nothing worth protecting and lets the write through to
		// recordEngiMatch, which writes the same 3-0 back
		// (state.ApplyMergedGroupStamps keeps the real T3 stamp, since the
		// merge's own group-stamps map is non-nil) and returns no error.
		// This is applied as a no-op echo; it is never superseded and never
		// held atomically.
		t.Run("the same count is an echo, not a hold", func(t *testing.T) {
			h := mmEngi(t, knockout)
			require.NoError(t, h.write(mmEngiFinish(h, mmT3, 3, 0)))

			require.NoError(t, h.write(mmEngiFinish(h, mmT2, 3, 0)), "an echoed count under an older stamp is no loss")

			m := h.load(t)
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
			assert.Equal(t, wrTeamA, m.Winner)
			assert.Equal(t, 3, m.FlagsA)
			assert.Equal(t, 0, m.FlagsB)
			assert.Equal(t, mmT3, m.GroupStamp(state.GroupResult), "the echo never lowers the stamp")
			assert.Equal(t, mmT3, m.GroupStamp(state.GroupFlags))

			entries := h.history(t)
			last := entries[len(entries)-1]
			assert.Equal(t, doorEngi, last.Door)
			assert.NotEqual(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupResult], "an echo, not a hold")
			assert.NotEqual(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupFlags])
			assert.Empty(t, last.Reason, "nothing was held atomically")

			if knockout {
				b, err := h.store.LoadBracket(h.compID)
				require.NoError(t, err)
				assert.Equal(t, wrTeamA, b.Rounds[1][0].SideA, "Ryu stays advanced")
			}
		})
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
