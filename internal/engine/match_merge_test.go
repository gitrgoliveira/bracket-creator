package engine

// bc-mrgc, operator ruling 2026-10-03: "Nothing should be dropped. All events
// must be ordered." These pin the merge owner (mergeMatchWrite) through the
// real write doors on both store branches: a write applies each group it
// changes only when its stamp is not older than that group's stored stamp,
// never overwrites a group it does not change, keeps what it could not apply
// in the match's history, and rides R2/R3/R4 on top.

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

const (
	mmT0 = int64(1_800_000_000_000) // the match started
	mmT1 = mmT0 + 60_000
	mmT2 = mmT0 + 120_000
	mmT3 = mmT0 + 180_000
	mmT4 = mmT0 + 240_000
	mmT5 = mmT0 + 300_000
)

// mmHome is one match under test, on one of the two store branches.
type mmHome struct {
	eng     *Engine
	store   *state.Store
	compID  string
	matchID string
	dir     string
}

func (h mmHome) load(t *testing.T) state.MatchResult {
	t.Helper()
	m, err := h.eng.lookupExistingResult(h.store, h.compID, h.matchID)
	require.NoError(t, err)
	return *m
}

func (h mmHome) write(result *state.MatchResult) error {
	_, err := h.eng.RecordMatchResultWithIneligibility(h.compID, h.matchID, result)
	return err
}

func (h mmHome) history(t *testing.T) []state.MatchHistoryEntry {
	t.Helper()
	entries, err := h.store.LoadMatchHistory(h.compID, h.matchID)
	require.NoError(t, err)
	return entries
}

// mmIndividual seeds an individual Ryu v Tora match, running since mmT0, in a
// league (pool branch) or a two-round knockout (bracket branch, whose winner
// advances into m-r2-0).
func mmIndividual(t *testing.T, knockout bool) mmHome {
	t.Helper()
	eng, store, dir := setupTestEngine(t)
	h := mmHome{eng: eng, store: store, dir: dir}
	running := state.MatchStatusRunning
	if knockout {
		h.compID, h.matchID = "mm-ko", "m-r1-0"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: h.compID, Name: "mm", Kind: "individual", Status: state.CompStatusKnockout}))
		wrSaveTeams(t, store, h.compID)
		require.NoError(t, store.SaveBracket(h.compID, &state.Bracket{Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID, Status: running, ModifiedAt: mmT0, MatchNumber: 1},
				{ID: "m-r1-1", SideA: wrTeamC, SideAID: wrTeamCID, Status: state.MatchStatusCompleted, Winner: wrTeamC, WinnerID: wrTeamCID, MatchNumber: 2},
			},
			{
				{ID: "m-r2-0", SideA: winnerOfPlaceholder(2, 0), SideB: wrTeamC, SideBID: wrTeamCID, MatchNumber: 3},
			},
		}}))
		return h
	}
	h.compID, h.matchID = "mm-pool", "Pool A-0"
	createTestCompetition(t, store, h.compID, "league", 3, func(c *state.Competition) { c.Status = state.CompStatusPools })
	wrSaveTeams(t, store, h.compID)
	require.NoError(t, store.SavePoolMatches(h.compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: running, ModifiedAt: mmT0,
	}}))
	return h
}

// mmTeam seeds a fixed-order team match (3 bouts), running since mmT0, on
// either branch.
func mmTeam(t *testing.T, knockout bool) mmHome {
	t.Helper()
	eng, store, dir := setupTestEngine(t)
	h := mmHome{eng: eng, store: store, dir: dir}
	team := func(c *state.Competition) { c.Kind = "team"; c.TeamSize = 3 }
	if knockout {
		h.compID, h.matchID = "mm-team-ko", "m-r1-0"
		c := &state.Competition{ID: h.compID, Name: "mm", Status: state.CompStatusKnockout}
		team(c)
		require.NoError(t, store.SaveCompetition(c))
		wrSaveTeams(t, store, h.compID)
		require.NoError(t, store.SaveBracket(h.compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
			ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusRunning, ModifiedAt: mmT0, MatchNumber: 1,
		}}}}))
		return h
	}
	h.compID, h.matchID = "mm-team-pool", "Pool A-0"
	createTestCompetition(t, store, h.compID, "league", 3, func(c *state.Competition) { team(c); c.Status = state.CompStatusPools })
	wrSaveTeams(t, store, h.compID)
	require.NoError(t, store.SavePoolMatches(h.compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning, ModifiedAt: mmT0,
	}}))
	return h
}

// mmRunning is a running write from a board, naming the groups it changes.
func mmRunning(h mmHome, at int64, changed ...string) *state.MatchResult {
	return &state.MatchResult{
		ID: h.matchID, SideA: wrTeamA, SideB: wrTeamB,
		Status: state.MatchStatusRunning, ModifiedAt: at, Changed: changed, WriteDoor: DoorScore,
	}
}

func bothBranches(t *testing.T, run func(t *testing.T, knockout bool)) {
	t.Run("pool", func(t *testing.T) { run(t, false) })
	t.Run("knockout", func(t *testing.T) { run(t, true) })
}

// Two writes about different groups both land, whatever order they arrive in:
// the overtime set on one board, a point on another.
func TestMerge_DifferentGroupsBothLand(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		for _, order := range []string{"older first", "newer first"} {
			t.Run(order, func(t *testing.T) {
				h := mmIndividual(t, knockout)
				point := mmRunning(h, mmT1, state.GroupPoints)
				point.IpponsA = []string{"M"}
				encho := mmRunning(h, mmT2, state.GroupEncho)
				encho.Encho = &state.EnchoMetadata{PeriodCount: 1}
				writes := []*state.MatchResult{point, encho}
				if order == "newer first" {
					writes = []*state.MatchResult{encho, point}
				}
				for _, w := range writes {
					require.NoError(t, h.write(w))
				}
				m := h.load(t)
				assert.Equal(t, []string{"M"}, m.IpponsA, "the point landed")
				assert.True(t, m.Encho.On(), "the overtime landed")
				assert.Equal(t, mmT1, m.GroupStamp(state.GroupPoints))
				assert.Equal(t, mmT2, m.GroupStamp(state.GroupEncho))
				assert.Equal(t, mmT2, m.ModifiedAt, "ModifiedAt stays the newest group stamp")
			})
		}
	})
}

