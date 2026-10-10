package engine

// bc-mrgc, operator ruling: "every change is dated and ordered; a change must
// only ever apply what it changed." The two representatives a team match's
// representative bout is fought by (SideAMemberID/SideBMemberID on the row at
// position -1) are their own dated change, state.GroupRepPicks, ordered apart
// from the bout's score and result: a point never alters the picks, a pick
// never alters the point, and an older change of either is kept in the
// match's history.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

const (
	repBoutGroup = "bout:-1"
	repPicksName = state.GroupRepPicks
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
	require.NoError(t, h.write(repWrite(h, mmT1, repRow("carol", "", nil, nil), repBoutGroup, repPicksName)))
	got := h.load(t)
	require.Len(t, got.SubResults, 1)
	require.Equal(t, "carol", got.SubResults[0].SideAMemberID, "setup: the pick is stored")
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

		require.NoError(t, h.write(repWrite(h, mmT2, repRow("carol", "", nil, nil), repPicksName)))

		row := h.load(t).SubResults[0]
		assert.Equal(t, "carol", row.SideAMemberID, "the pick applies")
		assert.Equal(t, []string{"M"}, row.IpponsA, "the point is not the pick's to erase")
	})
}

// A stale pick is held and kept in the history; a newer clear clears.
func TestMerge_RepPick_StaleIsHeldAndANewerClearClears(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		require.NoError(t, h.write(repWrite(h, mmT2, repRow("carol", "", nil, nil), repBoutGroup, repPicksName)))

		err := h.write(repWrite(h, mmT1, repRow("dana", "", nil, nil), repPicksName))
		require.ErrorIs(t, err, ErrMatchSuperseded, "its one change is outranked")
		assert.Equal(t, []string{repPicksName}, HeldGroupsOf(err))
		assert.Equal(t, "carol", h.load(t).SubResults[0].SideAMemberID, "the newer pick stands")
		entries := h.history(t)
		held := entries[len(entries)-1]
		assert.Equal(t, state.HistoryOutcomeHeld, held.Outcomes[repPicksName])
		assert.JSONEq(t, `{"sideAMemberId":"dana","sideBMemberId":""}`, string(held.Held[repPicksName]),
			"the older pick is kept, never discarded")

		// A deliberate clear is a change to the pick: the row is sent without
		// the id and the change is named.
		require.NoError(t, h.write(repWrite(h, mmT3, repRow("", "", nil, nil), repPicksName)))
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
	require.NoError(t, h.write(repWrite(h, mmT3, repRow("carol", "", nil, nil), repBoutGroup, repPicksName)))

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
	require.NoError(t, h.write(repWrite(h, mmT1, won, repBoutGroup, repPicksName)))

	require.NoError(t, h.write(repWrite(h, mmT2, repRow("dana", "", nil, nil), repPicksName)))

	row := h.load(t).SubResults[0]
	assert.Equal(t, "dana", row.SideAMemberID)
	assert.Equal(t, wrTeamA, row.Winner, "the bout's result is the point's, untouched")
	assert.Equal(t, "dana", row.WinnerMemberID, "re-derived from the winning side and the stored pick")

	// The pick cleared under a recorded winner id leaves no dangling id.
	require.NoError(t, h.write(repWrite(h, mmT3, repRow("", "", nil, nil), repPicksName)))
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
			require.NoError(t, h.write(repWrite(h, mmT1, won, repBoutGroup, repPicksName)))
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
			require.NoError(t, h.write(repWrite(h, mmT1, won, repBoutGroup, repPicksName)))

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
				require.NoError(t, h.write(repWrite(h, mmT1, won, repBoutGroup, repPicksName)))
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
	remove := mmRunning(h, mmT2, repBoutGroup, repPicksName)
	remove.WriteDoor = DoorDaihyosenDel
	remove.SubResults = []state.SubMatchResult{}
	require.NoError(t, h.write(remove))
	require.Empty(t, h.load(t).SubResults)
	removed := h.load(t)
	assert.Equal(t, mmT2, removed.GroupStamp(repPicksName), "the removal dates the picks too")

	late := repWrite(h, mmT3, repRow("dana", "", nil, nil), repPicksName)
	err := h.write(late)
	require.ErrorIs(t, err, ErrMatchSuperseded)
	assert.Equal(t, []string{repPicksName}, HeldGroupsOf(err))
	assert.Empty(t, h.load(t).SubResults, "nothing is resurrected")
}

// A match written before the picks had a stamp of their own is ordered by the
// stamp of its representative bout, as it always was.
func TestMerge_RepPicks_LegacyStampFallsBackToTheBout(t *testing.T) {
	h := mmTeam(t, true)
	bracket, err := h.store.LoadBracket(h.compID)
	require.NoError(t, err)
	bm := &bracket.Rounds[0][0]
	bm.SubResults = []state.SubMatchResult{repRow("carol", "", nil, nil)}
	bm.ModifiedAt = mmT2
	bm.GroupStamps = map[string]int64{repBoutGroup: mmT2} // a map written before the picks had an entry
	require.NoError(t, h.store.SaveBracket(h.compID, bracket))

	err = h.write(repWrite(h, mmT1, repRow("dana", "", nil, nil), repPicksName))
	require.ErrorIs(t, err, ErrMatchSuperseded, "older than the row it sits on")
	assert.Equal(t, "carol", h.load(t).SubResults[0].SideAMemberID)

	require.NoError(t, h.write(repWrite(h, mmT3, repRow("dana", "", nil, nil), repPicksName)))
	assert.Equal(t, "dana", h.load(t).SubResults[0].SideAMemberID)
}

// A legacy match (no stamp map) that holds no representative bout row is not
// given a repPicks stamp by an ordinary write: the materialization that runs
// before the first group is stamped adds the key only for a match that has the
// row to date. Individual matches never have one.
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
		assert.NotContains(t, got.GroupStamps, repPicksName)
	})
}
