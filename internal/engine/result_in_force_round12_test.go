package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-evts stage 2b review round 12: a repair line and a drift line that carry
// the result group, judged against the first recorded result (I1).
//
// A repair line (driftOf, DoorRepair) fills ids a load-time repair added to a
// result the log already holds. It is stamped at the stored match's group
// stamps, and it carries the WHOLE result group, so when an earlier-stamped
// finish arrives later and becomes the result in force, the repair line, sorted
// after it, put the displaced finish's verdict back while the scoreline stayed
// the first finish's: winner Ryu over A=[] B=[K]. A repair now fills only ids:
// it applies only while it differs from the replayed value at its point in ids
// alone, and is skipped otherwise.
//
// A drift line (DoorDrift) is a real fault: the stored match says what no line
// of the log explains. When it carries the result it is judged by I1 like any
// result: it saw the lines before it (its basis), so a first-by-stamp result
// that reached the log after it is one it did not see, and it is a rival.

const (
	// pmRepair appends the repair line the drift net mints when a load-time
	// repair filled the winner's id (Ryu's) on a finish the log holds.
	pmRepair = "repair"
	// pmPerturb changes the stored match outside the log (a fault the drift
	// net reports): the next write mints a drift line for it.
	pmPerturb = "perturb"
)

// pmRepairLine is that repair line, built from constants (never from the stored
// match, which in some arrival orders holds the other finish): Ryu's verdict
// with Ryu's id, stamped at the finish it repairs.
func pmRepairLine(at int64) pmStep {
	return pmStep{role: pmRepair, run: func(h mmHome) string {
		lines, err := h.store.LoadScoreLog(h.compID, h.matchID)
		if err != nil {
			return "logerr " + err.Error()
		}
		m := state.MatchResult{ID: h.matchID, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusCompleted, Winner: wrTeamA, WinnerID: wrTeamAID,
			GroupStamps: map[string]int64{state.GroupResult: at}}
		b := BaselineFromMatch(&m, logBaselineID(len(lines), DoorRepair), DoorRepair, at, []string{state.GroupResult}, state.ScoreBasis{Seen: len(lines)})
		b.ReceivedAt = serverNowMs()
		if err := h.store.AppendScoreLog(h.compID, h.matchID, state.ScoreLogLine{Baseline: &b}); err != nil {
			return "appenderr " + err.Error()
		}
		return "repair"
	}}
}

// pmPerturbPool rewrites the stored pool match to Tora over A=[] B=[M], a
// result neither finish carries, keeping its group stamps.
func pmPerturbPool() pmStep {
	return pmStep{role: pmPerturb, run: func(h mmHome) string {
		ms, err := h.store.LoadPoolMatches(h.compID)
		if err != nil {
			return "loaderr " + err.Error()
		}
		for i := range ms {
			if ms[i].ID == h.matchID {
				ms[i].Winner, ms[i].WinnerID, ms[i].IpponsA, ms[i].IpponsB = wrTeamB, wrTeamBID, nil, []string{"M"}
			}
		}
		if err := h.store.SavePoolMatches(h.compID, ms); err != nil {
			return "saveerr " + err.Error()
		}
		return "perturb"
	}}
}

// pmRepairCases: F1 (Ryu M-0) at T1, the repair line R of F1's winner id, and
// a device's earlier finish F0 (Tora 0-K) at T1-5s. R is minted after F1
// (causal), in every order of the three; F0 is the first recorded result
// whichever way they arrive, so Tora 0-K stands.
//
// The drift row runs ONE order (F1, the fault, F0), and not every order: a
// drift line is built from the stored match, so what it means depends on when
// the fault happened. A fault made after F0 arrived saw F0 and legitimately
// sets the result; one made before it did not see it, and is a rival of it.
func pmRepairCases() []pmCase {
	M, K := []string{"M"}, []string{"K"}
	t0 := mmT1 - 5_000
	tora := []string{`st=completed`, `w="Tora"`, `A=[] B=[K]`}
	return []pmCase{
		{name: "I1 repair: fin Tora 0-K T1-5s / fin Ryu M-0 T1 / repair of Ryu's id",
			steps:   []pmStep{pmFin(t0, wrTeamB, nil, K), pmFin(mmT1, wrTeamA, M, nil), pmRepairLine(mmT1)},
			causal:  [][2]int{{1, 2}},
			refused: []int{1}, want: tora},
		{name: "I1 drift: fin Tora 0-K T1-5s / fin Ryu M-0 T1 / a fault before F0 arrives", where: "pool", drift: true,
			steps:  []pmStep{pmFin(t0, wrTeamB, nil, K), pmFin(mmT1, wrTeamA, M, nil), pmPerturbPool()},
			causal: [][2]int{{1, 2}, {2, 0}},
			want:   tora},
	}
}
