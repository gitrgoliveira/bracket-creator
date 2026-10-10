package engine

// bc-mrgc, operator ruling: "every change is dated and ordered; a change must
// only ever apply what it changed." The two representatives a team match's
// representative bout is fought by (SideAMemberID/SideBMemberID on the row at
// position -1) are each their own dated change, state.GroupRepPickA and
// state.GroupRepPickB, ordered apart from each other and from the bout's score
// and result: a point never alters a pick, a pick never alters the point or the
// other side's pick, and an older change of any is kept in the match's history.

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

const (
	repBoutGroup = "bout:-1"
	// Each side's pick is its own group (side A is Aka, side B is Shiro).
	repPickAName = "repPickA"
	repPickBName = "repPickB"
)

// repRow is a representative-bout row as a board sends it: the TEAM names
// (placeHt needs them), the sub-decision, and whatever ids / marks the board
// knows about.
func repRow(sideAID, sideBID string, ipponsA, ipponsB []string) state.SubMatchResult {
	return state.SubMatchResult{
		Position: state.DaihyosenSubPosition, SideA: wrTeamA, SideB: wrTeamB,
		SideAMemberID: sideAID, SideBMemberID: sideBID,
		IpponsA: ipponsA, IpponsB: ipponsB,
		Decision: string(domain.DecisionDaihyosen),
	}
}

// repWrite is a running write from a board that changed the named groups and
// carries one representative row.
func repWrite(h mmHome, at int64, row state.SubMatchResult, changed ...string) *state.MatchResult {
	w := mmRunning(h, at, changed...)
	w.SubResults = []state.SubMatchResult{row}
	return w
}

// seedPick records Carol as the Ryu representative at mmT1 (the row is added
// with its pick, as the picker does on a bout that already exists).
func seedPick(t *testing.T, h mmHome) {
	t.Helper()
	require.NoError(t, h.write(repWrite(h, mmT1, repRow("carol", "", nil, nil), repBoutGroup, repPickAName)))
	got := h.load(t)
	require.Len(t, got.SubResults, 1)
	require.Equal(t, "carol", got.SubResults[0].SideAMemberID, "setup: the pick is stored")
}

// repStamp is the stored stamp of group on the match under test, read through
// GroupStamp (never the map keys: a side with no key reads the bout row's date).
func repStamp(t *testing.T, h mmHome, group string) int64 {
	t.Helper()
	m := h.load(t)
	return m.GroupStamp(group)
}

// Device 1 picks Carol; device 2, whose push has not arrived, taps a point on
// the representative bout a moment later. Its write is newer and carries its
// whole row view, which has no ids: the point applies and the pick stays.
func TestMerge_RepPoint_KeepsAnotherDevicesPick(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		seedPick(t, h)

		require.NoError(t, h.write(repWrite(h, mmT1+400, repRow("", "", []string{"M"}, nil), repBoutGroup)))

		row := h.load(t).SubResults[0]
		assert.Equal(t, []string{"M"}, row.IpponsA, "the point applies")
		assert.Equal(t, "carol", row.SideAMemberID, "the pick is not the point's to erase")
	})
}

// The reverse: a pick made after a point, on a board that never saw the point.
func TestMerge_RepPick_KeepsAnotherDevicesPoint(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		require.NoError(t, h.write(repWrite(h, mmT1, repRow("", "", []string{"M"}, nil), repBoutGroup)))

		require.NoError(t, h.write(repWrite(h, mmT2, repRow("carol", "", nil, nil), repPickAName)))

		row := h.load(t).SubResults[0]
		assert.Equal(t, "carol", row.SideAMemberID, "the pick applies")
		assert.Equal(t, []string{"M"}, row.IpponsA, "the point is not the pick's to erase")
	})
}

// A stale pick is held and kept in the history; a newer clear clears.
func TestMerge_RepPick_StaleIsHeldAndANewerClearClears(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		require.NoError(t, h.write(repWrite(h, mmT2, repRow("carol", "", nil, nil), repBoutGroup, repPickAName)))

		err := h.write(repWrite(h, mmT1, repRow("dana", "", nil, nil), repPickAName))
		require.ErrorIs(t, err, ErrMatchSuperseded, "its one change is outranked")
		assert.Equal(t, []string{repPickAName}, HeldGroupsOf(err))
		assert.Equal(t, "carol", h.load(t).SubResults[0].SideAMemberID, "the newer pick stands")
		entries := h.history(t)
		held := entries[len(entries)-1]
		assert.Equal(t, state.HistoryOutcomeHeld, held.Outcomes[repPickAName])
		assert.JSONEq(t, `{"sideAMemberId":"dana"}`, string(held.Held[repPickAName]),
			"the older pick is kept, never discarded")

		// A deliberate clear is a change to the pick: the row is sent without
		// the id and the change is named.
		require.NoError(t, h.write(repWrite(h, mmT3, repRow("", "", nil, nil), repPickAName)))
		assert.Empty(t, h.load(t).SubResults[0].SideAMemberID, "a newer clear clears")
	})
}

