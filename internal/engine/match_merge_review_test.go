package engine

// bc-mrgc review: each test pins one finding of the merge's review round, and
// fails without its fix.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// mmFinish is a whole-board Finish (no `changed`, today's client): completed,
// with the winner and the scoreline it was made on.
func mmFinish(h mmHome, at int64, winner string, a, b []string) *state.MatchResult {
	w := mmRunning(h, at)
	w.Changed = nil
	w.Status = state.MatchStatusCompleted
	w.Winner = winner
	w.IpponsA, w.IpponsB = a, b
	return w
}

// mmVerdict is what a final state is compared on across arrival orders.
type mmVerdict struct {
	Status      state.MatchStatus
	Winner      string
	WinnerID    string
	Decision    string
	IpponsA     []string
	IpponsB     []string
	GroupStamps map[string]int64
	ModifiedAt  int64
	NextRound   string
}

func (h mmHome) verdict(t *testing.T, knockout bool) mmVerdict {
	t.Helper()
	m := h.load(t)
	v := mmVerdict{
		Status: m.Status, Winner: m.Winner, WinnerID: m.WinnerID, Decision: m.Decision,
		IpponsA: append([]string{}, m.IpponsA...), IpponsB: append([]string{}, m.IpponsB...),
		GroupStamps: m.GroupStamps, ModifiedAt: m.ModifiedAt,
	}
	if knockout {
		b, err := h.store.LoadBracket(h.compID)
		require.NoError(t, err)
		v.NextRound = b.Rounds[1][0].SideA
	}
	return v
}

// S2: a Finish made at T2 and a point made at T3 on a board still scoring
// (it never saw the finish) reach the same final state whichever arrives
// first: the finish, then the point, in stamp order. The finish arriving
// second used to keep its own winner beside the newer scoreline that
// contradicts it.
func TestMergeReview_FinishAndNewerPointGiveOneVerdictInEitherOrder(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		run := func(t *testing.T, finishFirst bool) mmVerdict {
			h := mmIndividual(t, knockout)
			finish := mmFinish(h, mmT2, wrTeamA, []string{"M"}, []string{})
			point := mmRunning(h, mmT3, state.GroupPoints)
			point.IpponsA, point.IpponsB = []string{"M"}, []string{"K", "D"}
			if finishFirst {
				require.NoError(t, h.write(finish))
				require.NoError(t, h.write(point))
			} else {
				require.NoError(t, h.write(point))
				require.NoError(t, h.write(finish), "the finish's result applies; its scoreline is held by the newer point")
			}
			return h.verdict(t, knockout)
		}
		inOrder := run(t, true)
		reversed := run(t, false)
		assert.Equal(t, state.MatchStatusCompleted, inOrder.Status)
		assert.Equal(t, wrTeamB, inOrder.Winner, "Tora leads 2-1 on the newer point")
		assert.Equal(t, wrTeamBID, inOrder.WinnerID)
		assert.Equal(t, inOrder, reversed, "the arrival order makes no difference")
	})
}

// mmTyingChangeHeld finds the history entry that keeps the tying change (the
// point made at mmT3) as held because a knockout match needs a winner.
func mmTyingChangeHeld(t *testing.T, h mmHome, wantPoints string, reason string) {
	t.Helper()
	for _, e := range h.history(t) {
		if e.Stamp == mmT3 && e.Outcomes[state.GroupPoints] == state.HistoryOutcomeHeld {
			assert.JSONEq(t, wantPoints, string(e.Held[state.GroupPoints]), "the tying change is kept with its values")
			assert.Equal(t, reason, e.Reason)
			return
		}
	}
	t.Fatalf("no history entry holds the change made at mmT3: %+v", h.history(t))
}

