package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The built-on hold (bc-sseq FR8, result_lock.go holdBuiltOnResult): a court
// result that would replace a result another device recorded, which it never
// saw, WAITS once other matches were built on that result (a later Swiss round
// paired, a tie-break or representative row added from the standings, the
// competition finished). An ordinary device's late result keeps today's rules.

var builtOnKinds = []BuiltOnKind{BuiltOnSwissRound, BuiltOnStandings, BuiltOnFinished}

// boHome is an individual league match (Pool A-0), or for a Swiss case a
// round-1 match of a Swiss competition (Swiss-R1-0), running since mmT0; an
// engi one when engi.
func boHome(t *testing.T, kind BuiltOnKind, engi bool) mmHome {
	t.Helper()
	h := mmIndividual(t, false)
	if engi {
		h = mmEngi(t, false)
	}
	if kind == BuiltOnSwissRound {
		comp, err := h.store.LoadCompetition(h.compID)
		require.NoError(t, err)
		comp.Format = state.CompFormatSwiss
		require.NoError(t, h.store.SaveCompetition(comp))
		h.matchID = "Swiss-R1-0"
		require.NoError(t, h.store.SavePoolMatches(h.compID, []state.MatchResult{{
			ID: h.matchID, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusRunning, ModifiedAt: mmT0,
		}}))
	}
	return h
}

// boBuild builds on the match's stored result, as kind says.
func boBuild(t *testing.T, h mmHome, kind BuiltOnKind) {
	t.Helper()
	switch kind {
	case BuiltOnSwissRound, BuiltOnStandings:
		ms, err := h.store.LoadPoolMatches(h.compID)
		require.NoError(t, err)
		id := "Pool A-TB-1"
		if kind == BuiltOnSwissRound {
			id = "Swiss-R2-0"
		}
		ms = append(ms, state.MatchResult{ID: id, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamC, SideBID: wrTeamCID, Status: state.MatchStatusScheduled})
		require.NoError(t, h.store.SavePoolMatches(h.compID, ms))
	case BuiltOnFinished:
		comp, err := h.store.LoadCompetition(h.compID)
		require.NoError(t, err)
		comp.Status = state.CompStatusComplete
		require.NoError(t, h.store.SaveCompetition(comp))
	}
}

// boHeldLines counts the match's history lines the built-on hold kept.
func boHeldLines(t *testing.T, h mmHome) int {
	t.Helper()
	n := 0
	for _, e := range h.history(t) {
		if e.Reason == HoldReasonResultBuiltOn {
			n++
		}
	}
	return n
}

// boMessage is the sentence the answer to a held court result carries.
func boMessage(t *testing.T, h mmHome) string {
	t.Helper()
	var msg string
	require.NoError(t, h.store.WithTransaction(h.compID, func(tx state.StoreTx) error {
		msg = ResultBuiltOnMessageTx(tx, h.compID, h.matchID)
		return nil
	}))
	return msg
}

func TestBuiltOnHold_CourtResultWaits(t *testing.T) {
	M, K := []string{"M"}, []string{"K"}
	sentence := map[BuiltOnKind]string{
		BuiltOnSwissRound: "round 2 was already paired",
		BuiltOnStandings:  "tie-break or representative match was already added",
		BuiltOnFinished:   "that reopens the competition",
	}
	for _, kind := range builtOnKinds {
		for _, o := range []struct {
			name           string
			ordAt, courtAt int64
		}{
			{"the court's result stamped after the ordinary one", mmT1, mmT3},
			{"the court's result stamped before the ordinary one", mmT3, mmT1},
		} {
			t.Run(string(kind)+"/"+o.name, func(t *testing.T) {
				h := boHome(t, kind, false)
				require.Equal(t, "applied", pmOrdFin(o.ordAt, wrTeamA, M, nil).run(h))
				boBuild(t, h, kind)
				court := pmCourtFin(o.courtAt, wrTeamB, nil, K)
				assert.Equal(t, "superseded:"+state.HeldReasonBuiltOn, court.run(h))
				got := pmState(h, t)
				assert.Contains(t, got, `w="`+wrTeamA+`"`, "the stored result stands")
				assert.Contains(t, got, "A=[M] B=[]")
				assert.Empty(t, pmReplay(h, t), "the held court result left no score-log line")
				assert.Equal(t, 1, boHeldLines(t, h), "the court's values are kept in the history")
				assert.False(t, pmCourtReplaced(t, h), "nothing was replaced")

				// Resent (its answer lost): held again, kept once.
				assert.Equal(t, "superseded:"+state.HeldReasonBuiltOn, court.run(h))
				assert.Equal(t, 1, boHeldLines(t, h), "a resend adds no history line")
				assert.Equal(t, got, pmState(h, t))

				msg := boMessage(t, h)
				assert.Contains(t, msg, sentence[kind])
				assert.Contains(t, msg, "tap Correct")
				assert.Contains(t, msg, "kept in the match's history")
			})
		}
	}
}

