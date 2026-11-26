package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The result the court's result replaced (bc-sseq FR8) is derived by the
// replay onto the stored match, so the device that recorded it learns of the
// change from whatever copy of the match reaches it next: the newest result a
// later-stamped court result displaced without having seen it, named by the
// write id that device sent.

// rrFinish is a finish stamped at, from a device that saw no other result,
// with the court's standing when court is set, tagged with writeID.
func rrFinish(h mmHome, at int64, win string, a, b []string, court bool, writeID string) *state.MatchResult {
	w := mmRunning(h, at, state.GroupPoints, state.GroupResult)
	w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, win, a, b
	w.Basis, w.CourtStanding, w.WriteID = &state.ScoreBasis{}, court, writeID
	return w
}

// rrStored reads the match from a store opened afresh on the data folder, so
// the value is the one the file holds (pool-matches.csv or bracket.json).
func rrStored(t *testing.T, h mmHome, knockout bool) *state.ReplacedResult {
	t.Helper()
	fresh, err := state.NewStore(h.dir)
	require.NoError(t, err)
	if knockout {
		b, err := fresh.LoadBracket(h.compID)
		require.NoError(t, err)
		return b.Rounds[0][0].ReplacedResult
	}
	ms, err := fresh.LoadPoolMatches(h.compID)
	require.NoError(t, err)
	return ms[0].ReplacedResult
}

func TestReplacedResult_NamesTheOrdinaryResultTheCourtReplaced(t *testing.T) {
	M, K := []string{"M"}, []string{"K"}
	want := &state.ReplacedResult{WriteID: "w-ordinary", Stamp: mmT1}
	bothBranches(t, func(t *testing.T, knockout bool) {
		t.Run("ordinary first", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			require.Equal(t, "applied", pmWrite(h, rrFinish(h, mmT1, wrTeamA, M, nil, false, "w-ordinary")))
			require.Nil(t, h.load(t).ReplacedResult, "nothing replaced before the court's result")
			court := rrFinish(h, mmT3, wrTeamB, nil, K, true, "w-court")
			require.Equal(t, "applied", pmWrite(h, court))
			require.Equal(t, want, h.load(t).ReplacedResult, "the stored match")
			require.Equal(t, want, court.ReplacedResult, "the court's own answer")
			require.Equal(t, want, rrStored(t, h, knockout), "the file")
		})
		// The late result is held (court_result) and its device told on its
		// own answer, which names it as the log stands after the write, as it
		// names the turned-away taps: a held write stores nothing, so the
		// stored match takes it at its next write's replay, like TurnedAway.
		t.Run("court first", func(t *testing.T) {
			h := mmIndividual(t, knockout)
			require.Equal(t, "applied", pmWrite(h, rrFinish(h, mmT3, wrTeamB, nil, K, true, "w-court")))
			late := rrFinish(h, mmT1, wrTeamA, M, nil, false, "w-ordinary")
			_, err := h.eng.RecordMatchResultWithIneligibility(h.compID, h.matchID, late)
			require.Equal(t, "superseded:court_result", pmWriteAnswer(late, err))
			require.Equal(t, want, ReplacedResultOf(err), "the held write's answer")
		})
	})
}

func TestReplacedResult_NoneWhenTheCourtReplacedNothing(t *testing.T) {
	M, K := []string{"M"}, []string{"K"}
	cases := []struct {
		name  string
		first func(h mmHome) *state.MatchResult
		then  func(h mmHome) *state.MatchResult
	}{
		{"two ordinary results keep the first (I1)",
			func(h mmHome) *state.MatchResult { return rrFinish(h, mmT1, wrTeamA, M, nil, false, "w-1") },
			func(h mmHome) *state.MatchResult { return rrFinish(h, mmT3, wrTeamB, nil, K, false, "w-2") }},
		{"a court result stamped first is not replaced by a later ordinary one",
			func(h mmHome) *state.MatchResult { return rrFinish(h, mmT1, wrTeamB, nil, K, true, "w-court") },
			func(h mmHome) *state.MatchResult { return rrFinish(h, mmT3, wrTeamA, M, nil, false, "w-ordinary") }},
		{"court after court keeps the first (I1)",
			func(h mmHome) *state.MatchResult { return rrFinish(h, mmT1, wrTeamA, M, nil, true, "w-1") },
			func(h mmHome) *state.MatchResult { return rrFinish(h, mmT3, wrTeamB, nil, K, true, "w-2") }},
		{"an ordinary result sent with no write id cannot be named",
			func(h mmHome) *state.MatchResult { return rrFinish(h, mmT1, wrTeamA, M, nil, false, "") },
			func(h mmHome) *state.MatchResult { return rrFinish(h, mmT3, wrTeamB, nil, K, true, "w-court") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bothBranches(t, func(t *testing.T, knockout bool) {
				h := mmIndividual(t, knockout)
				pmWrite(h, tc.first(h))
				pmWrite(h, tc.then(h))
				require.Nil(t, h.load(t).ReplacedResult)
				require.Nil(t, rrStored(t, h, knockout))
			})
		})
	}
}

// A body cannot forge the field: a write that carries one stores what the
// replay derives, here nothing.
func TestReplacedResult_AWriteCannotSetIt(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h := mmIndividual(t, knockout)
		w := rrFinish(h, mmT1, wrTeamA, []string{"M"}, nil, false, "w-1")
		w.ReplacedResult = &state.ReplacedResult{WriteID: "forged", Stamp: 1}
		require.Equal(t, "applied", pmWrite(h, w))
		require.Nil(t, h.load(t).ReplacedResult)
		require.Nil(t, rrStored(t, h, knockout))
	})
}