// S2 with a newer point that TIES the match. A pool tie is a draw in either
// order. A knockout cannot end tied: in stamp order the finish came first and
// the tying point after it, which R4 does not apply. Both arrival orders end
// there (operator ruling: "All events must be ordered"): the match finished
// on the finish's own result and scoreline, and the tying point held in the
// history with the reason that a knockout needs a winner. Arriving first, the
// point is moved out of the match into the history by the finish that
// arrives after it.
func TestMergeReview_FinishAndNewerTyingPoint(t *testing.T) {
	setup := func(t *testing.T, knockout bool) (mmHome, *state.MatchResult, *state.MatchResult) {
		h := mmIndividual(t, knockout)
		finish := mmFinish(h, mmT2, wrTeamA, []string{"M"}, []string{})
		point := mmRunning(h, mmT3, state.GroupPoints)
		point.IpponsA, point.IpponsB = []string{"M"}, []string{"K"}
		return h, finish, point
	}
	t.Run("pool: a draw either way", func(t *testing.T) {
		var got []mmVerdict
		for _, finishFirst := range []bool{true, false} {
			h, finish, point := setup(t, false)
			writes := []*state.MatchResult{point, finish}
			if finishFirst {
				writes = []*state.MatchResult{finish, point}
			}
			for _, w := range writes {
				require.NoError(t, h.write(w))
			}
			v := h.verdict(t, false)
			assert.Equal(t, state.MatchStatusCompleted, v.Status)
			assert.Equal(t, state.DecisionDraw, v.Decision)
			assert.Empty(t, v.Winner)
			got = append(got, v)
		}
		assert.Equal(t, got[0], got[1])
	})
	t.Run("knockout: one state in either order", func(t *testing.T) {
		const tying = `{"ipponsA":["M"],"ipponsB":["K"],"hansokuA":0,"hansokuB":0}`

		h1, finish, point := setup(t, true)
		require.NoError(t, h1.write(finish))
		err := h1.write(point)
		require.ErrorIs(t, err, ErrMatchSuperseded, "the tying point arriving second is held")
		assert.Equal(t, state.HeldReasonNeedsWinner, HeldReasonOf(err))
		inOrder := h1.verdict(t, true)
		mmTyingChangeHeld(t, h1, tying, HoldReasonKnockoutNeedsWinner)

		h2, finish, point := setup(t, true)
		require.NoError(t, h2.write(point))
		require.NoError(t, h2.write(finish), "the finish arriving second is applied")
		rep := finish.Merge
		require.NotNil(t, rep)
		assert.Equal(t, []string{state.GroupPoints}, rep.DisplacedGroups(), "the answer names what was moved to the history")
		assert.Equal(t, state.HeldReasonNeedsWinner, rep.HeldReason())
		assert.Empty(t, rep.HeldGroups(), "nothing of the finish was held")
		reversed := h2.verdict(t, true)
		mmTyingChangeHeld(t, h2, tying, HoldReasonKnockoutNeedsWinner)

		assert.Equal(t, state.MatchStatusCompleted, inOrder.Status)
		assert.Equal(t, wrTeamA, inOrder.Winner, "the finish's result stands")
		assert.Equal(t, []string{"M"}, inOrder.IpponsA)
		assert.Empty(t, inOrder.IpponsB, "on the finish's own scoreline")
		assert.Equal(t, wrTeamA, inOrder.NextRound)
		assert.Equal(t, mmT2, inOrder.ModifiedAt)
		assert.Equal(t, inOrder, reversed, "status, winner, scoreline, stamps and the next round match")
	})
	t.Run("knockout: a finish without its scoreline is held", func(t *testing.T) {
		// A finish that names only its verdict and carries no scoreline
		// does not say what it stood on, so the state before the newer
		// point cannot be rebuilt: the finish is held instead.
		h, _, point := setup(t, true)
		require.NoError(t, h.write(point))
		bare := mmRunning(h, mmT2, state.GroupResult)
		bare.Status, bare.Winner = state.MatchStatusCompleted, wrTeamA
		err := h.write(bare)
		require.ErrorIs(t, err, ErrMatchSuperseded)
		assert.Equal(t, state.HeldReasonNeedsWinner, HeldReasonOf(err))
		v := h.verdict(t, true)
		assert.Equal(t, state.MatchStatusRunning, v.Status)
		assert.Equal(t, []string{"K"}, v.IpponsB)
	})
}

