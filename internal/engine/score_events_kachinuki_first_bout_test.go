package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// startingLineup saves a team's starting lineup, its first fighter named by
// name and member id (the lineup's first position is bout 1's fighter, K1).
func startingLineup(t *testing.T, store *state.Store, compID, teamID, name, member string) {
	t.Helper()
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: teamID, Round: 0,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): name},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): member},
	}, 3))
}

// Two devices strike on a kachinuki encounter's first bout at once, on a match
// started from the queue (no bout row yet). No device names a fighter (operator
// decision K1 (c): bout 1's fighters are whatever the lineup says): the first
// strike in stamp order makes the row, the server seats the lineup's fighters
// in the same batch as its own events, and no device's strike is refused
// because of the other device's (stage 6, the journeys' defect: a row write
// stamped after the first device's tap turned that tap away). Pinned in both
// arrival orders.
func TestScoreEvents_KachinukiFirstBoutTwoDevicesBothCount(t *testing.T) {
	for _, order := range []string{"device 1 first", "device 2 first"} {
		t.Run(order, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			const compID = "kfb"
			require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3}))
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
				ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning,
			}}))
			startingLineup(t, store, compID, "ta", "Aoki", "m-aoki")
			startingLineup(t, store, compID, "tb", "Fujii", "m-fujii")
			t0 := time.Now().UnixMilli() - 60_000
			strike := func(id, session string, stamp int64, letter string) domain.ScoreEvent {
				return domain.ScoreEvent{ID: id, Session: session, Seq: 1, Stamp: stamp, Kind: domain.ScoreIpponAdd, Pos: 1, Side: domain.ScoreSideA, Letter: letter}
			}
			d1 := []domain.ScoreEvent{strike("d1-m", "s1", t0+3, "M")}
			d2 := []domain.ScoreEvent{strike("d2-k", "s2", t0+6, "K")}
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
			m := stored[0]
			require.Len(t, m.SubResults, 1, "the first bout's row is made once")
			row := m.SubResults[0]
			assert.Equal(t, 1, row.Position)
			assert.Equal(t, []string{"M", "K"}, row.IpponsA, "both devices' strikes count, in stamp order")
			assert.Equal(t, []string{"Aoki", "m-aoki", "Fujii", "m-fujii"}, []string{row.SideA, row.SideAMemberID, row.SideB, row.SideBMemberID}, "the lineup's fighters")
			assert.Equal(t, "Aoki", row.Winner, "two points win the bout")
			assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")
		})
	}
}

// A later edit of the lineup renames bout 1 on a running match, and the stayer
// on the bout after it: the server's own events, so the points stay on bout 1
// and the fighter picked on the other side of bout 2 stands. A finished match
// keeps the names it was fought under (K1 (c)).
func TestScoreEvents_KachinukiLineupEditRenamesBoutOne(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "kfb-rename"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3}))
	rows := func() []state.SubMatchResult {
		return []state.SubMatchResult{
			{Position: 1, SideA: "Aoki", SideAMemberID: "m-aoki", SideB: "Fujii", SideBMemberID: "m-fujii",
				IpponsA: []string{"M", "K"}, IpponsB: []string{}, Winner: "Aoki", WinnerMemberID: "m-aoki", Decision: "fought"},
			{Position: 2, SideA: "Aoki", SideAMemberID: "m-aoki", SideB: "Goto", SideBMemberID: "m-goto", IpponsA: []string{}, IpponsB: []string{}},
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning, SubResults: rows()},
		{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusCompleted, Winner: "Taka", WinnerID: "ta", SubResults: rows()},
	}))
	startingLineup(t, store, compID, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, compID, "tb", "Fujii", "m-fujii")
	t0 := time.Now().UnixMilli() - 60_000
	// A strike on bout 2 begins the match's log.
	res := postIn(t, eng, compID, "P1-0", domain.ScoreEvent{ID: "s1", Session: "evs", Seq: 1, Stamp: t0, Kind: domain.ScoreIpponAdd, Pos: 2, Side: domain.ScoreSideB, Letter: "D"})
	require.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)

	startingLineup(t, store, compID, "ta", "Baba", "m-baba")
	var changed []string
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		var err error
		changed, err = eng.ReseatKachinukiOpenersTx(tx, compID, "ta")
		return err
	}))
	assert.Equal(t, []string{"P1-0"}, changed, "the running match is renamed; the finished one is not")
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	byID := map[string]state.MatchResult{}
	for _, m := range stored {
		byID[m.ID] = m
	}
	b1, b2 := byID["P1-0"].SubResults[0], byID["P1-0"].SubResults[1]
	assert.Equal(t, []string{"Baba", "m-baba"}, []string{b1.SideA, b1.SideAMemberID}, "bout 1 is the lineup's now")
	assert.Equal(t, []string{"M", "K"}, b1.IpponsA, "its points stay on bout 1")
	assert.Equal(t, []string{"Baba", "m-baba"}, []string{b1.Winner, b1.WinnerMemberID})
	assert.Equal(t, []string{"Baba", "m-baba"}, []string{b2.SideA, b2.SideAMemberID}, "the winner stays on: bout 2's stayer is renamed with bout 1")
	assert.Equal(t, []string{"Goto", "m-goto"}, []string{b2.SideB, b2.SideBMemberID}, "the fighter picked on the other side stands")
	assert.Equal(t, []string{"D"}, b2.IpponsB)
	assert.Equal(t, "Aoki", byID["P1-1"].SubResults[0].SideA, "a finished match keeps the names it was fought under")
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")

	// Nothing more to seat: a second pass changes nothing.
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		var err error
		changed, err = eng.ReseatKachinukiOpenersTx(tx, compID, "")
		return err
	}))
	assert.Empty(t, changed)
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