// A write that carries no bout list and names nothing (an older client) says
// nothing about the picks.
func TestMerge_RepPicks_SurviveAWriteThatNamesNothingAndSendsNoBouts(t *testing.T) {
	h := mmTeam(t, true)
	seedPick(t, h)

	w := mmRunning(h, mmT2) // Changed nil, SubResults nil
	w.IpponsA = []string{"K"}
	require.NoError(t, h.write(w))

	m := h.load(t)
	require.Len(t, m.SubResults, 1)
	assert.Equal(t, "carol", m.SubResults[0].SideAMemberID)
	assert.Equal(t, []string{"K"}, m.IpponsA)
}

// A write that names nothing but carries the row (bulk-score, an older
// client) is ordered like the others: picks it has no newer opinion on stay.
func TestMerge_RepPicks_NamelessWriteIsOrderedByStamp(t *testing.T) {
	h := mmTeam(t, true)
	require.NoError(t, h.write(repWrite(h, mmT3, repRow("carol", "", nil, nil), repBoutGroup, repPickAName)))

	stale := repWrite(h, mmT2, repRow("dana", "", []string{"M"}, nil))
	stale.Changed = nil
	stale.WriteDoor = DoorBulkScore
	_ = h.write(stale)

	assert.Equal(t, "carol", h.load(t).SubResults[0].SideAMemberID, "the older pick does not replace the newer one")
}

// The winner's member id belongs to the bout's result, but a stored one must
// name one of the stored picks (or be empty): a pick that changes under it
// re-derives it from the winning side.
func TestMerge_RepPick_ReDerivesTheWinnerMemberID(t *testing.T) {
	h := mmTeam(t, true)
	won := repRow("carol", "", []string{"M"}, nil)
	won.Winner, won.WinnerMemberID = wrTeamA, "carol"
	require.NoError(t, h.write(repWrite(h, mmT1, won, repBoutGroup, repPickAName)))

	require.NoError(t, h.write(repWrite(h, mmT2, repRow("dana", "", nil, nil), repPickAName)))

	row := h.load(t).SubResults[0]
	assert.Equal(t, "dana", row.SideAMemberID)
	assert.Equal(t, wrTeamA, row.Winner, "the bout's result is the point's, untouched")
	assert.Equal(t, "dana", row.WinnerMemberID, "re-derived from the winning side and the stored pick")

	// The pick cleared under a recorded winner id leaves no dangling id.
	require.NoError(t, h.write(repWrite(h, mmT3, repRow("", "", nil, nil), repPickAName)))
	row = h.load(t).SubResults[0]
	assert.Empty(t, row.SideAMemberID)
	assert.Empty(t, row.WinnerMemberID, "a winner id naming no stored pick is empty")
}

// A winner id that names the LOSING side's pick is as wrong as one naming nobody:
// the merge derives it from the winner's NAME and the stored picks alone, never
// through the id it is checking (id-first attribution would credit the side the
// id names). The real board stamps the winner and its id together, so a
// disagreeing pair is a crafted or confused write.
func TestMerge_RepBout_WinnerMemberIDFollowsTheWinnersName(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		t.Run("an id naming the other side's pick is replaced by the winning side's", func(t *testing.T) {
			h := mmTeam(t, knockout)
			won := repRow("x", "y", []string{"M"}, nil)
			won.Winner, won.WinnerMemberID = wrTeamB, "y"
			require.NoError(t, h.write(repWrite(h, mmT1, won, repBoutGroup, repPickAName, repPickBName)))
			require.Equal(t, "y", h.load(t).SubResults[0].WinnerMemberID, "setup: team B won, its pick y is the winner id")

			crafted := repRow("x", "y", []string{"M"}, nil)
			crafted.Winner, crafted.WinnerMemberID = wrTeamB, "x"
			require.NoError(t, h.write(repWrite(h, mmT2, crafted, repBoutGroup)))

			row := h.load(t).SubResults[0]
			assert.Equal(t, wrTeamB, row.Winner)
			assert.Equal(t, "y", row.WinnerMemberID, "x is team A's pick: the winner id is the winning side's, y")
		})

		t.Run("an id naming a pick of the side that has none is cleared", func(t *testing.T) {
			h := mmTeam(t, knockout)
			won := repRow("", "y", []string{"M"}, nil)
			won.Winner, won.WinnerMemberID = wrTeamA, "y"
			require.NoError(t, h.write(repWrite(h, mmT1, won, repBoutGroup, repPickBName)))

			row := h.load(t).SubResults[0]
			assert.Equal(t, wrTeamA, row.Winner)
			assert.Equal(t, "y", row.SideBMemberID, "setup: only team B has a pick")
			assert.Empty(t, row.WinnerMemberID, "team A won and has no pick: y is the loser's, so there is no winner id")
		})
	})
}