// S2 with R4 on an engi match: a recount that leaves no valid count (an even
// total), made after the finish, cannot apply after it. Either arrival order
// ends on the finish's own count and winner, with the recount in the history.
func TestMergeReview_EngiFinishAndNewerRecountInEitherOrder(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		type out struct {
			Status         state.MatchStatus
			Winner         string
			FlagsA, FlagsB int
		}
		run := func(t *testing.T, finishFirst bool) out {
			h := mmEngi(t, knockout)
			finish := mmEngiFinish(h, mmT2, 3, 0)
			recount := mmRunning(h, mmT3, state.GroupFlags)
			recount.FlagsA, recount.FlagsB = 1, 1
			if finishFirst {
				require.NoError(t, h.write(finish))
				require.ErrorIs(t, h.write(recount), ErrMatchSuperseded)
			} else {
				require.NoError(t, h.write(recount))
				require.NoError(t, h.write(finish), "the finish is applied on its own count")
			}
			held := false
			for _, e := range h.history(t) {
				if e.Stamp == mmT3 && e.Outcomes[state.GroupFlags] == state.HistoryOutcomeHeld {
					held = true
					assert.Contains(t, string(e.Held[state.GroupFlags]), `"flagsA":1`)
					assert.Equal(t, HoldReasonEngiNeedsValidCount, e.Reason)
				}
			}
			assert.True(t, held, "the recount is kept in the history")
			m := h.load(t)
			return out{m.Status, m.Winner, m.FlagsA, m.FlagsB}
		}
		inOrder, reversed := run(t, true), run(t, false)
		assert.Equal(t, out{state.MatchStatusCompleted, wrTeamA, 3, 0}, inOrder)
		assert.Equal(t, inOrder, reversed)
	})
}

// S2 keeps the finish's judges' decision: a hantei finish (tied 1-1, the mark
// to Ryu) made at mmT2, and a foul added at mmT3 on a board that never showed
// the mark and does not claim the result. In stamp order the foul applies and
// keeps the stored mark (S3); arriving first, the finish that follows it
// carries its own mark onto the newer scoreline. One state either way.
func TestMergeReview_HanteiFinishAndNewerPointsInEitherOrder(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		run := func(t *testing.T, finishFirst bool) mmVerdict {
			h := mmIndividual(t, knockout)
			finish := mmFinish(h, mmT2, wrTeamA, []string{"M", domain.HanteiMark}, []string{"K"})
			foul := mmRunning(h, mmT3, state.GroupPoints)
			foul.IpponsA, foul.IpponsB = []string{"M"}, []string{"K"}
			foul.HansokuB = 1
			writes := []*state.MatchResult{foul, finish}
			if finishFirst {
				writes = []*state.MatchResult{finish, foul}
			}
			for _, w := range writes {
				require.NoError(t, h.write(w))
			}
			m := h.load(t)
			assert.Equal(t, 1, m.HansokuB, "the foul applied")
			return h.verdict(t, knockout)
		}
		inOrder, reversed := run(t, true), run(t, false)
		assert.Equal(t, state.MatchStatusCompleted, inOrder.Status)
		assert.Equal(t, wrTeamA, inOrder.Winner, "the judges' decision settles the tie")
		assert.True(t, domain.ContainsHantei(inOrder.IpponsA), "the mark is kept")
		assert.Equal(t, inOrder, reversed)
	})
}

// S3: the hantei mark lives in the points group, but it is the judges'
// decision, the result's. A late points change from a board still scoring
// (here a foul added after the finish) does not name the result, so it keeps
// the stored mark: the tie stays settled by it and the winner stands.
func TestMergeReview_LatePointsKeepTheJudgesDecision(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		require.NoError(t, h.write(mmFinish(h, mmT1, wrTeamA, []string{"M", domain.HanteiMark}, []string{"K"})))

		foul := mmRunning(h, mmT2, state.GroupPoints)
		foul.IpponsA, foul.IpponsB = []string{"M"}, []string{"K"} // the board never showed the mark
		foul.HansokuB = 1
		require.NoError(t, h.write(foul))

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status)
		assert.Equal(t, wrTeamA, m.Winner, "the judges' decision still settles the tie")
		assert.True(t, domain.ContainsHantei(m.IpponsA), "the mark is kept")
		assert.Equal(t, 1, m.HansokuB, "and the foul applied")
		assert.Empty(t, m.Decision)
	})
}