// Bout 2 scored on one board and bout 3 on another both land on a team
// match, in either arrival order.
func TestMerge_DifferentBoutsBothLand(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		for _, order := range []string{"older first", "newer first"} {
			t.Run(order, func(t *testing.T) {
				h := mmTeam(t, knockout)
				b2 := mmRunning(h, mmT1, state.BoutGroup(2))
				b2.SubResults = []state.SubMatchResult{{Position: 2, SideA: "r2", SideB: "t2", Winner: "r2", IpponsA: []string{"K"}}}
				b3 := mmRunning(h, mmT2, state.BoutGroup(3))
				b3.SubResults = []state.SubMatchResult{{Position: 3, SideA: "r3", SideB: "t3", Winner: "t3", IpponsB: []string{"D"}}}
				writes := []*state.MatchResult{b2, b3}
				if order == "newer first" {
					writes = []*state.MatchResult{b3, b2}
				}
				for _, w := range writes {
					require.NoError(t, h.write(w))
				}
				m := h.load(t)
				require.Len(t, m.SubResults, 2, "both bouts are stored")
				assert.Equal(t, 2, m.SubResults[0].Position)
				assert.Equal(t, []string{"K"}, m.SubResults[0].IpponsA)
				assert.Equal(t, 3, m.SubResults[1].Position)
				assert.Equal(t, []string{"D"}, m.SubResults[1].IpponsB)
			})
		}
	})
}

// A clash on the same group is ordered by stamp: the newer stays and the
// older is kept in the match's history, with its values.
func TestMerge_SameGroupClashKeepsTheOlderInHistory(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		newer := mmRunning(h, mmT2, state.GroupPoints)
		newer.IpponsA = []string{"M", "K"}
		require.NoError(t, h.write(newer))

		older := mmRunning(h, mmT1, state.GroupPoints)
		older.IpponsA = []string{"D"}
		err := h.write(older)
		require.ErrorIs(t, err, ErrMatchSuperseded, "its one change is held, so nothing of it applies")
		assert.Equal(t, []string{state.GroupPoints}, HeldGroupsOf(err))

		m := h.load(t)
		assert.Equal(t, []string{"M", "K"}, m.IpponsA, "the newer point stands")
		entries := h.history(t)
		require.Len(t, entries, 2, "one entry per write, applied or not")
		assert.Equal(t, state.HistoryOutcomeApplied, entries[0].Outcomes[state.GroupPoints])
		held := entries[1]
		assert.Equal(t, mmT1, held.Stamp)
		assert.Equal(t, DoorScore, held.Door)
		assert.Equal(t, state.HistoryOutcomeHeld, held.Outcomes[state.GroupPoints])
		assert.JSONEq(t, `{"ipponsA":["D"],"ipponsB":[],"hansokuA":0,"hansokuB":0}`, string(held.Held[state.GroupPoints]),
			"the older value is kept, never discarded")
	})
}

// A held group whose value equals the stored one is not a loss (bc-mrgc phase
// 3): a stale decision's "no overtime" over a match with none is neither
// reported as held, nor kept as a held value, nor recorded as held in the
// history entry, while the groups that do differ are.
func TestMerge_EchoOfAHeldGroupIsNotListedAsHeld(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		point := mmRunning(h, mmT2, state.GroupPoints)
		point.IpponsA = []string{"M"}
		require.NoError(t, h.write(point))

		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
		require.ErrorIs(t, err, ErrMatchSuperseded)
		assert.NotContains(t, HeldGroupsOf(err), state.GroupEncho, "an echo of the stored overtime is not a held change")
		assert.Contains(t, HeldGroupsOf(err), state.GroupResult)

		entries := h.history(t)
		last := entries[len(entries)-1]
		assert.Contains(t, last.Changed, state.GroupEncho, "the write still names what it changed")
		assert.Equal(t, state.HistoryOutcomeUnchanged, last.Outcomes[state.GroupEncho], "recorded as unchanged, never as held")
		assert.NotContains(t, last.Held, state.GroupEncho, "no held value is kept for it")
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupResult])
	})
}

// Q1: a kiken made at 10:02 arriving after a point made at 10:05. The point
// is the later fact, so the kiken goes to history unapplied: the match runs
// on with the point and the competitor is never barred. The reverse order (a
// kiken made after the point) applies exactly as before.
func TestMerge_LateKikenOlderThanAPointIsHeld(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		t.Run("a board that sends every group", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			point := mmRunning(h, mmT2) // no `changed`: today's client
			point.Changed = nil
			point.IpponsA = []string{"M"}
			require.NoError(t, h.write(point))

			_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
			require.ErrorIs(t, err, ErrMatchSuperseded)
			// The decision changes result, points and encho; it carries no
			// overtime over a match with none, so encho echoes the stored
			// value and is no loss (bc-mrgc phase 3): not listed as held.
			assert.ElementsMatch(t, []string{state.GroupResult, state.GroupPoints}, HeldGroupsOf(err), "the withdrawal and its circles are held together")

			m := h.load(t)
			assert.Equal(t, state.MatchStatusRunning, m.Status, "the match runs on")
			assert.Equal(t, []string{"M"}, m.IpponsA, "with the point")
			assert.Empty(t, m.Decision)
			assert.True(t, wrEligible(t, h.store, h.compID, wrTeamAID), "the competitor was never barred")
			last := h.history(t)[len(h.history(t))-1]
			assert.Equal(t, DoorDecision, last.Door)
			assert.Contains(t, string(last.Held[state.GroupResult]), `"decision":"kiken-voluntary"`, "the withdrawal is kept in history")
		})
		// The shape a client that names its groups sends (bc-mrgc phase 2): the
		// point alone. The stored verdict is older than the kiken, so only R2's
		// late half can hold it: a withdrawal stands on the scoreline it was
		// declared over.
		t.Run("a board that names only the point", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			point := mmRunning(h, mmT2, state.GroupPoints)
			point.IpponsA = []string{"M"}
			require.NoError(t, h.write(point))

			_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
			require.ErrorIs(t, err, ErrMatchSuperseded)
			m := h.load(t)
			assert.Equal(t, state.MatchStatusRunning, m.Status)
			assert.Equal(t, []string{"M"}, m.IpponsA)
			assert.True(t, wrEligible(t, h.store, h.compID, wrTeamAID))
		})
		// A bout is not a group the decision changes, so only R2's late half
		// sees that one was scored after the kiken was made.
		t.Run("a team board that names only a bout", func(t *testing.T) {
			h := mmTeam(t, knockout)
			bout := mmRunning(h, mmT2, state.BoutGroup(2))
			bout.SubResults = []state.SubMatchResult{{Position: 2, SideA: "r2", SideB: "t2", Winner: "r2", IpponsA: []string{"K"}}}
			require.NoError(t, h.write(bout))

			_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
			require.ErrorIs(t, err, ErrMatchSuperseded, "a withdrawal declared before a bout was scored no longer describes the match")
			m := h.load(t)
			assert.Equal(t, state.MatchStatusRunning, m.Status)
			assert.Empty(t, m.Decision)
			assert.True(t, wrEligible(t, h.store, h.compID, wrTeamAID))
		})
		// The withdrawal is atomic with its circles: when its verdict is held
		// (here a later status change on the board), the default-win points
		// must not land on their own.
		t.Run("a held verdict holds its circles", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			// A write that NAMES the result group takes its own stamp
			// whether or not it actually differs from stored (bc-mrgc
			// Finding 1), so this status write alone is enough to put T2 on
			// the result group and outrank the kiken below; no extra value
			// change is needed to force that (round 3's CorrectionReason
			// workaround, now unnecessary, since an UNNAMED echo is the
			// only shape that still reverts to its stored stamp).
			status := mmRunning(h, mmT2, state.GroupResult)
			require.NoError(t, h.write(status))

			_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
			require.ErrorIs(t, err, ErrMatchSuperseded)
			m := h.load(t)
			assert.Empty(t, m.IpponsB, "no default-win circles without the withdrawal they belong to")
			assert.Empty(t, m.Decision)
		})
		t.Run("a kiken made after the point applies", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			point := mmRunning(h, mmT1)
			point.Changed = nil
			point.IpponsA = []string{"M"}
			require.NoError(t, h.write(point))

			_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT2)
			require.NoError(t, err)
			m := h.load(t)
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
			assert.Equal(t, "kiken-voluntary", m.Decision)
			assert.Equal(t, wrTeamB, m.Winner)
			assert.Equal(t, []string{"M"}, m.IpponsA, "the withdrawer keeps the point it struck (FIK Art. 32)")
			assert.False(t, wrEligible(t, h.store, h.compID, wrTeamAID))
		})
	})
}