// The winner's member id is derived state: the merge works it out again from
// the winner's name and the stored picks after every write, and the client's
// bout:-1 comparison leaves it out. So a write that names nothing (bulk-score,
// an older client) and echoes the representative row with the same score but
// a different or absent winner id has changed nothing: it must not date the
// bout (which would fence out an older real change) or count as scoring.
func TestMerge_RepBout_WinnerMemberIDAloneIsAnEcho(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		for _, tc := range []struct{ name, id string }{
			{"absent", ""},
			{"the other side's pick", "dana"},
			{"an id nobody holds", "zzz"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := mmTeam(t, knockout)
				won := repRow("carol", "dana", []string{"M"}, nil)
				won.Winner = wrTeamA
				require.NoError(t, h.write(repWrite(h, mmT1, won, repBoutGroup, repPickAName, repPickBName)))
				seeded := h.load(t)
				require.Len(t, seeded.SubResults, 1)
				require.Equal(t, "carol", seeded.SubResults[0].WinnerMemberID, "setup: derived from the winner and the picks")
				require.Equal(t, mmT1, seeded.GroupStamp(repBoutGroup), "setup: the bout is dated")
				require.Equal(t, mmT1, seeded.ModifiedAt, "setup: the match is dated")

				// The match-level winner id was backfilled after the seeding
				// merge, so the echo carries it too: only the bout's winner
				// member id differs from what is stored.
				echo := repRow("carol", "dana", []string{"M"}, nil)
				echo.Winner, echo.WinnerMemberID = wrTeamA, tc.id
				w := repWrite(h, mmT2, echo)
				w.Changed = nil
				w.WriteDoor = DoorBulkScore
				w.WinnerID = seeded.WinnerID
				require.NoError(t, h.write(w))

				got := h.load(t)
				require.Len(t, got.SubResults, 1)
				assert.Equal(t, mmT1, got.GroupStamp(repBoutGroup), "an id-only difference is an echo: the bout keeps its date")
				assert.Equal(t, mmT1, got.ModifiedAt, "and so does the match")
				assert.Equal(t, "carol", got.SubResults[0].WinnerMemberID, "re-derived from the winner and the picks")
			})
		}
	})
}

// A pick has no bout to land on once the representative bout is removed: it
// is held, in the history, not applied onto nothing.
func TestMerge_RepPick_OnARemovedBoutIsHeld(t *testing.T) {
	h := mmTeam(t, true)
	seedPick(t, h)
	remove := mmRunning(h, mmT2, repBoutGroup, repPickAName, repPickBName)
	remove.WriteDoor = DoorDaihyosenDel
	remove.SubResults = []state.SubMatchResult{}
	require.NoError(t, h.write(remove))
	require.Empty(t, h.load(t).SubResults)
	removed := h.load(t)
	assert.Equal(t, mmT2, removed.GroupStamp(repPickAName), "the removal dates side A's pick too")
	assert.Equal(t, mmT2, removed.GroupStamp(repPickBName), "and side B's")

	late := repWrite(h, mmT3, repRow("dana", "", nil, nil), repPickAName)
	err := h.write(late)
	require.ErrorIs(t, err, ErrMatchSuperseded)
	assert.Equal(t, []string{repPickAName}, HeldGroupsOf(err))
	assert.Empty(t, h.load(t).SubResults, "nothing is resurrected")

	// Each side is held on its own, with only its own id kept in the history.
	both := repWrite(h, mmT3+1, repRow("eve", "fay", nil, nil), repPickAName, repPickBName)
	err = h.write(both)
	require.ErrorIs(t, err, ErrMatchSuperseded)
	assert.Equal(t, []string{repPickAName, repPickBName}, HeldGroupsOf(err))
	entries := h.history(t)
	held := entries[len(entries)-1]
	assert.JSONEq(t, `{"sideAMemberId":"eve"}`, string(held.Held[repPickAName]))
	assert.JSONEq(t, `{"sideBMemberId":"fay"}`, string(held.Held[repPickBName]))
	still := h.load(t)
	assert.Empty(t, still.SubResults, "still nothing is resurrected")
	assert.Equal(t, mmT2, still.GroupStamp(repPickAName), "a held pick leaves the tombstone's date")
	assert.Equal(t, mmT2, still.GroupStamp(repPickBName))
}

