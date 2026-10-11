package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The live bout of a kachinuki encounter is the highest-position numbered row
// (lastNumberedBout), and every door names the same one: Record bout appends
// one past it, the removal removes it, the unrecord and the roster read read
// it. A log with a gap or stored out of position order is where a slice
// position and a bout position part, so these pin both shapes on both
// branches.

func TestMaybeAdvanceKachinuki_AGappedLogAppendsPastTheLiveBout(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := branchName(bracket)
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-gapped-advance-"+name, 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
			a, b := rt.a, rt.b
			// Bouts 1 and 3, no bout 2: A1 beat B1, then A1 beat B2.
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(1, a[0], b[0], &a[0], false),
				rosterBout(3, a[0], b[1], &a[0], false),
			}, bracket)

			appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.True(t, appended)
			require.Len(t, post.BoutLog, 3)
			next := post.BoutLog[2]
			assert.Equal(t, 4, next.Position, "the pairing is numbered one past the live bout, not by the row count")
			assert.Equal(t, a[0].ID, next.SideAMemberID, "A1 stays on")
			assert.Equal(t, b[2].ID, next.SideBMemberID, "B3 is next")
			assert.True(t, post.BoutLog[1].Recorded, "bout 3, the live bout, is the one recorded")
			assert.False(t, post.BoutLog[0].Recorded)
			assert.NotZero(t, storedGroupStamp(t, store, comp.ID, matchID, bracket, state.BoutGroup(4)),
				"the appended bout's own group is stamped")
		})
	}
}

func TestRemoveTrailingKachinukiBout_AnOutOfOrderLogRemovesTheLiveBout(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := branchName(bracket)
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-out-of-order-remove-"+name, 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
			a, b := rt.a, rt.b
			// The unscored pairing (bout 2) is stored BEFORE the scored bout 1.
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(2, a[0], b[1], nil, false),
				rosterBout(1, a[0], b[0], &a[0], false),
			}, bracket)

			updated, err := eng.RemoveTrailingKachinukiBout(comp.ID, matchID)
			require.NoError(t, err)
			require.Len(t, updated.SubResults, 1)
			assert.Equal(t, 1, updated.SubResults[0].Position, "bout 2, the live bout, is the one removed")
			subs := storedSubs(t, store, comp.ID, matchID, bracket)
			require.Len(t, subs, 1)
			assert.Equal(t, 1, subs[0].Position)
			assert.NotZero(t, storedGroupStamp(t, store, comp.ID, matchID, bracket, state.BoutGroup(2)),
				"the removed bout leaves its tombstone")
			assert.Zero(t, storedGroupStamp(t, store, comp.ID, matchID, bracket, state.BoutGroup(1)),
				"the scored bout's group is untouched")
		})
	}
}

func TestRemoveTrailingKachinukiBout_AScoredLiveBoutStoredFirstIsRefused(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := branchName(bracket)
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-out-of-order-refuse-"+name, 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
			a, b := rt.a, rt.b
			// The live bout (2) is scored, stored first; an unscored bout 1 is
			// the slice's last row. Neither is the unscored trailing pairing.
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(2, a[0], b[1], &a[0], false),
				rosterBout(1, a[0], b[0], nil, false),
			}, bracket)

			_, err := eng.RemoveTrailingKachinukiBout(comp.ID, matchID)
			require.ErrorIs(t, err, ErrNoRemovableBout)
			assert.Len(t, storedSubs(t, store, comp.ID, matchID, bracket), 2, "nothing is removed")
		})
	}
}

// The floor counts numbered bouts only: a legacy log holding one unscored
// numbered bout beside a -1 row has no bout to remove, so the encounter is
// never left without a numbered bout.
func TestRemoveTrailingKachinukiBout_ARepRowDoesNotCountTowardTheFloor(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := branchName(bracket)
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-remove-floor-rep-row-"+name, 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
			a, b := rt.a, rt.b
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(1, a[0], b[0], nil, false),
				{Position: -1},
			}, bracket)

			_, err := eng.RemoveTrailingKachinukiBout(comp.ID, matchID)
			require.ErrorIs(t, err, ErrNoRemovableBout)
			assert.Len(t, storedSubs(t, store, comp.ID, matchID, bracket), 2, "nothing is removed")
		})
	}
}

// An UNSTAMPED write cannot be ordered after the Record bout that set a mark,
// so it never takes the mark off, even when it raises the bout's overtime.
func TestKachinukiRecorded_AnUnstampedWriteNeverClearsIt(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := branchName(bracket)
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-recorded-unstamped-"+name, 2,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 2,
				[]string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
			a, b := rt.a, rt.b
			tie := rosterBout(3, a[1], b[1], nil, true)
			tie.IpponsA, tie.IpponsB = []string{"M"}, []string{"K"}
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(1, a[0], b[0], &b[0], false),
				rosterBout(2, a[1], b[0], &a[1], false),
				tie,
			}, bracket)
			appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.False(t, appended)
			require.True(t, post.BoutLog[2].Recorded)
			recordedStamp := storedGroupStamp(t, store, comp.ID, matchID, bracket, state.GroupRecorded)

			sent := append([]state.SubMatchResult(nil), post.BoutLog...)
			sent[2].Decision = ""
			sent[2].Encho = &state.EnchoMetadata{PeriodCount: 1}
			require.NoError(t, eng.RecordMatchResult(comp.ID, matchID, &state.MatchResult{
				ID: matchID, SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
				Status:     state.MatchStatusRunning,
				SubResults: sent,
				Changed:    []string{state.BoutGroup(3)}, WriteDoor: DoorScore,
			}))
			subs := storedSubs(t, store, comp.ID, matchID, bracket)
			assert.True(t, subs[2].Recorded, "an unstamped write leaves the recorded mark on")
			assert.Equal(t, recordedStamp, storedGroupStamp(t, store, comp.ID, matchID, bracket, state.GroupRecorded),
				"and does not restamp the recorded group")
		})
	}
}