// R2: a scoring change made on a board after the withdrawal was recorded
// means the withdrawal was a mistake. It is cleared (and kept in history),
// the competitor is eligible again, and the match stays finished with the
// winner worked out from the points.
func TestMerge_ScoringAfterAWithdrawalClearsIt(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
		require.NoError(t, err)
		require.False(t, wrEligible(t, h.store, h.compID, wrTeamAID), "precondition: Ryu is barred")

		point := mmRunning(h, mmT2)
		point.Changed = nil // today's client: the whole board
		point.IpponsA = []string{"M"}
		point.IpponsB = []string{}
		require.NoError(t, h.write(point))

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "the match stays finished")
		assert.Empty(t, m.Decision, "the withdrawal is cleared")
		assert.Equal(t, wrTeamA, m.Winner, "the winner is worked out from the points")
		assert.Equal(t, wrTeamAID, m.WinnerID)
		assert.True(t, wrEligible(t, h.store, h.compID, wrTeamAID), "eligibility follows the cleared ruling")
		last := h.history(t)[len(h.history(t))-1]
		assert.Contains(t, string(last.ClearedWithdrawal), `"decision":"kiken-voluntary"`, "the withdrawal is kept in history")
	})
}

// R2's clear is for a withdrawal of the match itself. A fusensho closed this
// match because the other side is barred by ANOTHER match; points a board
// still scoring it sends after that say nothing about that bar, so the
// default win stands and the scoring is held, in the history with the reason,
// and no winner is asked for (the match has one).
func TestMerge_ScoringAfterADefaultWinForABarElsewhereIsHeld(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		// Ryu (aka) cannot fight: the default win is Tora's.
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "fusensho", "aka", "", nil, mmT1)
		require.NoError(t, err)
		before := h.load(t)
		require.Equal(t, wrTeamB, before.Winner, "precondition: Tora has the default win")

		point := mmRunning(h, mmT2)
		point.Changed = nil // today's client: the whole board
		point.IpponsA = []string{"M"}
		point.IpponsB = []string{}
		err = h.write(point)
		require.ErrorIs(t, err, ErrMatchSuperseded, "the point is its one change, and it is held")
		assert.Equal(t, []string{state.GroupPoints}, HeldGroupsOf(err))
		assert.Equal(t, state.HeldReasonDefaultWinStands, HeldReasonOf(err), "the default win stands, so no winner is asked for")

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status)
		assert.Equal(t, "fusensho", m.Decision, "the default win stands")
		assert.Equal(t, wrTeamB, m.Winner)
		assert.Equal(t, before.IpponsA, m.IpponsA, "the scoreline is as the default win recorded it")
		assert.Equal(t, before.IpponsB, m.IpponsB)
		last := h.history(t)[len(h.history(t))-1]
		assert.Equal(t, defaultWinStandsReason("fusensho"), last.Reason)
		assert.Empty(t, last.ClearedWithdrawal, "nothing was cleared")
	})
}

// The team twin: the bouts a board changed are held too (every scoring group,
// not the points alone), and the credited rows the default win padded come
// back untouched.
func TestMerge_ScoringAfterATeamDefaultWinForABarElsewhereIsHeld(t *testing.T) {
	eng, store, compID, _ := seedPoolWithdrawal(t, "fusensho")
	require.True(t, wrEligible(t, store, compID, wrTeamAID), "precondition: a fusensho bars nobody on this match")
	h := mmHome{eng: eng, store: store, compID: compID, matchID: "Pool A-0"}
	before := wrPoolMatch(t, store, compID)
	require.Equal(t, "fusensho", before.Decision)
	require.Len(t, before.SubResults, 3, "precondition: the default win padded every position")

	board := mmRunning(h, mmT2)
	board.Changed = nil
	board.SubResults = []state.SubMatchResult{
		wrBout1("M"),
		{Position: 2, SideA: "r2", SideB: "t2", Winner: "t2", IpponsB: []string{"K"}},
		{Position: 3, SideA: "r3", SideB: "t3", Winner: "t3", IpponsB: []string{"D", "M"}},
	}
	board.IpponsA, board.IpponsB = []string{}, []string{}
	err := h.write(board)
	require.ErrorIs(t, err, ErrMatchSuperseded, "nothing of the board applies")
	held := HeldGroupsOf(err)
	assert.Subset(t, held, []string{state.BoutGroup(2), state.BoutGroup(3)}, "every bout the board changed is held")
	assert.NotContains(t, held, state.BoutGroup(1), "an echo of the stored bout is not a held change")
	assert.Equal(t, state.HeldReasonDefaultWinStands, HeldReasonOf(err), "the default win stands, so no winner is asked for")

	m := wrPoolMatch(t, store, compID)
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, "fusensho", m.Decision, "the default win stands")
	assert.Equal(t, before.Winner, m.Winner)
	assert.Equal(t, before.SubResults, m.SubResults, "the rows the default win padded are untouched")
	assert.Equal(t, defaultWinStandsReason("fusensho"), h.history(t)[len(h.history(t))-1].Reason)
}