// A match written before the picks had a stamp of their own is ordered by the
// stamp of its representative bout, as it always was.
func TestMerge_RepPicks_LegacyStampFallsBackToTheBout(t *testing.T) {
	h := mmTeam(t, true)
	bracket, err := h.store.LoadBracket(h.compID)
	require.NoError(t, err)
	bm := &bracket.Rounds[0][0]
	bm.SubResults = []state.SubMatchResult{repRow("carol", "gus", nil, nil)}
	bm.ModifiedAt = mmT2
	bm.GroupStamps = map[string]int64{repBoutGroup: mmT2} // a map written before the picks had an entry
	require.NoError(t, h.store.SaveBracket(h.compID, bracket))

	// Both sides read the row's date, until each is stamped itself.
	require.Equal(t, mmT2, repStamp(t, h, repPickAName))
	require.Equal(t, mmT2, repStamp(t, h, repPickBName))

	err = h.write(repWrite(h, mmT1, repRow("dana", "gus", nil, nil), repPickAName))
	require.ErrorIs(t, err, ErrMatchSuperseded, "older than the row it sits on")
	assert.Equal(t, "carol", h.load(t).SubResults[0].SideAMemberID)
	err = h.write(repWrite(h, mmT1, repRow("carol", "hal", nil, nil), repPickBName))
	require.ErrorIs(t, err, ErrMatchSuperseded, "side B reads the row's date too")
	assert.Equal(t, "gus", h.load(t).SubResults[0].SideBMemberID)

	require.NoError(t, h.write(repWrite(h, mmT3, repRow("dana", "gus", nil, nil), repPickAName)))
	got := h.load(t)
	assert.Equal(t, "dana", got.SubResults[0].SideAMemberID)
	assert.Equal(t, "gus", got.SubResults[0].SideBMemberID, "a pick the write did not name stays")
	assert.Equal(t, mmT2, got.GroupStamp(repPickBName), "and keeps its date")
}

// A legacy match (no stamp map) that holds no representative bout row is not
// given a pick stamp by an ordinary write: the materialization that runs before
// the first group is stamped adds the keys only for a match that has the row to
// date. Individual matches never have one.
func TestMerge_LegacyMatchWithoutARepBoutGainsNoRepPicksStamp(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		require.Nil(t, h.load(t).GroupStamps, "precondition: a legacy match")
		w := mmRunning(h, mmT1, state.GroupPoints)
		w.IpponsA = []string{"M"}
		require.NoError(t, h.write(w))
		got := h.load(t)
		assert.Equal(t, mmT1, got.GroupStamp(state.GroupPoints), "the group the write changed is stamped")
		require.NotNil(t, got.GroupStamps)
		assert.NotContains(t, got.GroupStamps, repPickAName)
		assert.NotContains(t, got.GroupStamps, repPickBName)
	})
}

// Two captains pick their representatives at about the same moment, each on
// their own phone: device 1 picks Shiro's (side B), device 2, whose screen never
// showed that pick, picks Aka's (side A) a moment later, carrying a row with no
// side-B id. Each side's pick is its own dated change, so device 2's write
// applies side A and leaves device 1's side B alone: both picks stand, each
// dated by its own write. Both sides are asserted after every write.
func TestMerge_RepPicks_TwoCaptainsBothLand(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)

		// Device 1, at T1: the pick creates the row.
		require.NoError(t, h.write(repWrite(h, mmT1, repRow("", "x", nil, nil), repBoutGroup, repPickBName)))
		got := h.load(t)
		require.Len(t, got.SubResults, 1)
		require.Equal(t, "x", got.SubResults[0].SideBMemberID, "setup: device 1's pick is stored")
		require.Empty(t, got.SubResults[0].SideAMemberID)

		// Device 2, at T2 > T1: its row carries side A only.
		err := h.write(repWrite(h, mmT2, repRow("y", "", nil, nil), repPickAName))

		got = h.load(t)
		require.Len(t, got.SubResults, 1)
		assert.Equal(t, "y", got.SubResults[0].SideAMemberID, "device 2's pick applies")
		assert.Equal(t, "x", got.SubResults[0].SideBMemberID, "and device 1's pick is not its to erase")
		assert.NoError(t, err)
		assert.Equal(t, mmT1, got.GroupStamp(repPickBName), "side B is still dated by device 1's write")
		assert.Equal(t, mmT2, got.GroupStamp(repPickAName), "side A is dated by device 2's write")
	})
}