// S4: a kachinuki finish strips a trailing unscored pairing the server had
// appended. A writer that names only the verdict never named that row, and
// the merge copied it back; it is named as the server's change now.
func TestMergeReview_KachinukiFinishStripsTheTrailingPairing(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	_ = dir
	h := mmHome{eng: eng, store: store, compID: "mm-kachi", matchID: "Pool A-0"}
	createTestCompetition(t, store, h.compID, "league", 3, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
		c.TeamMatchType = state.TeamMatchTypeKachinuki
		c.Status = state.CompStatusPools
	})
	wrSaveTeams(t, store, h.compID)
	require.NoError(t, store.SavePoolMatches(h.compID, []state.MatchResult{{
		ID: h.matchID, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning, ModifiedAt: mmT1,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "r1", SideB: "t1", Winner: "r1", IpponsA: []string{"M"}},
			{Position: 2, SideA: "r1", SideB: "t2"},
		},
	}}))

	finish := mmRunning(h, mmT2, state.GroupResult)
	finish.Status = state.MatchStatusCompleted
	finish.Decision = string(domain.DecisionKachinukiExhaustion)
	finish.Winner = wrTeamA
	finish.SubResults = []state.SubMatchResult{{Position: 1, SideA: "r1", SideB: "t1", Winner: "r1", IpponsA: []string{"M"}}}
	require.NoError(t, h.write(finish))

	m := h.load(t)
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	require.Len(t, m.SubResults, 1, "the unscored trailing pairing is gone")
	assert.Equal(t, 1, m.SubResults[0].Position)
	last := h.history(t)[len(h.history(t))-1]
	assert.Contains(t, last.Changed, state.BoutGroup(2), "the strip is named as a change")
}

// S5: an unstamped server-built correction (quick-score) takes the server's
// time, so a board's autosave made before it but replayed after it is
// ordered against it: the correction stands, the stale autosave is held.
func TestMergeReview_UnstampedCorrectionLeavesAFence(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		now := time.Now().UnixMilli()
		t0, t1, t2 := now-120_000, now-60_000, now-30_000
		// Started at t0 by the real clock (the fixture's stamps lie ahead of
		// it), so the server's time of the correction is after every write.
		if knockout {
			b, err := h.store.LoadBracket(h.compID)
			require.NoError(t, err)
			b.Rounds[0][0].ModifiedAt = t0
			require.NoError(t, h.store.SaveBracket(h.compID, b))
		} else {
			ms, err := h.store.LoadPoolMatches(h.compID)
			require.NoError(t, err)
			ms[0].ModifiedAt = t0
			require.NoError(t, h.store.SavePoolMatches(h.compID, ms))
		}

		auto1 := mmRunning(h, t1)
		auto1.Changed = nil
		auto1.IpponsA = []string{"M"}
		require.NoError(t, h.write(auto1))

		correction := &state.MatchResult{
			ID: h.matchID, SideA: wrTeamA, SideB: wrTeamB, Status: state.MatchStatusCompleted,
			Winner: wrTeamB, IpponsA: []string{"M"}, IpponsB: []string{"K", "D"}, WriteDoor: DoorQuickScore,
		}
		changed, err := h.eng.WholeMatchChangedGroups(h.store, h.compID, h.matchID, correction)
		require.NoError(t, err)
		correction.Changed = changed
		require.NoError(t, h.write(correction))

		auto2 := mmRunning(h, t2) // queued offline before the correction, replayed after
		auto2.Changed = nil
		auto2.IpponsA = []string{"M", "K"}
		require.ErrorIs(t, h.write(auto2), ErrMatchSuperseded)

		m := h.load(t)
		assert.Equal(t, state.MatchStatusCompleted, m.Status)
		assert.Equal(t, wrTeamB, m.Winner, "the correction stands")
		assert.Equal(t, []string{"K", "D"}, m.IpponsB)
		assert.Equal(t, []string{"M"}, m.IpponsA)
		last := h.history(t)[len(h.history(t))-1]
		assert.Equal(t, t2, last.Stamp)
		assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupPoints], "the stale autosave is kept in the history")
	})
}

// S6: a correction sent without its sides (a payload that omits them) over a
// finished knockout match. The winner is worked out again from the stored
// pairing; it used to be worked out from the payload's empty sides, which
// named nobody and refused the correction.
func TestMergeReview_SidelessCorrectionOverAFinishedKnockout(t *testing.T) {
	h := mmIndividual(t, true)
	require.NoError(t, h.write(mmFinish(h, mmT1, wrTeamA, []string{"M"}, []string{})))

	late := mmRunning(h, mmT2)
	late.Changed = nil
	late.SideA, late.SideB = "", ""
	late.IpponsA, late.IpponsB = []string{"M"}, []string{"K", "D"}
	require.NoError(t, h.write(late))

	m := h.load(t)
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, wrTeamB, m.Winner)
	assert.Equal(t, wrTeamBID, m.WinnerID)
	b, err := h.store.LoadBracket(h.compID)
	require.NoError(t, err)
	assert.Equal(t, wrTeamB, b.Rounds[1][0].SideA, "the new winner advances")
}