// bc-mrgc Finding 3 (a regression): holdScoring only holds groups
// IsScoringGroup counts, which excludes overtime, so a running write's
// encho change over a default-win-closed match landed even though its
// points were held with the default win -- an (E) mark on a match FIK Art.
// 32 never puts one on. Both the points and the overtime must be held
// together, with the same reason.
func TestMerge_ScoringAndOvertimeAfterADefaultWinAreBothHeld(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "fusensho", "aka", "", nil, mmT1)
		require.NoError(t, err)
		before := h.load(t)

		board := mmRunning(h, mmT2, state.GroupPoints, state.GroupEncho)
		board.IpponsA = []string{"M"}
		board.Encho = &state.EnchoMetadata{PeriodCount: 1}
		err = h.write(board)
		require.ErrorIs(t, err, ErrMatchSuperseded, "both changes are held with the default win")
		assert.ElementsMatch(t, []string{state.GroupPoints, state.GroupEncho}, HeldGroupsOf(err))
		assert.Equal(t, state.HeldReasonDefaultWinStands, HeldReasonOf(err))

		m := h.load(t)
		assert.Equal(t, "fusensho", m.Decision, "the default win stands")
		assert.Nil(t, m.Encho, "no overtime lands on a default win")
		assert.Equal(t, before.IpponsA, m.IpponsA, "the scoreline is as the default win recorded it")
	})
}

// The encho-only half of Finding 3: a running write that changes NOTHING
// but overtime over a match a withdrawal closed must still be held, with
// the same reason, even though scoringChanged alone would never have seen
// it (no point, bout or flag moved).
func TestMerge_OvertimeAloneAfterAWithdrawalIsHeld(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
		require.NoError(t, err)

		board := mmRunning(h, mmT2, state.GroupEncho)
		board.Encho = &state.EnchoMetadata{PeriodCount: 1}
		err = h.write(board)
		require.ErrorIs(t, err, ErrMatchSuperseded, "an (E) tap alone is not a scoring change, so it cannot clear the withdrawal")
		assert.Equal(t, []string{state.GroupEncho}, HeldGroupsOf(err))
		assert.Equal(t, state.HeldReasonDefaultWinStands, HeldReasonOf(err))

		m := h.load(t)
		assert.Equal(t, "kiken-voluntary", m.Decision, "the withdrawal stands")
		assert.Nil(t, m.Encho, "no overtime lands on a withdrawal that was never cleared")
	})
}

// bc-mrgc fix: a running write whose only named group is overtime, sent
// back with the SAME "no overtime" a default win already recorded, is an
// echo like any other -- it must not be read as a genuine overtime change
// just because GroupEncho sits in rep.Applied. Before the fix, entering the
// R2/R4 block for this echo held the whole write as superseded even though
// nothing about it differed from what was stored.
func TestMerge_EchoedOvertimeOverADefaultWinAppliesAsANoOp(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "fusensho", "aka", "", nil, mmT1)
		require.NoError(t, err)
		before := h.load(t)
		require.False(t, before.Encho.On(), "precondition: no overtime on a default win")

		board := mmRunning(h, mmT2, state.GroupEncho)
		// No Encho set: the same "no overtime" the default win already holds.
		require.NoError(t, h.write(board), "an echo of the stored overtime is a no-op, never a hold")

		m := h.load(t)
		assert.Equal(t, "fusensho", m.Decision, "unaffected")
		assert.False(t, m.Encho.On())
	})
}

// bc-mrgc fix: holdScoring (and holdGroups generally) used to set HoldReason
// and NeedsWinner even when the groups it was asked to hold were never
// actually applied, so nothing was genuinely held. A knockout match whose
// winner was set by hand over a 0-0 scoreline has no scoring group for an
// encho-only write to clash with (encho is not one): the write fully
// applies, yet the history used to record "a knockout match needs a
// winner" for it anyway.
func TestMerge_OvertimeOverAHandSetWinnerAppliesWithNoReason(t *testing.T) {
	h := mmIndividual(t, true)
	applied, err := h.eng.OverrideBracketWinner(h.compID, h.matchID, wrTeamA, mmT1)
	require.NoError(t, err)
	require.True(t, applied)

	board := mmRunning(h, mmT2, state.GroupEncho)
	board.Encho = &state.EnchoMetadata{PeriodCount: 1}
	require.NoError(t, h.write(board), "the overtime change applies on its own")

	m := h.load(t)
	assert.True(t, m.Encho.On())
	assert.Equal(t, wrTeamA, m.Winner, "the hand-set winner stands")
	last := h.history(t)[len(h.history(t))-1]
	assert.Equal(t, state.HistoryOutcomeApplied, last.Outcomes[state.GroupEncho])
	assert.Empty(t, last.Reason, "nothing was actually held, so no reason is recorded")
}

// R2's clear still needs an actual scoring change, and once it has one the
// overtime the same board carries lands with it, exactly as the points do.
func TestMerge_ScoringAndOvertimeTogetherClearAWithdrawal(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
		require.NoError(t, err)
		require.False(t, wrEligible(t, h.store, h.compID, wrTeamAID), "precondition: Ryu is barred")

		board := mmRunning(h, mmT2, state.GroupPoints, state.GroupEncho)
		board.IpponsA = []string{"M"}
		board.IpponsB = []string{}
		board.Encho = &state.EnchoMetadata{PeriodCount: 1}
		require.NoError(t, h.write(board), "the scoring change clears the withdrawal, and the overtime applies with it")

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "the match stays finished")
		assert.Empty(t, m.Decision, "the withdrawal is cleared")
		assert.Equal(t, wrTeamA, m.Winner, "the winner is worked out from the points")
		assert.True(t, m.Encho.On(), "the overtime genuinely played lands with the cleared scoreline")
		assert.True(t, wrEligible(t, h.store, h.compID, wrTeamAID), "eligibility follows the cleared ruling")
	})
}

// bc-mrgc fix: a running write's scoring change that would CLEAR a stored
// withdrawal (R2) but, once cleared, cannot give a KNOCKOUT match the
// winner it needs (the new scoreline ties) used to hold only the scoring
// groups through holdScoring: the overtime landed beside the kiken that
// still stood, and the answer told the operator to "correct the result
// with a winner" even though the kiken already gives it one. The decision
// must stand instead, exactly as a write that never tried to clear it,
// holding the scoring AND the overtime together.
func TestMerge_ClearingAWithdrawalThatWouldTieAKnockoutLeavesItStanding(t *testing.T) {
	h := mmIndividual(t, true)
	_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
	require.NoError(t, err)
	before := h.load(t)

	board := mmRunning(h, mmT2, state.GroupPoints, state.GroupEncho)
	board.IpponsA = []string{"M"}
	board.IpponsB = []string{"K"}
	board.Encho = &state.EnchoMetadata{PeriodCount: 1}
	err = h.write(board)
	require.ErrorIs(t, err, ErrMatchSuperseded, "the tie cannot clear a knockout's kiken, so it stands")
	assert.ElementsMatch(t, []string{state.GroupPoints, state.GroupEncho}, HeldGroupsOf(err))
	assert.Equal(t, state.HeldReasonDefaultWinStands, HeldReasonOf(err), "the kiken already gives the match its winner; no correction is asked for")
	assert.Equal(t, "kiken-voluntary", HeldDecisionOf(err))

	m := h.load(t)
	assert.Equal(t, "kiken-voluntary", m.Decision, "the withdrawal stands")
	assert.Equal(t, wrTeamB, m.Winner, "unchanged")
	assert.Nil(t, m.Encho, "no overtime lands beside a withdrawal that stands")
	assert.Equal(t, before.IpponsA, m.IpponsA, "the scoreline is as the withdrawal recorded it")
	assert.Equal(t, before.IpponsB, m.IpponsB)
	last := h.history(t)[len(h.history(t))-1]
	assert.Equal(t, defaultWinStandsReason("kiken-voluntary"), last.Reason)
	assert.Empty(t, last.ClearedWithdrawal, "nothing was actually cleared")
}