// A stale pick on one side is held and kept in the history while a newer pick on
// the other side, in the same write, applies. The seed order is deliberate: a
// fresh match has no representative row, and the write that creates it dates
// every side it does not name through the bout:-1 fallback, so side A is made
// newer (T3) by its own write before the write under test (T2) arrives.
func TestMerge_RepPicks_StaleSideHeldWhileTheOtherSideApplies(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		require.NoError(t, h.write(repWrite(h, mmT1, repRow("", "b1", nil, nil), repBoutGroup, repPickBName)))
		require.NoError(t, h.write(repWrite(h, mmT3, repRow("a1", "", nil, nil), repPickAName)))
		seeded := h.load(t)
		require.Equal(t, "a1", seeded.SubResults[0].SideAMemberID, "setup: side A picked at T3")
		require.Equal(t, "b1", seeded.SubResults[0].SideBMemberID, "setup: side B picked at T1")

		err := h.write(repWrite(h, mmT2, repRow("a2", "b2", nil, nil), repPickAName, repPickBName))

		got := h.load(t)
		require.Len(t, got.SubResults, 1)
		assert.Equal(t, "a1", got.SubResults[0].SideAMemberID, "the newer side-A pick stands")
		assert.Equal(t, "b2", got.SubResults[0].SideBMemberID, "the side-B pick, newer than what it replaces, applies")
		assert.NoError(t, err, "applied in part: not superseded")
		entries := h.history(t)
		last := entries[len(entries)-1]
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[repPickAName])
		assert.Equal(t, state.HistoryOutcomeApplied, last.Outcomes[repPickBName])
		assert.JSONEq(t, `{"sideAMemberId":"a2"}`, string(last.Held[repPickAName]), "the stale pick is kept, never discarded")
		assert.NotContains(t, last.Held, repPickBName)
		assert.Equal(t, mmT3, got.GroupStamp(repPickAName))
		assert.Equal(t, mmT2, got.GroupStamp(repPickBName))
	})
}

// repRemoval is the representative bout's removal as the daihyosen DELETE
// door builds it: the bout, the verdict it carried, overtime and both
// representatives, and no row.
func repRemoval(h mmHome, at int64) *state.MatchResult {
	remove := mmRunning(h, at, repBoutGroup, state.GroupResult, state.GroupEncho, repPickAName, repPickBName)
	remove.WriteDoor = DoorDaihyosenDel
	remove.SubResults = []state.SubMatchResult{}
	return remove
}

// historyOf is the entries of one door, in the order they were written.
func historyOf(entries []state.MatchHistoryEntry, door string) []state.MatchHistoryEntry {
	var out []state.MatchHistoryEntry
	for _, e := range entries {
		if e.Door == door {
			out = append(out, e)
		}
	}
	return out
}

// J. A removal that is OLDER than a representative picked after it: in stamp
// order the bout was removed first and the pick came after, onto a bout that
// no longer existed. Arriving second, the pick is held (the no-row hold, see
// TestMerge_RepPick_OnARemovedBoutIsHeld). Arriving first, it must end in the
// same place: no row, the pick kept in the history under its own stamp with its
// id, and the pick dated by the removal, which is the last thing that happened
// to the row. Before this the removal held the pick with ITS empty value, the
// row went with the removal, and the newer pick was recorded nowhere.
func TestMerge_RepBoutRemove_OlderThanAStoredPick_MovesThePickToTheHistory(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		seedPick(t, h)
		require.NoError(t, h.write(repWrite(h, mmT3, repRow("dana", "", nil, nil), repPickAName)))
		require.Equal(t, "dana", h.load(t).SubResults[0].SideAMemberID, "setup: the newer pick is stored")

		remove := repRemoval(h, mmT2)
		require.NoError(t, h.write(remove), "the removal applies: it is newer than the bout")

		got := h.load(t)
		assert.Empty(t, got.SubResults, "the row is gone")
		assert.Equal(t, mmT2, got.GroupStamp(repPickAName), "the removal dates the picks, as it does when it comes first")
		assert.Equal(t, mmT2, got.GroupStamp(repPickBName))
		assert.Equal(t, mmT2, got.ModifiedAt, "the newest stamp the match still holds")

		rep := remove.Merge
		require.NotNil(t, rep)
		assert.Equal(t, []string{repPickAName}, rep.DisplacedGroups(), "the answer names the pick moved to the history")
		assert.Empty(t, rep.HeldGroups(), "nothing of the removal was held")
		assert.Empty(t, rep.HeldReason(), "no needs_winner: the pick lost its bout, not its winner")
		assert.False(t, rep.NeedsWinner)

		entries := h.history(t)
		own := historyOf(entries, DoorDaihyosenDel)
		require.Len(t, own, 1)
		assert.Equal(t, state.HistoryOutcomeApplied, own[0].Outcomes[repPickAName], "the removal's own entry applies the pick's clear")
		assert.NotContains(t, own[0].Held, repPickAName, "and holds no empty value in its name")

		moved := historyOf(entries, DoorDisplaced)
		require.Len(t, moved, 1)
		assert.Equal(t, mmT3, moved[0].Stamp, "at the pick's own stamp")
		assert.Equal(t, state.HistoryOutcomeHeld, moved[0].Outcomes[repPickAName])
		assert.JSONEq(t, `{"sideAMemberId":"dana"}`, string(moved[0].Held[repPickAName]), "with its id")
		assert.Equal(t, HoldReasonRepBoutRemoved, moved[0].Reason)
	})
}

