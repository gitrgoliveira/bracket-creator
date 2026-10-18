package engine

// The rule for who names bout 1 of a kachinuki encounter (kachinuki_opener.go),
// pinned case by case on the defects three review rounds found. The orderings
// of all of them together are driven by the permutation gate
// (mobileapp/kachinuki_opener_permutations_test.go).

import (
	"errors"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openerRows() []state.SubMatchResult {
	return []state.SubMatchResult{{Position: 1, SideA: "Aoki", SideAMemberID: "m-aoki", SideB: "Fujii", SideBMemberID: "m-fujii",
		IpponsA: []string{"M", "K"}, IpponsB: []string{}, Winner: "Aoki", WinnerMemberID: "m-aoki", Decision: "fought"}}
}

func openerComp(t *testing.T, store *state.Store, compID string, ms ...state.MatchResult) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(compID, ms))
}

func openerMatch(t *testing.T, store *state.Store, compID, matchID string) state.MatchResult {
	t.Helper()
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == matchID {
			return m
		}
	}
	t.Fatalf("no match %s", matchID)
	return state.MatchResult{}
}

func ownLineupsOf(t *testing.T, store *state.Store, compID, matchID string) int {
	t.Helper()
	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	n := 0
	for _, l := range lineups {
		if l.MatchID == matchID {
			n++
		}
	}
	return n
}

// C1: a match finished before this rule (no lineup kept at its start) is not
// renamed by an edit of the starting lineup, nor by a late tap turned away as
// matchClosed, and no write keeps a lineup for it after its start.
func TestKachinukiOpener_FinishedBeforeTheRuleIsNotRenamedByAWrite(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-legacy"
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb",
		Status: state.MatchStatusCompleted, Winner: "Taka", WinnerID: "ta", Decision: "kachinuki-exhaustion", SubResults: openerRows()})
	startingLineup(t, store, c, "ta", "Baba", "m-baba")
	startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
	changed, err := eng.ReseatKachinukiOpeners(c, "ta")
	require.NoError(t, err)
	assert.Empty(t, changed, "an edit of the starting lineup renames no match that started")
	r := postIn(t, eng, c, "P1-0", domain.ScoreEvent{ID: "late", Session: "evs", Seq: 1, Stamp: time.Now().UnixMilli() - 500, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "D"})
	assert.Equal(t, domain.ScoreOutcomeMatchClosed, r.Answers[0].Outcome)
	m := openerMatch(t, store, c, "P1-0")
	assert.Equal(t, []string{"Aoki", "Aoki"}, []string{m.SubResults[0].SideA, m.SubResults[0].Winner}, "bout 1 keeps the fighter it was fought under")
	assert.Equal(t, "Taka", m.Winner)
	assert.Zero(t, ownLineupsOf(t, store, c, "P1-0"), "no lineup is kept for a match after its start")
}

// C1: a match that started with no lineup kept, then finished, is not renamed
// by a starting lineup entered later, nor by a late tap.
func TestKachinukiOpener_NoLineupAtTheStartThenALateTap(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-nolineup"
	rows := openerRows()
	rows[0].IpponsA, rows[0].Winner, rows[0].WinnerMemberID, rows[0].Decision = []string{"M"}, "", "", ""
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning, SubResults: rows})
	t0 := time.Now().UnixMilli() - 5000
	r := postIn(t, eng, c, "P1-0", domain.ScoreEvent{ID: "a1", Session: "evs", Seq: 1, Stamp: t0, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "D"})
	require.Equal(t, domain.ScoreOutcomeApplied, r.Answers[0].Outcome)
	assert.Zero(t, ownLineupsOf(t, store, c, "P1-0"), "a tap keeps no lineup: only the start does")
	m := openerMatch(t, store, c, "P1-0")
	m.Status, m.Winner, m.WinnerID, m.Decision = state.MatchStatusCompleted, "Taka", "ta", "kachinuki-exhaustion"
	require.NoError(t, store.SavePoolMatches(c, []state.MatchResult{m}))
	startingLineup(t, store, c, "ta", "Baba", "m-baba")
	changed, err := eng.ReseatKachinukiOpeners(c, "ta")
	require.NoError(t, err)
	assert.Empty(t, changed)
	postIn(t, eng, c, "P1-0", domain.ScoreEvent{ID: "a2", Session: "evs", Seq: 2, Stamp: t0 + 10, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "K"})
	m = openerMatch(t, store, c, "P1-0")
	assert.Equal(t, "Aoki", m.SubResults[0].SideA)
	assert.Equal(t, "Taka", m.Winner)
	assert.Zero(t, ownLineupsOf(t, store, c, "P1-0"))
}