// The pool twin: the same tie clears the kiken and settles as a completed
// draw (R4's pool rule), with the overtime applying alongside it -- existing
// behaviour, unaffected by the knockout fix above.
func TestMerge_ClearingAWithdrawalThatTiesAPoolMatchBecomesADraw(t *testing.T) {
	h := mmIndividual(t, false)
	_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
	require.NoError(t, err)

	board := mmRunning(h, mmT2, state.GroupPoints, state.GroupEncho)
	board.IpponsA = []string{"M"}
	board.IpponsB = []string{"K"}
	board.Encho = &state.EnchoMetadata{PeriodCount: 1}
	require.NoError(t, h.write(board), "the tie clears the kiken and settles as a draw")

	m := h.load(t)
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, state.DecisionDraw, m.Decision)
	assert.Empty(t, m.Winner)
	assert.True(t, m.Encho.On(), "the overtime applies with the cleared scoreline")
}

// A verdict that applies but does not move (the same withdrawal sent again
// under a later stamp: another device's copy, a correction restating it) has
// no eligibility consequence: recording it again would bar once more a
// competitor reinstated since.
func TestMerge_SameDecisionSentAgainLeavesAReinstatement(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-injury", "aka", "knee", nil, mmT1)
		require.NoError(t, err)
		require.False(t, wrEligible(t, h.store, h.compID, wrTeamAID), "precondition: Ryu is barred")
		_, err = h.eng.ReinstateCompetitor(h.compID, wrTeamAID)
		require.NoError(t, err)
		require.True(t, wrEligible(t, h.store, h.compID, wrTeamAID), "precondition: Ryu is reinstated")

		_, status, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-injury", "aka", "knee", nil, mmT2)
		require.NoError(t, err)
		assert.Nil(t, status, "a verdict that did not move records no status")
		assert.True(t, wrEligible(t, h.store, h.compID, wrTeamAID), "the reinstatement stands")
		m := h.load(t)
		assert.Equal(t, "kiken-injury", m.Decision)
		// A decision always NAMES its groups (decisionChangedGroups), so even
		// though this echo moved nothing, it is still a real action taken at
		// T2 (bc-mrgc Finding 1): only a write that names NO groups falls
		// back to the stamp it already held.
		assert.Equal(t, mmT2, m.ModifiedAt, "a named echo still takes the stamp of the write that repeated it")
	})
}

// bc-mrgc Finding 1 (a regression in the previous round): a write that NAMES
// a group it changes took that stamp only when the value actually differed
// from stored; an echo of the same value reverted to the stored stamp as if
// the write had never named anything, exactly like a whole-match re-send.
// That is wrong for a NAMED write: the operator's screen showed the old
// value and they acted on it, so the echo is still a real action taken at
// its own time. Three writes land in this order: device A strikes a point
// at T1; device B, whose screen still showed none, taps the SAME point at
// T3 and sends it back (an echo, but a named one); device A's own removal,
// made at T2 -- before B's tap -- but delayed in flight, arrives last. In
// real time the removal is now stale and must be held, leaving the point
// B (re-)struck at T3 standing.
func TestMerge_NamedEchoTakesItsOwnStamp(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		first := mmRunning(h, mmT1, state.GroupPoints)
		first.IpponsA = []string{"M"}
		require.NoError(t, h.write(first))

		echo := mmRunning(h, mmT3, state.GroupPoints)
		echo.IpponsA = []string{"M"}
		require.NoError(t, h.write(echo), "an echo of the stored point, but named")
		afterEcho := h.load(t)
		assert.Equal(t, mmT3, afterEcho.GroupStamp(state.GroupPoints), "the named echo takes its own stamp, not the stamp already stored")

		late := mmRunning(h, mmT2, state.GroupPoints)
		late.IpponsA = []string{}
		err := h.write(late)
		require.ErrorIs(t, err, ErrMatchSuperseded, "T2 is older than the point's real T3 stamp, so the removal is held")

		m := h.load(t)
		assert.Equal(t, []string{"M"}, m.IpponsA, "the point struck again at T3 stands")
	})
}

// bc-mrgc Finding 1, the decision side: the same regression made an exact
// replay of a recorded decision unrecognisable once that decision had been
// sent a second time as an echo. exactDecisionReplay matches by comparing
// the stored result group's stamp to the replay's own stamp, so a decision
// echoed at T2 must leave the result group stamped T2 -- not reverted to
// T1, the stamp of the ORIGINAL send -- or a later exact replay of the T2
// send can no longer recognise itself and falls through to
// the ordinary write instead of being answered as already recorded.
func TestMerge_ExactReplayOfANamedEchoedDecisionStillMatches(t *testing.T) {
	h := mmIndividual(t, false)
	ms, err := h.store.LoadPoolMatches(h.compID)
	require.NoError(t, err)
	ms = append(ms, state.MatchResult{
		ID: "Pool A-1", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamC, SideBID: wrTeamCID,
		Status: state.MatchStatusScheduled,
	})
	require.NoError(t, h.store.SavePoolMatches(h.compID, ms))

	_, _, err = h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT1)
	require.NoError(t, err)

	// The same decision sent again: it moves nothing (an echo), but a
	// decision always names its groups (decisionChangedGroups), so under
	// the fix it still takes T2 as the result group's own stamp.
	_, status, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT2)
	require.NoError(t, err)
	assert.Nil(t, status, "the echo moved nothing, so it has no eligibility consequence")
	afterEcho := h.load(t)
	assert.Equal(t, mmT2, afterEcho.GroupStamp(state.GroupResult), "the named echo took its own stamp")

	// The withdrawer's later match is put under way.
	ms, err = h.store.LoadPoolMatches(h.compID)
	require.NoError(t, err)
	for i := range ms {
		if ms[i].ID == "Pool A-1" {
			ms[i].Status = state.MatchStatusRunning
		}
	}
	require.NoError(t, h.store.SavePoolMatches(h.compID, ms))

	// An exact replay of the T2 send (the operator's lost-answer retry) is
	// answered as recorded, as a replay, not a new decision.
	got, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", nil, mmT2)
	require.NoError(t, err, "an exact replay of the T2 write is the recorded write, not an undo")
	require.NotNil(t, got)
	assert.Equal(t, "kiken-voluntary", got.Decision)
}