// J, both arrival orders: (remove at T2, pick at T3) ends in one stored state
// and one history content however they arrive.
func TestMerge_RepBoutRemove_BothArrivalOrdersConverge(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		type outcome struct {
			Rows      []state.SubMatchResult
			StampA    int64
			StampB    int64
			Modified  int64
			HeldPicks []string
		}
		run := func(removeFirst bool) outcome {
			h := mmTeam(t, knockout)
			seedPick(t, h)
			pick := repWrite(h, mmT3, repRow("dana", "", nil, nil), repPickAName)
			remove := repRemoval(h, mmT2)
			if removeFirst {
				require.NoError(t, h.write(remove))
				require.ErrorIs(t, h.write(pick), ErrMatchSuperseded, "the pick lands on no bout")
			} else {
				require.NoError(t, h.write(pick))
				require.NoError(t, h.write(remove))
			}
			got := h.load(t)
			out := outcome{
				Rows: got.SubResults, StampA: got.GroupStamp(repPickAName), StampB: got.GroupStamp(repPickBName),
				Modified: got.ModifiedAt,
			}
			for _, e := range h.history(t) {
				if v, ok := e.Held[repPickAName]; ok {
					out.HeldPicks = append(out.HeldPicks, string(v))
				}
			}
			return out
		}
		inOrder, reversed := run(true), run(false)
		assert.Empty(t, inOrder.Rows)
		assert.Equal(t, []string{`{"sideAMemberId":"dana"}`}, inOrder.HeldPicks, "the pick is in the history")
		assert.Equal(t, inOrder, reversed, "rows, stamps and the picks kept in the history match")
	})
}

// J with both sides: picks stamped alike share one history entry, each at its
// own stamp otherwise, oldest first; each side is dated by the removal.
func TestMerge_RepBoutRemove_OlderThanBothPicks_OneEntryPerStamp(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		seedPick(t, h)
		require.NoError(t, h.write(repWrite(h, mmT3, repRow("dana", "gus", nil, nil), repPickAName, repPickBName)))
		require.NoError(t, h.write(repWrite(h, mmT4, repRow("dana", "hal", nil, nil), repPickBName)))

		remove := repRemoval(h, mmT2)
		require.NoError(t, h.write(remove))

		assert.Equal(t, []string{repPickAName, repPickBName}, remove.Merge.DisplacedGroups())
		moved := historyOf(h.history(t), DoorDisplaced)
		require.Len(t, moved, 2, "side A is the pick at T3, side B the one at T4")
		assert.Equal(t, mmT3, moved[0].Stamp)
		assert.JSONEq(t, `{"sideAMemberId":"dana"}`, string(moved[0].Held[repPickAName]))
		assert.NotContains(t, moved[0].Held, repPickBName, "side B's pick was replaced at T4")
		assert.Equal(t, mmT4, moved[1].Stamp)
		assert.JSONEq(t, `{"sideBMemberId":"hal"}`, string(moved[1].Held[repPickBName]))
		got := h.load(t)
		assert.Empty(t, got.SubResults)
		assert.Equal(t, mmT2, got.GroupStamp(repPickAName))
		assert.Equal(t, mmT2, got.GroupStamp(repPickBName))
		assert.Equal(t, mmT2, got.ModifiedAt)

		// Both picks made in one write share its stamp: one entry.
		h2 := mmTeam(t, knockout)
		seedPick(t, h2)
		require.NoError(t, h2.write(repWrite(h2, mmT3, repRow("dana", "gus", nil, nil), repPickAName, repPickBName)))
		require.NoError(t, h2.write(repRemoval(h2, mmT2)))
		one := historyOf(h2.history(t), DoorDisplaced)
		require.Len(t, one, 1)
		assert.Equal(t, mmT3, one[0].Stamp)
		assert.Equal(t, []string{repPickAName, repPickBName}, one[0].Changed)
	})
}

// A removal refused for the BOUT (a point on it, made after the removal, is
// stored) is refused whole for the representatives: a pick stays on the row that
// stays. Before this the pick's older stamp let the removal's empty pick apply, so
// its history entry called the pick cleared and its stamp moved, while the row
// kept the pick.
func TestMerge_RepBoutRemove_RefusedForTheBout_HoldsItsPicksWithIt(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		seedPick(t, h) // carol at T1
		require.NoError(t, h.write(repWrite(h, mmT3, repRow("", "", []string{"M"}, nil), repBoutGroup)))

		remove := repRemoval(h, mmT2)
		err := h.write(remove)
		require.ErrorIs(t, err, ErrMatchSuperseded, "nothing of the removal applies")
		assert.Contains(t, HeldGroupsOf(err), repBoutGroup)
		assert.Contains(t, HeldGroupsOf(err), repPickAName, "the pick is held with the bout")

		got := h.load(t)
		require.Len(t, got.SubResults, 1, "the row stays")
		assert.Equal(t, "carol", got.SubResults[0].SideAMemberID, "with its pick")
		assert.Equal(t, []string{"M"}, got.SubResults[0].IpponsA, "and its point")
		assert.Equal(t, mmT1, remove.GroupStamps[repPickAName], "the pick keeps its date: the removal did not touch it")

		entries := h.history(t)
		last := entries[len(entries)-1]
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[repPickAName])
		assert.JSONEq(t, `{"sideAMemberId":""}`, string(last.Held[repPickAName]), "the clear that was not applied")
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[repBoutGroup])
	})
}