// Before anything is built on it, the court's result replaces the ordinary one
// as FR8 says (a Swiss round-1 match before round 2 is paired).
func TestBuiltOnHold_NothingBuiltYetApplies(t *testing.T) {
	for _, kind := range builtOnKinds {
		t.Run(string(kind), func(t *testing.T) {
			h := boHome(t, kind, false)
			require.Equal(t, "applied", pmOrdFin(mmT1, wrTeamA, []string{"M"}, nil).run(h))
			assert.Equal(t, "applied", pmCourtFin(mmT3, wrTeamB, nil, []string{"K"}).run(h))
			assert.Contains(t, pmState(h, t), `w="`+wrTeamB+`"`)
			assert.True(t, pmCourtReplaced(t, h))
			assert.Zero(t, boHeldLines(t, h))
			assert.Empty(t, boMessage(t, h), "nothing is built on the result")
		})
	}
}

// An explicit correction from the court console has seen the stored result:
// it keeps today's rules and applies.
func TestBuiltOnHold_CorrectionFromTheConsoleApplies(t *testing.T) {
	for _, kind := range builtOnKinds {
		t.Run(string(kind), func(t *testing.T) {
			h := boHome(t, kind, false)
			require.Equal(t, "applied", pmOrdFin(mmT1, wrTeamA, []string{"M"}, nil).run(h))
			boBuild(t, h, kind)
			w := mmRunning(h, mmT3, state.GroupPoints, state.GroupResult)
			w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, wrTeamB, nil, []string{"K"}
			w.Basis, w.CourtStanding, w.CorrectionReason = &state.ScoreBasis{}, true, "the court's result"
			assert.Equal(t, "applied", pmWrite(h, w))
			assert.Contains(t, pmState(h, t), `w="`+wrTeamB+`"`)
			assert.Zero(t, boHeldLines(t, h))
		})
	}
}

// The court's decision waits as its finish does, and bars nobody.
func TestBuiltOnHold_CourtDecisionWaits(t *testing.T) {
	h := boHome(t, BuiltOnStandings, false)
	require.Equal(t, "applied", pmOrdFin(mmT1, wrTeamA, []string{"M"}, nil).run(h))
	boBuild(t, h, BuiltOnStandings)
	assert.Equal(t, "superseded:"+state.HeldReasonBuiltOn, pmDecWith(mmT3, "kiken-voluntary", "shiro", true).run(h))
	got := pmState(h, t)
	assert.Contains(t, got, `w="`+wrTeamA+`" d=""`)
	assert.Contains(t, got, "cs=[]", "the held kiken bars nobody")
	assert.Equal(t, 1, boHeldLines(t, h))
}

// The court's engi finish waits as a kendo finish does.
func TestBuiltOnHold_CourtEngiFinishWaits(t *testing.T) {
	h := boHome(t, BuiltOnStandings, true)
	require.Equal(t, "applied", pmEngiFinWith(mmT1, 2, 1, false).run(h))
	boBuild(t, h, BuiltOnStandings)
	assert.Equal(t, "superseded:"+state.HeldReasonBuiltOn, pmEngiFinWith(mmT3, 1, 2, true).run(h))
	m := h.load(t)
	assert.Equal(t, wrTeamA, m.Winner)
	assert.Equal(t, 2, m.FlagsA)
	assert.Equal(t, 1, m.FlagsB)
	assert.Empty(t, pmReplay(h, t))
	assert.Equal(t, 1, boHeldLines(t, h))
}

// A knockout match of a finished competition: the court's result waits, and the
// winner seated in the next round is the stored result's.
func TestBuiltOnHold_FinishedKnockoutWaits(t *testing.T) {
	h := mmIndividual(t, true)
	require.Equal(t, "applied", pmOrdFin(mmT1, wrTeamA, []string{"M"}, nil).run(h))
	boBuild(t, h, BuiltOnFinished)
	assert.Equal(t, "superseded:"+state.HeldReasonBuiltOn, pmCourtFin(mmT3, wrTeamB, nil, []string{"K"}).run(h))
	got := pmState(h, t)
	assert.Contains(t, got, `w="`+wrTeamA+`"`)
	assert.Contains(t, got, `next="`+wrTeamA+`"/`)
	assert.Empty(t, pmReplay(h, t))
	msg := boMessage(t, h)
	assert.Contains(t, msg, "the competition already finished")
	assert.NotContains(t, msg, "reopens", "a finished knockout is not reopened by a correction")
}