// R2 on a fixed-order team match: the bouts decide the winner (IV, then PW).
func TestMerge_ScoringAfterATeamWithdrawalClearsIt(t *testing.T) {
	eng, store, compID, _ := seedPoolWithdrawal(t, "kiken-voluntary")
	require.False(t, wrEligible(t, store, compID, wrTeamAID))
	h := mmHome{eng: eng, store: store, compID: compID, matchID: "Pool A-0"}

	board := mmRunning(h, mmT2)
	board.Changed = nil
	board.SubResults = []state.SubMatchResult{
		wrBout1("M"),
		{Position: 2, SideA: "r2", SideB: "t2", Winner: "t2", IpponsB: []string{"K"}},
		{Position: 3, SideA: "r3", SideB: "t3", Winner: "t3", IpponsB: []string{"D", "M"}},
	}
	board.IpponsA, board.IpponsB = []string{}, []string{}
	require.NoError(t, h.write(board))

	m := wrPoolMatch(t, store, compID)
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Empty(t, m.Decision)
	assert.Equal(t, wrTeamB, m.Winner, "Tora wins two bouts to one")
	assert.True(t, wrEligible(t, store, compID, wrTeamAID))
}

// R3: a correction made on a board after the match finished applies to the
// finished match, which stays finished, and the winner is worked out again.
// R4 (revised, operator ruling 2026-10-04: "It needs a winner if the match is
// finished and is being corrected"): when that would leave the match tied, a
// pool match is a draw, and in a knockout the change is NOT applied: the
// match keeps its recorded finish and its advanced winner, never goes back to
// running, and the change is held in the history with the reason.
func TestMerge_RunningPointAfterTheFinish(t *testing.T) {
	finish := func(t *testing.T, h mmHome) {
		t.Helper()
		done := mmRunning(h, mmT1)
		done.Changed = nil
		done.Status = state.MatchStatusCompleted
		done.IpponsA, done.IpponsB = []string{"M"}, []string{}
		done.Winner = wrTeamA
		require.NoError(t, h.write(done))
	}
	bothBranches(t, func(t *testing.T, knockout bool) {
		t.Run("the winner is worked out again", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			finish(t, h)
			late := mmRunning(h, mmT2)
			late.Changed = nil
			late.IpponsA, late.IpponsB = []string{"M"}, []string{"K", "D"}
			require.NoError(t, h.write(late))
			m := h.load(t)
			assert.Equal(t, state.MatchStatusCompleted, m.Status, "stays finished")
			assert.Equal(t, wrTeamB, m.Winner, "Tora now leads 2-1")
			assert.Equal(t, wrTeamBID, m.WinnerID)
			if knockout {
				b, err := h.store.LoadBracket(h.compID)
				require.NoError(t, err)
				assert.Equal(t, wrTeamB, b.Rounds[1][0].SideA, "the new winner advances")
			}
		})
		t.Run("a tie", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			finish(t, h)
			late := mmRunning(h, mmT2)
			late.Changed = nil
			late.IpponsA, late.IpponsB = []string{"M"}, []string{"K"}
			err := h.write(late)
			m := h.load(t)
			assert.Equal(t, state.MatchStatusCompleted, m.Status, "never back to running")
			if knockout {
				require.ErrorIs(t, err, ErrMatchSuperseded, "R4: the change that ties a knockout is held")
				assert.Equal(t, []string{state.GroupPoints}, HeldGroupsOf(err))
				assert.Equal(t, state.HeldReasonNeedsWinner, HeldReasonOf(err))
				assert.Equal(t, wrTeamA, m.Winner, "the recorded finish stands")
				assert.Equal(t, []string{"M"}, m.IpponsA)
				assert.Empty(t, m.IpponsB)
				assert.Equal(t, mmT1, m.GroupStamp(state.GroupPoints), "the held change moves no stamp")
				b, berr := h.store.LoadBracket(h.compID)
				require.NoError(t, berr)
				assert.Equal(t, wrTeamA, b.Rounds[1][0].SideA, "the winner stays advanced")
				last := h.history(t)[len(h.history(t))-1]
				assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupPoints])
				assert.JSONEq(t, `{"ipponsA":["M"],"ipponsB":["K"],"hansokuA":0,"hansokuB":0}`, string(last.Held[state.GroupPoints]))
				assert.Equal(t, HoldReasonKnockoutNeedsWinner, last.Reason)
			} else {
				require.NoError(t, err)
				assert.Empty(t, m.Winner)
				assert.Equal(t, state.DecisionDraw, m.Decision, "R4: a pool tie is a draw")
			}
		})
		t.Run("a change made before the finish is held", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			late := mmRunning(h, mmT1) // made at mmT1, the finish below is stamped later
			late.Changed = nil
			late.IpponsB = []string{"K", "D"}
			finishAt := mmRunning(h, mmT2)
			finishAt.Changed = nil
			finishAt.Status = state.MatchStatusCompleted
			finishAt.IpponsA, finishAt.IpponsB = []string{"M"}, []string{}
			finishAt.Winner = wrTeamA
			require.NoError(t, h.write(finishAt))
			require.ErrorIs(t, h.write(late), ErrMatchSuperseded)
			m := h.load(t)
			assert.Equal(t, wrTeamA, m.Winner)
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
		})
	})
}

// A whole-match echo (bulk-score, or an older client sending no `changed`,
// re-sending a finished match's exact scoreline) moves nothing, so it must
// not fence out a real, older change (bc-mrgc Finding 3): the echo keeps
// every group's T1 stamp instead of dragging them to its own T5, so a point
// genuinely made at T4 -- after the finish, before the echo arrived -- still
// applies and the winner is worked out again, exactly as it would with no
// echo in between.
func TestMerge_WholeMatchEchoDoesNotFenceOutAnOlderChange(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		finish := mmRunning(h, mmT1)
		finish.Changed = nil
		finish.Status = state.MatchStatusCompleted
		finish.IpponsA, finish.IpponsB = []string{"M"}, []string{}
		finish.Winner, finish.WinnerID = wrTeamA, wrTeamAID
		require.NoError(t, h.write(finish))

		// An echo of the exact stored finish, replayed late (a bulk-score
		// re-send, or an older client with no `changed`): nothing about it
		// differs, including the winner id, so under the fix it moves no
		// stamp.
		echo := mmRunning(h, mmT5)
		echo.Changed = nil
		echo.Status = state.MatchStatusCompleted
		echo.IpponsA, echo.IpponsB = []string{"M"}, []string{}
		echo.Winner, echo.WinnerID = wrTeamA, wrTeamAID
		require.NoError(t, h.write(echo), "an echo of the stored finish; nothing about it changed")
		afterEcho := h.load(t)
		assert.Equal(t, mmT1, afterEcho.GroupStamp(state.GroupResult), "the echo did not move the result's stamp")
		assert.Equal(t, mmT1, afterEcho.GroupStamp(state.GroupPoints), "nor the points'")

		late := mmRunning(h, mmT4)
		late.Changed = nil
		late.IpponsA, late.IpponsB = []string{"M"}, []string{"K", "D"}
		require.NoError(t, h.write(late), "T4 is after the finish and the echo moved nothing, so it applies")

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "stays finished")
		assert.Equal(t, wrTeamB, m.Winner, "the winner is worked out again from the later points")
		assert.Equal(t, wrTeamBID, m.WinnerID)
		if knockout {
			b, err := h.store.LoadBracket(h.compID)
			require.NoError(t, err)
			assert.Equal(t, wrTeamB, b.Rounds[1][0].SideA, "the new winner advances")
		}

		entries := h.history(t)
		last := entries[len(entries)-1]
		assert.NotEqual(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupPoints], "the later point applied; it was not held")
	})
}

