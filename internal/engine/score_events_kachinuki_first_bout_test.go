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

// startKachinuki starts a scheduled match as the court console does: a start
// write (startOnly) stamped at stamp, which keeps the lineups it starts with.
func startKachinuki(t *testing.T, eng *Engine, compID, matchID string, stamp int64) {
	t.Helper()
	_, err := eng.RecordMatchResultWithIneligibility(compID, matchID, &state.MatchResult{
		Status: state.MatchStatusRunning, ModifiedAt: stamp, Changed: []string{state.GroupResult},
	}, ForceOptions{StartOnly: true})
	require.NoError(t, err)
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
				ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled,
			}}))
			startingLineup(t, store, compID, "ta", "Aoki", "m-aoki")
			startingLineup(t, store, compID, "tb", "Fujii", "m-fujii")
			t0 := time.Now().UnixMilli() - 60_000
			startKachinuki(t, eng, compID, "P1-0", t0)
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

// matchLineup saves a team's own lineup for one match, its first fighter named
// by name and member id.
func matchLineup(t *testing.T, store *state.Store, compID, teamID, matchID, name, member string) {
	t.Helper()
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: teamID, MatchID: matchID,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): name},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): member},
	}, 3))
}

// Operator decision K1 (c): bout 1 is whatever the lineup says, and lineups
// are stuck per match. A match that has started keeps the lineup it started
// with as its own, so an edit of the starting lineup renames none of it; an
// edit of the match's OWN lineup renames bout 1 and the stayer on the bout
// after it, whatever the match's status, through the server's own events: the
// points stay on bout 1, the fighter picked on the other side of bout 2
// stands, and a finished match's result, winner and team figures do not move.
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
		{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled, SubResults: rows()},
		{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusCompleted,
			Winner: "Taka", WinnerID: "ta", Decision: "kachinuki-exhaustion", SubResults: rows()},
	}))
	startingLineup(t, store, compID, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, compID, "tb", "Fujii", "m-fujii")
	t0 := time.Now().UnixMilli() - 60_000
	// The match (sent back to the queue with its bouts) starts again: it
	// keeps the lineup it started with as its own, and a strike on bout 2
	// lands.
	startKachinuki(t, eng, compID, "P1-0", t0-1)
	res := postIn(t, eng, compID, "P1-0", domain.ScoreEvent{ID: "s1", Session: "evs", Seq: 1, Stamp: t0, Kind: domain.ScoreIpponAdd, Pos: 2, Side: domain.ScoreSideB, Letter: "D"})
	require.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)
	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.True(t, ownLineup(lineups, "ta", "P1-0") && ownLineup(lineups, "tb", "P1-0"), "the running match keeps the lineup it started with as its own")

	reseat := func(team string) []string {
		var changed []string
		require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
			var err error
			changed, err = eng.ReseatKachinukiOpenersTx(tx, compID, team)
			return err
		}))
		return changed
	}
	byID := func() map[string]state.MatchResult {
		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		out := map[string]state.MatchResult{}
		for _, m := range stored {
			out[m.ID] = m
		}
		return out
	}

	// Stuck per match: the starting lineup edited renames no match that started.
	startingLineup(t, store, compID, "ta", "Baba", "m-baba")
	assert.Empty(t, reseat("ta"))
	assert.Equal(t, "Aoki", byID()["P1-0"].SubResults[0].SideA)
	assert.Equal(t, "Aoki", byID()["P1-1"].SubResults[0].SideA)

	// The running match's own lineup edited: bout 1 and the stayer renamed.
	matchLineup(t, store, compID, "ta", "P1-0", "Baba", "m-baba")
	assert.Equal(t, []string{"P1-0"}, reseat("ta"))
	b1, b2 := byID()["P1-0"].SubResults[0], byID()["P1-0"].SubResults[1]
	assert.Equal(t, []string{"Baba", "m-baba"}, []string{b1.SideA, b1.SideAMemberID}, "bout 1 is the lineup's now")
	assert.Equal(t, []string{"M", "K"}, b1.IpponsA, "its points stay on bout 1")
	assert.Equal(t, []string{"Baba", "m-baba"}, []string{b1.Winner, b1.WinnerMemberID})
	assert.Equal(t, []string{"Baba", "m-baba"}, []string{b2.SideA, b2.SideAMemberID}, "the winner stays on: bout 2's stayer is renamed with bout 1")
	assert.Equal(t, []string{"Goto", "m-goto"}, []string{b2.SideB, b2.SideBMemberID}, "the fighter picked on the other side stands")
	assert.Equal(t, []string{"D"}, b2.IpponsB)
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")

	// The finished match's own lineup edited: renamed too, its result unmoved.
	before := byID()["P1-1"]
	matchLineup(t, store, compID, "ta", "P1-1", "Chiba", "m-chiba")
	assert.Equal(t, []string{"P1-1"}, reseat("ta"))
	after := byID()["P1-1"]
	assert.Equal(t, state.MatchStatusCompleted, after.Status)
	assert.Equal(t, []string{before.Winner, before.WinnerID, before.Decision}, []string{after.Winner, after.WinnerID, after.Decision}, "the result does not move")
	assert.Equal(t, before.TeamResult(), after.TeamResult(), "nor do the team figures")
	assert.Equal(t, []string{"Chiba", "m-chiba"}, []string{after.SubResults[0].SideA, after.SubResults[0].SideAMemberID})
	assert.Equal(t, []string{"M", "K"}, after.SubResults[0].IpponsA, "its points stay on bout 1")
	assert.Equal(t, domain.MatchSideA, state.SubBoutWinnerSide(after.SubResults[0], after.SideA, after.SideB), "bout 1 is still won by the same side")
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-1")

	// Nothing more to seat: a second pass changes nothing.
	assert.Empty(t, reseat(""))
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

