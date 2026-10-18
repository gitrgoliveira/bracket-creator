package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/require"
)

// review6TurnedAwayLog is a running match, `own` taps of device session
// "s1" landed on it, a finish that never saw them (so each is turned away
// once it arrives), and then `others` taps of another session "s2" stamped
// after the finish (each matchClosed).
func review6TurnedAwayLog(t *testing.T, own, others int) []state.ScoreLogLine {
	t.Helper()
	raw := []string{`{"v":1,"baseline":{"id":"m0","door":"test","stamp":100,"values":{"points":{"ipponsA":[],"ipponsB":[],"hansokuA":0,"hansokuB":0},"result":{"status":"running"}},"basis":{"seen":0}}}`}
	for i := 1; i <= own; i++ {
		raw = append(raw, fmt.Sprintf(`{"v":1,"event":{"id":"s1.%d","session":"s1","seq":%d,"stamp":%d,"kind":"foul.add","side":"a"}}`, i, i, 100+i))
	}
	raw = append(raw, `{"v":1,"baseline":{"id":"f","door":"score","stamp":5000,"values":{"result":{"status":"completed","winner":"Aka","winnerId":"pa"}},"basis":{"seen":1},"basisSent":true}}`)
	for i := 1; i <= others; i++ {
		raw = append(raw, fmt.Sprintf(`{"v":1,"event":{"id":"s2.%d","session":"s2","seq":%d,"stamp":%d,"kind":"foul.add","side":"b"}}`, i, i, 6000+i))
	}
	return review5Lines(t, raw...)
}

// Every tap of a device that a later result turned away is listed in the
// match's turnedAway, so the device is told, however many taps OTHER
// devices had turned away after it (stage 6 review 6, finding 3: the list
// kept the newest 64 of the whole log, and another session's later taps
// pushed this device's out). The list is bounded per device session
// (MaxTurnedAwayPerSession) and per match (MaxTurnedAway).
func TestReplayScoring_TurnedAwayListsEveryTapOfADevice(t *testing.T) {
	shape, err := goldenShape("individual", false)
	require.NoError(t, err)
	match := state.MatchResult{ID: "Pool A-0", SideA: "Aka", SideB: "Shiro", SideAID: "pa", SideBID: "pb",
		Status: state.MatchStatusRunning, IpponsA: []string{}, IpponsB: []string{}}

	t.Run("another session's later taps do not push a device's out", func(t *testing.T) {
		r := ReplayScoring(&match, review6TurnedAwayLog(t, 3, state.MaxTurnedAwayPerSession+20), shape)
		for i := 1; i <= 3; i++ {
			require.Equal(t, domain.ScoreOutcomeBeforeBaseline, r.Match.TurnedAway[fmt.Sprintf("s1.%d", i)], "s1.%d", i)
		}
		require.Len(t, r.Match.TurnedAway, 3+state.MaxTurnedAwayPerSession)
	})
	t.Run("a device with more than 64 turned away is told every one", func(t *testing.T) {
		r := ReplayScoring(&match, review6TurnedAwayLog(t, 100, 0), shape)
		require.Len(t, r.Match.TurnedAway, 100)
	})
	t.Run("the list is bounded per session and per match", func(t *testing.T) {
		r := ReplayScoring(&match, review6TurnedAwayLog(t, state.MaxTurnedAwayPerSession+10, 0), shape)
		require.Len(t, r.Match.TurnedAway, state.MaxTurnedAwayPerSession)
		require.NotContains(t, r.Match.TurnedAway, "s1.1", "past a session's bound its oldest go")
		require.Contains(t, r.Match.TurnedAway, fmt.Sprintf("s1.%d", state.MaxTurnedAwayPerSession+10))
	})
}

// A rival result (a later finish from a device that did not see the first
// one) is answered superseded; its answer carries the match's turnedAway as
// the log stands after it, so the rival's device is told of its own tap the
// first result turned away on the answer itself, not only on the push
// (stage 6 review 6, finding 5).
func TestRivalResultAnswerCarriesTurnedAway(t *testing.T) {
	for _, ko := range []bool{false, true} {
		t.Run(fmt.Sprintf("knockout=%v", ko), func(t *testing.T) {
			h := mmIndividual(t, ko)
			tap := domain.ScoreEvent{ID: "r.1", Session: "r", Seq: 1, Stamp: mmT2, Kind: domain.ScoreIpponAdd, Side: "a", Letter: "K"}
			require.Equal(t, "ev:"+domain.ScoreOutcomeApplied, pmEventOf(tap).run(h))
			require.NotContains(t, pmFin(mmT1, wrTeamB, nil, []string{"M"}).run(h), "superseded")

			w := mmRunning(h, mmT3, state.GroupPoints, state.GroupResult)
			w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, wrTeamA, []string{"K"}, nil
			w.Basis = &state.ScoreBasis{Own: []string{tap.ID}}
			w.TurnedAway = map[string]string{"forged": "matchClosed"}
			err := h.write(w)
			require.ErrorIs(t, err, ErrMatchSuperseded)
			away, version := TurnedAwayOf(err)
			require.Equal(t, map[string]string{tap.ID: domain.ScoreOutcomeMatchClosed}, away, "the rival's answer names its own tap, and never what the write carried")
			require.Positive(t, version)
		})
	}
}