// C1: the lineup a match started with is kept only by a start that stands. A
// start the merge holds (made before the match was sent back to the queue)
// keeps none, though its transaction commits to keep it in the history; the
// one that stands keeps it.
func TestKachinukiOpener_OnlyAStartThatStandsKeepsTheLineup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-refused"
	t0 := time.Now().UnixMilli() - 10_000
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled,
		ModifiedAt: t0 + 1000, GroupStamps: map[string]int64{state.GroupResult: t0 + 1000}})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
	_, err := eng.RecordMatchResultWithIneligibility(c, "P1-0", &state.MatchResult{Status: state.MatchStatusRunning, ModifiedAt: t0, Changed: []string{state.GroupResult}}, ForceOptions{StartOnly: true})
	require.ErrorIs(t, err, ErrMatchSuperseded, "a start made before the send-back is held")
	assert.Equal(t, state.MatchStatusScheduled, openerMatch(t, store, c, "P1-0").Status)
	assert.Zero(t, ownLineupsOf(t, store, c, "P1-0"), "a refused start keeps no lineup")

	startKachinuki(t, eng, c, "P1-0", t0+2000)
	assert.Equal(t, 2, ownLineupsOf(t, store, c, "P1-0"), "the start that stands keeps both sides' lineups")
}

// S1: the lineup a match started with is never carried on. An edit of the
// starting lineup reaches the team's later match that has not started, as it
// did before the rule, and does not rename the match that started.
func TestKachinukiOpener_TheLineupAMatchStartedWithIsNotCarried(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-carry"
	openerComp(t, store, c,
		state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled},
		state.MatchResult{ID: "P1-1", SideA: "Taka", SideB: "Kuma", SideAID: "ta", SideBID: "tk", Status: state.MatchStatusScheduled})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
	startKachinuki(t, eng, c, "P1-0", time.Now().UnixMilli()-2000)
	postIn(t, eng, c, "P1-0", domain.ScoreEvent{ID: "x1", Session: "evs", Seq: 1, Stamp: time.Now().UnixMilli() - 1000, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "D"})
	assert.Equal(t, "Aoki", openerMatch(t, store, c, "P1-0").SubResults[0].SideA, "bout 1 is the lineup's the match started with")

	startingLineup(t, store, c, "ta", "Baba", "m-baba")
	changed, err := eng.ReseatKachinukiOpeners(c, "ta")
	require.NoError(t, err)
	assert.Empty(t, changed)
	assert.Equal(t, "Aoki", openerMatch(t, store, c, "P1-0").SubResults[0].SideA, "the started match keeps the lineup it started with")
	in, err := eng.LineupInForce(c, "ta", "P1-1")
	require.NoError(t, err)
	assert.Equal(t, "Baba", in.Lineup.Positions[domain.PositionNumbered(1)], "the later match takes the edit of the starting lineup")
	assert.Equal(t, LineupSource{}, in.Source, "from the starting lineup, never from the lineup the earlier match started with")
	own, err := eng.LineupInForce(c, "ta", "P1-0")
	require.NoError(t, err)
	assert.Equal(t, "P1-0", own.Source.MatchID, "the started match shows the lineup it started with as its own")
	assert.True(t, own.Lineup.IsStartPin())
}

// M1: no client can claim the server's session. A request carrying it that the
// server did not make is refused before anything is read or written, on a
// scheduled match and on a finished one alike.
func TestKachinukiOpener_TheServerSessionIsTheServers(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-spoof"
	openerComp(t, store, c,
		state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled},
		state.MatchResult{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusCompleted, Winner: "Taka", WinnerID: "ta", Decision: "kachinuki-exhaustion", SubResults: openerRows()})
	t0 := time.Now().UnixMilli() - 1000
	for _, req := range []ScoreEventsRequest{
		{Session: ServerEventSession, Events: []domain.ScoreEvent{{ID: "i", Session: ServerEventSession, Stamp: t0, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"}}},
		{Session: "dev", Events: []domain.ScoreEvent{{ID: "f", Session: ServerEventSession, Stamp: t0, Kind: domain.ScoreBoutFighter, Pos: 1, Side: "a", Name: "Zed"}}},
	} {
		for _, match := range []string{"P1-0", "P1-1"} {
			_, err := eng.RecordScoreEvents(c, match, req)
			assert.True(t, errors.Is(err, ErrServerEventSession), "%s: %v", match, err)
		}
	}
	assert.Equal(t, state.MatchStatusScheduled, openerMatch(t, store, c, "P1-0").Status)
	assert.Empty(t, openerMatch(t, store, c, "P1-0").SubResults)
	assert.Equal(t, "Aoki", openerMatch(t, store, c, "P1-1").SubResults[0].SideA)
	lines, err := store.LoadScoreLog(c, "P1-1")
	require.NoError(t, err)
	assert.Empty(t, lines)
}

// Rule 7: a strike from a device that never saw Remove this bout, stamped
// after it, would make a bout with no fighters: it is turned away (noTarget,
// kept in the history) and the bout stays removed.
func TestKachinukiOpener_AStaleStrikeAfterRemoveMakesNoBout(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-remove"
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning, SubResults: openerRows()})
	t0 := time.Now().UnixMilli() - 60_000
	_, err := eng.RecordScoreEvents(c, "P1-0", ScoreEventsRequest{Session: "s1", Events: []domain.ScoreEvent{
		{ID: "f1", Session: "s1", Seq: 1, Stamp: t0 + 1, Kind: domain.ScoreBoutFighter, Pos: 2, Side: "a", Name: "Aoki"},
		{ID: "f2", Session: "s1", Seq: 2, Stamp: t0 + 2, Kind: domain.ScoreBoutFighter, Pos: 2, Side: "b", Name: "Goto"},
	}})
	require.NoError(t, err)
	_, err = eng.RemoveTrailingKachinukiBout(c, "P1-0")
	require.NoError(t, err)
	t1 := max(time.Now().UnixMilli(), openerMatch(t, store, c, "P1-0").ModifiedAt+1)
	r, err := eng.RecordScoreEvents(c, "P1-0", ScoreEventsRequest{Session: "s2", Events: []domain.ScoreEvent{
		{ID: "late", Session: "s2", Seq: 1, Stamp: t1, Kind: domain.ScoreIpponAdd, Pos: 2, Side: "b", Letter: "M"}}})
	require.NoError(t, err)
	assert.Equal(t, domain.ScoreOutcomeNoTarget, r.Answers[0].Outcome)
	assert.Equal(t, EventReasonBoutRemoved, r.Answers[0].Reason, "the device is told why")
	assert.Len(t, openerMatch(t, store, c, "P1-0").SubResults, 1, "the bout stays removed")
	history, err := store.LoadMatchHistory(c, "P1-0")
	require.NoError(t, err)
	kept := false
	for _, h := range history {
		if h.Event != nil && h.Event.ID == "late" {
			kept = true
		}
	}
	assert.True(t, kept, "the strike is kept in the match's history")
}

