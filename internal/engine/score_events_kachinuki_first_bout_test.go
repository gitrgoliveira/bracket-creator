package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Two devices strike on a kachinuki encounter's first bout at once, on a match
// started from the queue (no bout row yet). Each device seats the first bout's
// lineup fighters (bout.fighter on pos 1) and strikes, all as events: the
// first event in stamp order makes the row, and no device's tap is refused
// because the other device made the row (stage 6, the journeys' defect: a row
// write stamped after the first device's tap turned that tap away,
// beforeBaseline). Pinned in both arrival orders.
func TestScoreEvents_KachinukiFirstBoutTwoDevicesBothCount(t *testing.T) {
	for _, order := range []string{"device 1 first", "device 2 first"} {
		t.Run(order, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			const compID = "kfb"
			require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3}))
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
				ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning,
			}}))
			t0 := time.Now().UnixMilli() - 60_000
			fighter := func(id, session string, seq, stamp int64, side, name, member string) domain.ScoreEvent {
				return domain.ScoreEvent{ID: id, Session: session, Seq: seq, Stamp: stamp, Kind: domain.ScoreBoutFighter, Pos: 1, Side: side, Name: name, MemberID: member}
			}
			strike := func(id, session string, seq, stamp int64, letter string) domain.ScoreEvent {
				return domain.ScoreEvent{ID: id, Session: session, Seq: seq, Stamp: stamp, Kind: domain.ScoreIpponAdd, Pos: 1, Side: domain.ScoreSideA, Letter: letter}
			}
			// Device 1 struck first (t0+3); device 2 seated the fighters and
			// struck after it (t0+4..t0+6), having seen none of it.
			d1 := []domain.ScoreEvent{
				fighter("d1-a", "s1", 1, t0+1, domain.ScoreSideA, "Aoki", "m-aoki"),
				fighter("d1-b", "s1", 2, t0+2, domain.ScoreSideB, "Fujii", "m-fujii"),
				strike("d1-m", "s1", 3, t0+3, "M"),
			}
			d2 := []domain.ScoreEvent{
				fighter("d2-a", "s2", 1, t0+4, domain.ScoreSideA, "Aoki", "m-aoki"),
				fighter("d2-b", "s2", 2, t0+5, domain.ScoreSideB, "Fujii", "m-fujii"),
				strike("d2-k", "s2", 3, t0+6, "K"),
			}
			post := func(session string, evs []domain.ScoreEvent) *ScoreEventsResult {
				res, err := eng.RecordScoreEvents(compID, "P1-0", ScoreEventsRequest{Session: session, Events: evs})
				require.NoError(t, err)
				return res
			}
			var first, second *ScoreEventsResult
			if order == "device 1 first" {
				first, second = post("s1", d1), post("s2", d2)
			} else {
				second, first = post("s2", d2), post("s1", d1)
			}
			for _, res := range []*ScoreEventsResult{first, second} {
				for _, a := range res.Answers {
					assert.Equal(t, domain.ScoreOutcomeApplied, a.Outcome, "event %s", a.ID)
				}
			}
			stored, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			require.Len(t, stored, 1)
			m := stored[0]
			require.Len(t, m.SubResults, 1, "the first bout's row is made once")
			row := m.SubResults[0]
			assert.Equal(t, 1, row.Position)
			assert.Equal(t, []string{"M", "K"}, row.IpponsA, "both devices' strikes count, in stamp order")
			assert.Equal(t, "Aoki", row.SideA)
			assert.Equal(t, "m-aoki", row.SideAMemberID)
			assert.Equal(t, "Fujii", row.SideB)
			assert.Equal(t, "m-fujii", row.SideBMemberID)
			assert.Equal(t, "Aoki", row.Winner, "two points win the bout")
			assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")
		})
	}
}