// K. A pick whose bout a LATER hold took out of the write has no bout to land
// on either. A running write made after a knockout's finish names the bout and a
// pick and ties the scoreline: R4 holds the bout (the stored match has no
// representative row to put it on), and the pick the row carried must go with
// it, held with its id, not left "applied" onto nothing with its stamp moved.
func TestMerge_RepPick_OnARowAHoldTookOut_IsHeldWithItsValue(t *testing.T) {
	finished := func(t *testing.T) mmHome {
		h := mmTeam(t, true)
		done := mmRunning(h, mmT1)
		done.Changed = nil
		done.Status = state.MatchStatusCompleted
		done.IpponsA, done.IpponsB = []string{"M"}, []string{}
		done.Winner = wrTeamA
		require.NoError(t, h.write(done))
		return h
	}
	tying := func(h mmHome, changed ...string) *state.MatchResult {
		w := repWrite(h, mmT2, repRow("eve", "", nil, nil), append([]string{repBoutGroup, repPickAName, state.GroupPoints}, changed...)...)
		w.IpponsA, w.IpponsB = []string{"M"}, []string{"K"}
		return w
	}

	t.Run("the whole write is held", func(t *testing.T) {
		h := finished(t)
		late := tying(h)
		err := h.write(late)
		require.ErrorIs(t, err, ErrMatchSuperseded)
		assert.ElementsMatch(t, []string{state.GroupPoints, repBoutGroup, repPickAName}, HeldGroupsOf(err))
		assert.NotContains(t, late.Merge.Applied, repPickAName, "never applied onto nothing")
		assert.Equal(t, HoldReasonKnockoutNeedsWinner, late.Merge.HoldReason)

		last := h.history(t)
		last0 := last[len(last)-1]
		assert.Equal(t, state.HistoryOutcomeHeld, last0.Outcomes[repPickAName])
		assert.JSONEq(t, `{"sideAMemberId":"eve"}`, string(last0.Held[repPickAName]), "the pick's id is kept")
		got := h.load(t)
		assert.Empty(t, got.SubResults)
		assert.Zero(t, got.GroupStamps[repPickAName], "and its stamp is the stored one")
	})
	t.Run("a write applied in part moves no stamp of the pick", func(t *testing.T) {
		h := finished(t)
		late := tying(h, state.GroupEncho)
		late.Encho = &state.EnchoMetadata{PeriodCount: 1}
		require.NoError(t, h.write(late), "the overtime applies")
		assert.NotContains(t, late.Merge.Applied, repPickAName)
		assert.Contains(t, late.Merge.Held, repPickAName)
		assert.JSONEq(t, `{"sideAMemberId":"eve"}`, string(late.Merge.HeldValues[repPickAName]))
		got := h.load(t)
		assert.Empty(t, got.SubResults)
		assert.NotContains(t, got.GroupStamps, repPickAName, "the stored match never stamped the pick")
	})
}

// The pick judges (mobileapp: the participant's and the organiser's) judge a
// representative pick for team membership only when the merge will APPLY it,
// and they ask RepPicksApplied, which runs the merge on copies, rather than
// re-deriving its rule. The table below pins the probe to the merge itself.

const rpaStoredAt = int64(100_000)

var rpaComp = &state.Competition{
	ID: "rpa", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed, Format: state.CompFormatKnockout,
}

// rpaStored is a stored running team match; withRow adds the representative
// bout with side A's pick (carol) stamped at rpaStoredAt on all three groups.
func rpaStored(withRow bool) *state.MatchResult {
	m := &state.MatchResult{
		ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning, ModifiedAt: rpaStoredAt,
		SubResults:  []state.SubMatchResult{{Position: 1}},
		GroupStamps: map[string]int64{"bout:1": rpaStoredAt},
	}
	if withRow {
		m.SubResults = append(m.SubResults, repRow("carol", "", nil, nil))
		m.GroupStamps[repBoutGroup], m.GroupStamps[repPickAName], m.GroupStamps[repPickBName] = rpaStoredAt, rpaStoredAt, rpaStoredAt
	}
	return m
}

// rpaWrite is a client's running score write that picks dave and erin.
func rpaWrite(at int64, changed []string) *state.MatchResult {
	return &state.MatchResult{
		ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning, ModifiedAt: at, Changed: changed, WriteDoor: DoorScore,
		SubResults: []state.SubMatchResult{repRow("dave", "erin", nil, nil)},
	}
}