// M2: a change to the lineup a match started with from, stamped before the
// start, reaches the lineup it started with whenever it arrives; one stamped
// after the start does not, nor does one older than the change already there.
func TestKachinukiOpener_FollowLineupSaveOrdersByStamp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-follow"
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
	const start = int64(1_000_000)
	startKachinuki(t, eng, c, "P1-0", start)
	comp, err := store.LoadCompetition(c)
	require.NoError(t, err)
	p1 := domain.PositionNumbered(1)
	follow := func(name string, stamp int64) []string {
		saved := domain.TeamLineup{TeamID: "ta", Round: 0, Positions: map[domain.Position]string{p1: name},
			MemberIDs: map[domain.Position]string{p1: "m-" + name}, Stamps: map[domain.Position]int64{p1: stamp}}
		var moved []string
		require.NoError(t, store.WithTransaction(c, func(tx state.StoreTx) error {
			require.NoError(t, tx.SetTeamLineup(c, saved, 3))
			var err error
			moved, err = eng.FollowLineupSave(tx, comp, saved, []domain.Position{p1})
			return err
		}))
		return moved
	}
	pinned := func() string {
		in, err := eng.LineupInForce(c, "ta", "P1-0")
		require.NoError(t, err)
		return in.Lineup.Positions[p1]
	}
	assert.Empty(t, follow("Mori", start+10), "made after the start: not the lineup it started with")
	assert.Equal(t, "Aoki", pinned())
	assert.Equal(t, []string{"P1-0"}, follow("Kato", start-10), "made before the start, arriving after it")
	assert.Equal(t, "Kato", pinned())
	assert.Empty(t, follow("Sato", start-20), "older than the change already there")
	assert.Equal(t, "Kato", pinned())
}

// An operator's save of a started match's own lineup replaces the lineup it
// started with; taking it away (Use the previous match's lineup) leaves the
// match with none of its own, and bout 1 keeps the fighter it holds.
func TestKachinukiOpener_RemovingAStartedMatchsLineupRenamesNothing(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-remove-own"
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
	startKachinuki(t, eng, c, "P1-0", time.Now().UnixMilli()-2000)
	postIn(t, eng, c, "P1-0", domain.ScoreEvent{ID: "x1", Session: "evs", Seq: 1, Stamp: time.Now().UnixMilli() - 1000, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"})
	matchLineup(t, store, c, "ta", "P1-0", "Ito", "m-ito")
	_, err := eng.ReseatKachinukiOpeners(c, "ta")
	require.NoError(t, err)
	assert.Equal(t, "Ito", openerMatch(t, store, c, "P1-0").SubResults[0].SideA)
	require.NoError(t, store.DeleteTeamLineupForMatch(c, "ta", "P1-0"))
	changed, err := eng.ReseatKachinukiOpeners(c, "ta")
	require.NoError(t, err)
	assert.Empty(t, changed)
	assert.Equal(t, "Ito", openerMatch(t, store, c, "P1-0").SubResults[0].SideA, "nothing of its own: bout 1 keeps its fighter")
}