// F1: a write queued before a team was renamed names it by the old name, in
// the bout rows as well: a fixed-order row that names no fighter records the
// team as its winner. Its id proves the side; every row is renamed with it,
// so each bout is still credited to the team that won it.
func TestMergeReview_RenameReachesTheBoutRows(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmTeam(t, knockout)
		const old = "Ryu (old name)"
		queued := &state.MatchResult{
			ID: h.matchID, SideA: old, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Winner: old, WinnerID: wrTeamAID, Status: state.MatchStatusCompleted, ModifiedAt: mmT2, WriteDoor: DoorScore,
			SubResults: []state.SubMatchResult{
				{Position: 1, Winner: old, IpponsA: []string{"M"}},
				{Position: 2, Winner: old, IpponsA: []string{"K"}},
				{Position: 3, Winner: wrTeamB, IpponsB: []string{"D"}},
			},
		}
		require.NoError(t, h.write(queued))
		m := h.load(t)
		assert.Equal(t, wrTeamA, m.Winner)
		line := m.TeamResult()
		require.NotNil(t, line)
		assert.Equal(t, 2, line.AkaIV, "both bouts the renamed team won are credited to it")
		assert.Equal(t, 1, line.ShiroIV)
		for _, sub := range m.SubResults {
			assert.NotEqual(t, old, sub.Winner)
		}
	})
}

// GH-T1: a pool representative bout or tie-break bout is ONE individual bout,
// whose overtime lives at match level. In a kachinuki competition it keeps
// its encho on a write, and a withdrawal in encho gives the one circle an
// overtime bout scores, not the two of regulation time.
func TestMergeReview_KachinukiPoolRepBoutKeepsItsEncho(t *testing.T) {
	seed := func(t *testing.T) mmHome {
		eng, store, _ := setupTestEngine(t)
		h := mmHome{eng: eng, store: store, compID: "mm-kachi-dh", matchID: "Pool A-DH-1"}
		createTestCompetition(t, store, h.compID, "league", 3, func(c *state.Competition) {
			c.Kind = "team"
			c.TeamSize = 3
			c.TeamMatchType = state.TeamMatchTypeKachinuki
			c.Status = state.CompStatusPools
		})
		wrSaveTeams(t, store, h.compID)
		require.NoError(t, store.SavePoolMatches(h.compID, []state.MatchResult{{
			ID: h.matchID, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusRunning, ModifiedAt: mmT0,
		}}))
		return h
	}
	t.Run("a write keeps it", func(t *testing.T) {
		h := seed(t)
		w := mmRunning(h, mmT1, state.GroupEncho)
		w.Encho = &state.EnchoMetadata{PeriodCount: 1}
		require.NoError(t, h.write(w))
		assert.True(t, h.load(t).Encho.On(), "the bout's overtime is recorded")
	})
	t.Run("a withdrawal in encho gives one circle", func(t *testing.T) {
		h := seed(t)
		_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "kiken-voluntary", "aka", "knee", &state.EnchoMetadata{PeriodCount: 1}, false, mmT1)
		require.NoError(t, err)
		m := h.load(t)
		assert.Equal(t, wrTeamB, m.Winner)
		assert.Equal(t, domain.DefaultWinIppons(true), m.IpponsB, "one circle in overtime")
		assert.True(t, m.Encho.On())
	})
}

// GH-T4: the fields that belong to one write never ride on the stored copy
// the pool write leaves: a writer building its write from it (the daihyosen
// add, `u := *match`) must not inherit another write's instruction to replace
// a ruling, nor its groups, door or merge report.
func TestMergeReview_StoredCopyCarriesNoRequestFields(t *testing.T) {
	stored := &state.MatchResult{ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Status: state.MatchStatusRunning}
	incoming := &state.MatchResult{
		ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Status: state.MatchStatusRunning,
		IpponsA: []string{"M"}, ClearsWithdrawal: true, WriteDoor: DoorScore, Changed: []string{state.GroupPoints},
	}
	_, superseded, _, err := applyPoolWrite(stored, incoming, matchWriteForward, nil)
	require.NoError(t, err)
	require.False(t, superseded)
	assert.Equal(t, []string{"M"}, stored.IpponsA)
	assert.False(t, stored.ClearsWithdrawal)
	assert.Nil(t, stored.Changed)
	assert.Empty(t, stored.WriteDoor)
	assert.Nil(t, stored.Merge)
	assert.True(t, incoming.ClearsWithdrawal, "the write itself keeps what it was sent with")
}