// rpaCopy is a deep copy built without the probe's own cloning.
func rpaCopy(m *state.MatchResult) state.MatchResult {
	c := *m
	c.SubResults = state.CloneSubResults(m.SubResults)
	c.GroupStamps = state.CloneGroupStamps(m.GroupStamps)
	if m.Changed != nil {
		c.Changed = append([]string{}, m.Changed...)
	}
	return c
}

func TestRepPicksApplied_AgreesWithTheMerge(t *testing.T) {
	changeds := []struct {
		name    string
		changed []string
	}{
		{"naming nothing", nil},
		{"naming both picks and the bout", []string{repBoutGroup, repPickAName, repPickBName}},
		{"naming both picks", []string{repPickAName, repPickBName}},
		{"naming side A's pick", []string{repPickAName}},
		{"naming side B's pick", []string{repPickBName}},
		{"naming the bout alone", []string{repBoutGroup}},
	}
	stamps := []struct {
		name string
		at   int64
	}{
		{"older", rpaStoredAt - 100}, {"equal", rpaStoredAt}, {"newer", rpaStoredAt + 100}, {"unstamped", 0},
	}
	for _, knockout := range []bool{false, true} {
		for _, storedRow := range []bool{true, false} {
			for _, ch := range changeds {
				for _, st := range stamps {
					name := fmt.Sprintf("knockout=%v/storedRow=%v/%s/%s", knockout, storedRow, ch.name, st.name)
					t.Run(name, func(t *testing.T) {
						stored, incoming := rpaStored(storedRow), rpaWrite(st.at, ch.changed)
						storedBefore, incomingBefore := rpaCopy(stored), rpaCopy(incoming)

						gotA, gotB := RepPicksApplied(stored, incoming, rpaComp, knockout)

						assert.Equal(t, storedBefore, *stored, "the stored match is not changed by the probe")
						assert.Equal(t, incomingBefore, *incoming, "the write is not changed by the probe")

						// The merge, run on its own copies with the branch's context.
						s, w := rpaCopy(stored), rpaCopy(incoming)
						rep := mergeMatchWrite(&s, &w, matchWriteForward, mergeCtx{comp: rpaComp, knockout: knockout, nilSubsClear: !knockout})
						assert.Equal(t, slices.Contains(rep.Applied, repPickAName), gotA, "side A")
						assert.Equal(t, slices.Contains(rep.Applied, repPickBName), gotB, "side B")

						// And the answer is the plain reading of the rule, so the
						// equality above cannot hold only because both are wrong:
						// the write names the pick, it is not older than the stored
						// pick's stamp, and it lands on a row (the match's own, or
						// one the write creates by naming the bout: a pick named
						// onto a row nothing creates is held).
						applies := func(group string) bool {
							named := ch.changed == nil || slices.Contains(ch.changed, group)
							onARow := storedRow || ch.changed == nil || slices.Contains(ch.changed, repBoutGroup)
							return named && onARow && (st.at == 0 || !storedRow || st.at >= rpaStoredAt)
						}
						assert.Equal(t, applies(repPickAName), gotA, "side A by the rule")
						assert.Equal(t, applies(repPickBName), gotB, "side B by the rule")
					})
				}
			}
		}
	}
}

// A finish stamped before the stored verdict that names another winner is held
// WHOLE (HoldReasonFinishAtomic): its picks land nowhere, whatever their own
// stamps say. A finish that only repeats the stored verdict is not a hold, so
// its picks are ordered by their own stamps.
func TestRepPicksApplied_AStaleFinishLandsNoPick(t *testing.T) {
	stored := func() *state.MatchResult {
		m := rpaStored(true)
		m.Status, m.Winner, m.WinnerID = state.MatchStatusCompleted, wrTeamA, wrTeamAID
		m.GroupStamps[state.GroupResult] = rpaStoredAt
		m.GroupStamps[repBoutGroup], m.GroupStamps[repPickAName], m.GroupStamps[repPickBName] = rpaStoredAt-40, rpaStoredAt-50, rpaStoredAt-50
		return m
	}
	finish := func(winner, winnerID string) *state.MatchResult {
		w := rpaWrite(rpaStoredAt-20, nil)
		w.Status, w.Winner, w.WinnerID = state.MatchStatusCompleted, winner, winnerID
		return w
	}
	for _, knockout := range []bool{false, true} {
		t.Run(fmt.Sprintf("knockout=%v", knockout), func(t *testing.T) {
			a, b := RepPicksApplied(stored(), finish(wrTeamB, wrTeamBID), rpaComp, knockout)
			assert.False(t, a, "another winner, older than the stored verdict: held whole, side A")
			assert.False(t, b, "side B")

			a, b = RepPicksApplied(stored(), finish(wrTeamA, wrTeamAID), rpaComp, knockout)
			assert.True(t, a, "the stored verdict repeated is no hold: the pick is newer than its stored stamp, side A")
			assert.True(t, b, "side B")
		})
	}
}