// Stage 6 review 2, F1: a pick on bout 2's stayer side stamped before a lineup
// edit renames the stayer, but reaching the server after it, stands: the
// server's event carrying the stayer on names the fighter it replaces, and at
// its point in stamp order the bout holds the pick instead.
func TestScoreEvents_KachinukiPickBeforeRenameArrivingLateStands(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "kfb-late-pick"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning,
	}}))
	startingLineup(t, store, compID, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, compID, "tb", "Fujii", "m-fujii")
	t0 := time.Now().UnixMilli() - 60_000
	ev := func(id, session string, seq, stamp int64, kind domain.ScoreEventKind, pos int, side, letter, name, member string) domain.ScoreEvent {
		return domain.ScoreEvent{ID: id, Session: session, Seq: seq, Stamp: stamp, Kind: kind, Pos: pos, Side: side, Letter: letter, Name: name, MemberID: member}
	}
	_, err := eng.RecordScoreEvents(compID, "P1-0", ScoreEventsRequest{Session: "s1", Events: []domain.ScoreEvent{
		ev("a1", "s1", 1, t0+1, domain.ScoreIpponAdd, 1, "a", "M", "", ""),
		ev("a2", "s1", 2, t0+2, domain.ScoreIpponAdd, 1, "a", "K", "", ""),
		ev("a3", "s1", 3, t0+3, domain.ScoreBoutFighter, 2, "a", "", "Aoki", "m-aoki"),
		ev("a4", "s1", 4, t0+4, domain.ScoreBoutFighter, 2, "b", "", "Goto", "m-goto"),
	}})
	require.NoError(t, err)
	// The organiser renames bout 1 by the match's own lineup; bout 2 still
	// holds Aoki on the server, so the stayer is renamed with it.
	matchLineup(t, store, compID, "ta", "P1-0", "Baba", "m-baba")
	changed, err := eng.ReseatKachinukiOpeners(compID, "ta")
	require.NoError(t, err)
	require.Equal(t, []string{"P1-0"}, changed)
	// A tablet that was offline picked Chiba on bout 2 at t0+10, before the
	// rename; its pick reaches the server now.
	res, err := eng.RecordScoreEvents(compID, "P1-0", ScoreEventsRequest{Session: "s2", Events: []domain.ScoreEvent{
		ev("b1", "s2", 1, t0+10, domain.ScoreBoutFighter, 2, "a", "", "Chiba", "m-chiba"),
	}})
	require.NoError(t, err)
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	b1, b2 := stored[0].SubResults[0], stored[0].SubResults[1]
	assert.Equal(t, "Baba", b1.SideA, "bout 1 is the lineup's")
	assert.Equal(t, []string{"Chiba", "m-chiba"}, []string{b2.SideA, b2.SideAMemberID}, "the pick made before the rename stands")
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")
}