// The first bout's fighters are the lineup's: a fighter on the first bout
// seats a side that holds none, or says again the one it holds, and never
// replaces another (a name picked there is a lineup save).
func TestScoreEvents_KachinukiFirstBoutFighterNeverReplacesOne(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "kfb-keep"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "Aoki", SideAMemberID: "m-aoki", IpponsA: []string{}, IpponsB: []string{}}},
	}}))
	t0 := time.Now().UnixMilli() - 60_000
	ev := func(id string, seq, stamp int64, side, name, member string) domain.ScoreEvent {
		return domain.ScoreEvent{ID: id, Session: "s", Seq: seq, Stamp: stamp, Kind: domain.ScoreBoutFighter, Pos: 1, Side: side, Name: name, MemberID: member}
	}
	res, err := eng.RecordScoreEvents(compID, "P1-0", ScoreEventsRequest{Session: "s", Events: []domain.ScoreEvent{
		ev("same", 1, t0+1, domain.ScoreSideA, "Aoki", "m-aoki"),
		ev("other", 2, t0+2, domain.ScoreSideA, "Baba", "m-baba"),
		ev("fill", 3, t0+3, domain.ScoreSideB, "Fujii", "m-fujii"),
	}})
	require.NoError(t, err)
	got := map[string]string{}
	for _, a := range res.Answers {
		got[a.ID] = a.Outcome
	}
	assert.Equal(t, map[string]string{"same": domain.ScoreOutcomeApplied, "other": domain.ScoreOutcomeNotAllowed, "fill": domain.ScoreOutcomeApplied}, got)
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	row := stored[0].SubResults[0]
	assert.Equal(t, []string{"Aoki", "m-aoki", "Fujii", "m-fujii"}, []string{row.SideA, row.SideAMemberID, row.SideB, row.SideBMemberID})
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")
}

// Two devices add the same kachinuki pairing by hand at once and strike on it.
// No device writes the row ahead of its taps: the first event in stamp order
// makes it (createsMissingRow), so neither device's strike is turned away by a
// row the other device wrote after it (stage 6 review 1: a row write stamped
// after the first device's tap answered that tap beforeBaseline). Pinned in
// both arrival orders.
func TestScoreEvents_KachinukiPairingAddedByHandTwoDevicesBothCount(t *testing.T) {
	for _, order := range []string{"device 1 first", "device 2 first"} {
		t.Run(order, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			const compID = "khand"
			require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3}))
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
				ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{{Position: 1, SideA: "Aoki", SideAMemberID: "m-aoki", SideB: "Fujii", SideBMemberID: "m-fujii",
					IpponsA: []string{}, IpponsB: []string{}}},
			}}))
			t0 := time.Now().UnixMilli() - 60_000
			strike := func(id, session string, stamp int64, side, letter string) domain.ScoreEvent {
				return domain.ScoreEvent{ID: id, Session: session, Seq: 1, Stamp: stamp, Kind: domain.ScoreIpponAdd, Pos: 2, Side: side, Letter: letter}
			}
			d1 := []domain.ScoreEvent{strike("d1-m", "s1", t0+1, domain.ScoreSideA, "M")}
			d2 := []domain.ScoreEvent{strike("d2-k", "s2", t0+2, domain.ScoreSideB, "K")}
			post := func(session string, evs []domain.ScoreEvent) *ScoreEventsResult {
				res, err := eng.RecordScoreEvents(compID, "P1-0", ScoreEventsRequest{Session: session, Events: evs})
				require.NoError(t, err)
				return res
			}
			var results []*ScoreEventsResult
			if order == "device 1 first" {
				results = append(results, post("s1", d1), post("s2", d2))
			} else {
				results = append(results, post("s2", d2), post("s1", d1))
			}
			for _, res := range results {
				for _, a := range res.Answers {
					assert.Equal(t, domain.ScoreOutcomeApplied, a.Outcome, "event %s", a.ID)
				}
			}
			stored, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			require.Len(t, stored, 1)
			subs := stored[0].SubResults
			require.Len(t, subs, 2, "the hand-added pairing's row is made once")
			row := subs[1]
			assert.Equal(t, 2, row.Position)
			assert.Equal(t, []string{"M"}, row.IpponsA, "device 1's strike counts")
			assert.Equal(t, []string{"K"}, row.IpponsB, "device 2's strike counts")
			assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")
		})
	}
}