// Finish atomicity (bc-mrgc Finding 4): a write that completes the match
// carries a scoreline its verdict stood on. When that verdict is held by its
// stamp -- a correction made on another device, after the stale write --
// everything else the stale write changes is held with it, never applied
// alone beside a verdict it never declared.
func TestMerge_FinishIsAtomicWithTheScorelineItStoodOn(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		finish := mmRunning(h, mmT1)
		finish.Changed = nil
		finish.Status = state.MatchStatusCompleted
		finish.IpponsA, finish.IpponsB = []string{"M"}, []string{}
		finish.Winner, finish.WinnerID = wrTeamA, wrTeamAID
		require.NoError(t, h.write(finish))

		// A verdict-only correction (an operator's hantei call): Tora
		// actually won. Needs no CorrectionReason here -- that gate is the
		// handler's (applyCorrectionReasonUnderTx), not the engine's.
		correction := mmRunning(h, mmT3, state.GroupResult)
		correction.Status = state.MatchStatusCompleted
		correction.Winner, correction.WinnerID = wrTeamB, wrTeamBID
		require.NoError(t, h.write(correction))

		// Device B's queued Finish from T2: stamped before the correction,
		// carrying a different scoreline than the one the correction judged.
		late := mmRunning(h, mmT2)
		late.Changed = nil
		late.Status = state.MatchStatusCompleted
		late.IpponsA, late.IpponsB = []string{"M"}, []string{"K"}
		late.Winner, late.WinnerID = wrTeamA, wrTeamAID
		err := h.write(late)
		require.ErrorIs(t, err, ErrMatchSuperseded, "the whole write is held with its outranked verdict")
		held := HeldGroupsOf(err)
		assert.Subset(t, held, []string{state.GroupResult, state.GroupPoints}, "the scoreline is held with the verdict it stood on")
		assert.Empty(t, HeldReasonOf(err), "the match already has a winner; none is needed")

		m := h.load(t)
		assert.Equal(t, wrTeamB, m.Winner, "the correction's verdict stands")
		assert.Equal(t, []string{"M"}, m.IpponsA, "B's queued scoreline never lands beside it")
		assert.Empty(t, m.IpponsB)

		last := h.history(t)[len(h.history(t))-1]
		assert.Equal(t, HoldReasonFinishAtomic, last.Reason)
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupPoints])
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupResult])
	})
}

// A removed representative bout keeps its stamp as a tombstone: an older
// write still carrying the row does not bring it back, a newer one does.
func TestMerge_RemovedBoutIsATombstone(t *testing.T) {
	h := mmTeam(t, true)
	dh := state.SubMatchResult{Position: state.DaihyosenSubPosition, Decision: string(domain.DecisionDaihyosen)}
	add := mmRunning(h, mmT1, state.BoutGroup(-1))
	add.WriteDoor = DoorDaihyosenAdd
	add.SubResults = []state.SubMatchResult{dh}
	require.NoError(t, h.write(add))
	require.Len(t, h.load(t).SubResults, 1)

	remove := mmRunning(h, mmT3, state.BoutGroup(-1))
	remove.WriteDoor = DoorDaihyosenDel
	remove.SubResults = []state.SubMatchResult{}
	require.NoError(t, h.write(remove))
	m := h.load(t)
	require.Empty(t, m.SubResults, "removed")
	assert.Equal(t, mmT3, m.GroupStamp(state.BoutGroup(-1)), "its stamp stays: the tombstone")

	stale := mmRunning(h, mmT2, state.BoutGroup(-1))
	stale.SubResults = []state.SubMatchResult{dh}
	require.ErrorIs(t, h.write(stale), ErrMatchSuperseded)
	assert.Empty(t, h.load(t).SubResults, "an older write does not resurrect it")

	fresh := mmRunning(h, mmT4, state.BoutGroup(-1))
	fresh.SubResults = []state.SubMatchResult{dh}
	require.NoError(t, h.write(fresh))
	assert.Len(t, h.load(t).SubResults, 1, "a newer one re-adds it")
}

// A stored match with no group stamps and a payload naming no groups behave
// exactly as the whole-match guard did, and a running payload naming no
// groups over a finished match never reopens it.
func TestMerge_LegacyMatchesBehaveAsBefore(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		t.Run("whole match by stamp", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			require.Nil(t, h.load(t).GroupStamps, "precondition: a legacy match")
			older := mmRunning(h, mmT0-1)
			older.Changed = nil
			older.IpponsA = []string{"M"}
			older.Encho = &state.EnchoMetadata{PeriodCount: 1}
			require.ErrorIs(t, h.write(older), ErrMatchSuperseded, "older than the match: nothing of it applies")
			assert.Empty(t, h.load(t).IpponsA)

			newer := mmRunning(h, mmT1)
			newer.Changed = nil
			newer.IpponsA = []string{"M"}
			require.NoError(t, h.write(newer))
			m := h.load(t)
			assert.Equal(t, []string{"M"}, m.IpponsA)
			assert.Equal(t, mmT1, m.ModifiedAt)
			assert.Equal(t, mmT1, m.GroupStamp(state.GroupPoints), "the group that actually changed is stamped")
			// Status stayed "running" on both sides: an echo of a group it
			// carried, so it keeps the stamp materialized from the match's
			// own start (bc-mrgc Finding 3: an echo never takes the write's
			// stamp, whole-match legacy write included).
			assert.Equal(t, mmT0, m.GroupStamp(state.GroupResult), "a group the write only echoed keeps its own stamp")
		})
		t.Run("a running echo never reopens a finished match", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			done := mmRunning(h, mmT1)
			done.Changed = nil
			done.Status = state.MatchStatusCompleted
			done.IpponsA, done.IpponsB = []string{"M"}, []string{}
			done.Winner = wrTeamA
			require.NoError(t, h.write(done))

			echo := mmRunning(h, mmT2)
			echo.Changed = nil
			echo.IpponsA, echo.IpponsB = []string{"M"}, []string{}
			require.NoError(t, h.write(echo))
			m := h.load(t)
			assert.Equal(t, state.MatchStatusCompleted, m.Status, "the result group was never the write's")
			assert.Equal(t, wrTeamA, m.Winner)
		})
	})
}