// Stage 6 review 2, F2: a pairing taken off with Remove this bout and then
// added again by hand on the same bout number scores: a tap made after the
// removal makes the row again.
func TestScoreEvents_KachinukiPairingAddedAgainAfterRemoveScores(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "kfb-readd"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "Aoki", SideAMemberID: "m-aoki", SideB: "Fujii", SideBMemberID: "m-fujii",
			IpponsA: []string{"M", "K"}, IpponsB: []string{}, Winner: "Aoki", WinnerMemberID: "m-aoki", Decision: "fought"}},
	}}))
	t0 := time.Now().UnixMilli() - 60_000
	fighter := func(id string, seq, stamp int64, side, name string) domain.ScoreEvent {
		return domain.ScoreEvent{ID: id, Session: "s1", Seq: seq, Stamp: stamp, Kind: domain.ScoreBoutFighter, Pos: 2, Side: side, Name: name}
	}
	_, err := eng.RecordScoreEvents(compID, "P1-0", ScoreEventsRequest{Session: "s1", Events: []domain.ScoreEvent{
		fighter("f1", 1, t0+1, "a", "Aoki"), fighter("f2", 2, t0+2, "b", "Goto"),
	}})
	require.NoError(t, err)
	_, err = eng.RemoveTrailingKachinukiBout(compID, "P1-0")
	require.NoError(t, err)
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored[0].SubResults, 1, "the pairing is removed")
	t1 := max(time.Now().UnixMilli(), stored[0].ModifiedAt+1)
	res, err := eng.RecordScoreEvents(compID, "P1-0", ScoreEventsRequest{Session: "s1", Events: []domain.ScoreEvent{
		fighter("f3", 3, t1, "a", "Aoki"),
		{ID: "i1", Session: "s1", Seq: 4, Stamp: t1 + 1, Kind: domain.ScoreIpponAdd, Pos: 2, Side: "b", Letter: "M"},
	}})
	require.NoError(t, err)
	for _, a := range res.Answers {
		assert.Equal(t, domain.ScoreOutcomeApplied, a.Outcome, "event %s", a.ID)
	}
	stored, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored[0].SubResults, 2, "the pairing added again is made")
	assert.Equal(t, []string{"M"}, stored[0].SubResults[1].IpponsB)
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")
}

// K1 (c) on a knockout: an edit of a finished match's own lineup renames its
// bout 1 even after the next round was fought, and nothing downstream moves
// (the next match keeps its result and its seats).
func TestScoreEvents_KachinukiFinishedKnockoutMatchRenamedAfterNextRound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "kfb-ko"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3, Status: state.CompStatusKnockout}))
	won := []state.SubMatchResult{{Position: 1, SideA: "Aoki", SideAMemberID: "m-aoki", SideB: "Fujii", SideBMemberID: "m-fujii",
		IpponsA: []string{"M", "K"}, IpponsB: []string{}, Winner: "Aoki", WinnerMemberID: "m-aoki", Decision: "fought"}}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r1-m0", SideA: "Taka", SideAID: "ta", SideB: "Washi", SideBID: "tb", Status: state.MatchStatusCompleted,
			Winner: "Taka", WinnerID: "ta", Decision: "kachinuki-exhaustion", MatchNumber: 1, SubResults: state.CloneSubResults(won)},
			{ID: "r1-m1", SideA: "Kuma", SideAID: "tk", SideB: "Tora", SideBID: "tt", Status: state.MatchStatusCompleted,
				Winner: "Kuma", WinnerID: "tk", Decision: "kachinuki-exhaustion", MatchNumber: 2}},
		{{ID: "r2-m0", SideA: "Taka", SideAID: "ta", SideB: "Kuma", SideBID: "tk", Status: state.MatchStatusCompleted,
			Winner: "Kuma", WinnerID: "tk", Decision: "kachinuki-exhaustion", MatchNumber: 3}},
	}}))
	matchLineup(t, store, compID, "ta", "r1-m0", "Baba", "m-baba")
	changed, err := eng.ReseatKachinukiOpeners(compID, "ta")
	require.NoError(t, err)
	assert.Equal(t, []string{"r1-m0"}, changed)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := b.Rounds[0][0]
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, []string{"Taka", "ta"}, []string{m.Winner, m.WinnerID}, "the result does not move")
	assert.Equal(t, []string{"Baba", "m-baba"}, []string{m.SubResults[0].SideA, m.SubResults[0].SideAMemberID})
	assert.Equal(t, []string{"M", "K"}, m.SubResults[0].IpponsA)
	next := b.Rounds[1][0]
	assert.Equal(t, []string{"Taka", "ta", "Kuma", "tk", "Kuma", string(state.MatchStatusCompleted)},
		[]string{next.SideA, next.SideAID, next.SideB, next.SideBID, next.Winner, string(next.Status)}, "the next round is untouched")
}