// The history: one entry per write that reached the merge, in the match's own
// file, staged in the write's transaction; a refused write leaves none, and
// it goes with the competition and with a discarded draw.
func TestMerge_HistoryIsStagedWithTheWrite(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		applied := mmRunning(h, mmT2, state.GroupPoints, state.GroupEncho)
		applied.IpponsA = []string{"M"}
		applied.Encho = &state.EnchoMetadata{PeriodCount: 1}
		require.NoError(t, h.write(applied))

		partial := mmRunning(h, mmT1, state.GroupPoints, state.GroupFlags)
		partial.IpponsA = []string{"K"}
		partial.FlagsA = 1
		require.NoError(t, h.write(partial), "the flags apply, the points are held")
		assert.Equal(t, []string{state.GroupPoints}, partial.Merge.HeldGroups())

		held := mmRunning(h, mmT1, state.GroupEncho)
		require.ErrorIs(t, h.write(held), ErrMatchSuperseded)

		entries := h.history(t)
		require.Len(t, entries, 3)
		assert.Equal(t, state.HistoryOutcomeApplied, entries[0].Outcomes[state.GroupPoints])
		assert.Equal(t, state.HistoryOutcomeHeld, entries[1].Outcomes[state.GroupPoints])
		assert.Equal(t, state.HistoryOutcomeApplied, entries[1].Outcomes[state.GroupFlags])
		assert.Equal(t, state.HistoryOutcomeHeld, entries[2].Outcomes[state.GroupEncho])
		_, statErr := os.Stat(filepath.Join(h.dir, "competitions", h.compID, "history", url.PathEscape(h.matchID)+".jsonl"))
		assert.NoError(t, statErr, "one file per match")

		// A write the side check refuses leaves no entry.
		wrong := mmRunning(h, mmT4, state.GroupPoints)
		wrong.SideA = "Someone else"
		require.ErrorIs(t, h.write(wrong), ErrMatchSideMismatch)
		assert.Len(t, h.history(t), 3)

		// Staged in the SAME transaction: an aborted one leaves none.
		boom := errors.New("abort")
		err := h.store.WithTransaction(h.compID, func(tx state.StoreTx) error {
			w := mmRunning(h, mmT4, state.GroupPoints)
			w.IpponsA = []string{"D"}
			if _, e := h.eng.RecordMatchResultWithIneligibilityTx(tx, h.compID, h.matchID, w); e != nil {
				return e
			}
			return boom
		})
		require.ErrorIs(t, err, boom)
		assert.Len(t, h.history(t), 3, "the entry vanished with the write it describes")
		assert.Equal(t, []string{"M"}, h.load(t).IpponsA)
	})
}

func TestMerge_HistoryGoesWithTheCompetitionAndTheDraw(t *testing.T) {
	t.Run("deleted competition", func(t *testing.T) {
		h := mmIndividual(t, false)
		w := mmRunning(h, mmT1, state.GroupPoints)
		w.IpponsA = []string{"M"}
		require.NoError(t, h.write(w))
		require.Len(t, h.history(t), 1)
		require.NoError(t, h.store.DeleteCompetition(h.compID))
		_, err := os.Stat(filepath.Join(h.dir, "competitions", h.compID))
		assert.True(t, os.IsNotExist(err))
		// A history append after the delete fails rather than resurrecting
		// the competition's directory.
		require.Error(t, h.store.AppendMatchHistory(h.compID, state.MatchHistoryEntry{MatchID: h.matchID}))
		_, err = os.Stat(filepath.Join(h.dir, "competitions", h.compID))
		assert.True(t, os.IsNotExist(err), "the directory is not resurrected")
	})
	t.Run("discarded draw", func(t *testing.T) {
		h := mmIndividual(t, false)
		w := mmRunning(h, mmT1, state.GroupPoints)
		w.IpponsA = []string{"M"}
		require.NoError(t, h.write(w))
		require.Len(t, h.history(t), 1)
		_, err := h.store.UpdateCompetitionChanged(h.compID, func(c *state.Competition) (*state.Competition, error) {
			c.Status = state.CompStatusDrawReady
			return c, nil
		})
		require.NoError(t, err)
		require.NoError(t, h.eng.DiscardDraw(h.compID))
		assert.Empty(t, h.history(t), "a draw generated again reuses the match ids")
	})
}

// The K3 rollback restores every group and its stamps exactly, a tombstone
// included.
func TestMerge_RollbackRestoresTheStamps(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		w := mmRunning(h, mmT2, state.GroupPoints, state.BoutGroup(1))
		w.IpponsA = []string{"M"}
		w.SubResults = []state.SubMatchResult{wrBout1("M")}
		require.NoError(t, h.write(w))
		snapshot := h.load(t)
		snapshot.GroupStamps[state.BoutGroup(5)] = mmT1 // a tombstone the snapshot carries
		want := state.CloneGroupStamps(snapshot.GroupStamps)

		later := mmRunning(h, mmT4, state.GroupPoints, state.GroupEncho)
		later.IpponsA = []string{"K"}
		later.Encho = &state.EnchoMetadata{PeriodCount: 1}
		require.NoError(t, h.write(later))
		loaded := h.load(t)
		require.Equal(t, mmT4, loaded.GroupStamp(state.GroupPoints))

		require.NoError(t, inTx(t, h.store, h.compID, func(tx state.StoreTx) error {
			h.eng.rollbackMatchResultTx(tx, h.compID, h.matchID, &snapshot)
			return nil
		}))
		m := h.load(t)
		assert.Equal(t, want, m.GroupStamps, "every stamp restored exactly")
		assert.Equal(t, []string{"M"}, m.IpponsA)
		assert.False(t, m.Encho.On())
		assert.Equal(t, snapshot.ModifiedAt, m.ModifiedAt)
	})
}

// A team match whose only bout row is the representative bout is still a
// team match: its winner comes from that bout, never from the match-level
// ippons (empty on a team match), which would read it tied and, on a
// knockout, hold the change that decided it (review thread on PR #453).
func TestDeriveWinnerAfterMerge_RepresentativeBoutAloneDecidesATeamMatch(t *testing.T) {
	comp := &state.Competition{ID: "teams", Kind: "team", TeamSize: 3}
	m := &state.MatchResult{
		ID: "m-r1-0", SideA: "Kyoto", SideAID: "t-kyoto", SideB: "Osaka", SideBID: "t-osaka",
		Status: state.MatchStatusCompleted,
		SubResults: []state.SubMatchResult{
			{Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka", Winner: "Osaka", IpponsB: []string{"M"}, Decision: "daihyosen"},
		},
	}
	require.True(t, deriveWinnerAfterMerge(m, mergeCtx{comp: comp, knockout: true}))
	assert.Equal(t, "Osaka", m.Winner)
	assert.Equal(t, "t-osaka", m.WinnerID)
	assert.Equal(t, "B", m.WinnerSide)
}
