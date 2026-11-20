package engine

// The rule for who names bout 1 of a kachinuki encounter (kachinuki_opener.go),
// pinned case by case on the defects three review rounds found. The orderings
// of all of them together are driven by the permutation gate
// (mobileapp/kachinukigate/kachinuki_opener_permutations_test.go).

import (
	"errors"
	"fmt"
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
// matchClosed, and no write keeps a lineup for it after its start. Its teams
// hold no members, so nothing seats it by number either (bc-kno1: with members
// it opens with member 1, pinned by the member-number tests at the end).
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
// by a starting lineup entered later, nor by a late tap. Its teams hold no
// members, so nothing seats it by number either.
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
		var moved []string
		require.NoError(t, store.WithTransaction(c, func(tx state.StoreTx) error {
			// Landed as the lineup door lands a partial save of the
			// starting lineup (its History kept).
			saved := carryTestStartingSave(t, tx, c, "ta", p1, name, "m-"+name, stamp)
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

// carryTestStartingSave lands a one-position change of a team's starting
// lineup as the lineup door does (ApplyStampedChanges on the stored entry, so
// the value it replaces is kept in History), stores it and returns it.
func carryTestStartingSave(t *testing.T, tx state.StoreTx, c, teamID string, p domain.Position, name, id string, stamp int64) domain.TeamLineup {
	t.Helper()
	lineups, err := tx.LoadTeamLineups(c)
	require.NoError(t, err)
	var base domain.TeamLineup
	for _, l := range lineups {
		if l.TeamID == teamID && l.MatchID == "" && l.Round == 0 {
			base = l
		}
	}
	saved, err := base.ApplyStampedChanges([]domain.Position{p}, map[domain.Position]string{p: name}, map[domain.Position]string{p: id},
		map[domain.Position]int64{p: stamp}, stamp+1_000_000)
	require.NoError(t, err)
	saved.TeamID, saved.Round, saved.MatchID = teamID, 0, ""
	require.NoError(t, tx.SetTeamLineup(c, saved, 3))
	return saved
}

// carryTestMatchSave lands a one-position change of a match's own lineup as
// the lineup door does (LandMatchLineupSave), stores it and returns it.
func carryTestMatchSave(t *testing.T, tx state.StoreTx, comp *state.Competition, teamID, matchID string, p domain.Position, name, id string, stamp int64) domain.TeamLineup {
	t.Helper()
	lineups, err := tx.LoadTeamLineups(comp.ID)
	require.NoError(t, err)
	pool, err := tx.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	br, err := tx.LoadBracket(comp.ID)
	require.NoError(t, err)
	landed, err := LandMatchLineupSave(lineups, pool, br, comp.IsKnockoutEnabled(), comp.TeamSize, teamID, matchID,
		[]domain.Position{p}, map[domain.Position]string{p: name}, map[domain.Position]string{p: id}, map[domain.Position]int64{p: stamp}, stamp+1_000_000)
	require.NoError(t, err)
	require.True(t, landed.Live)
	saved := landed.Lineup
	saved.CompetitionID = comp.ID
	require.NoError(t, tx.SetTeamLineup(comp.ID, saved, comp.TeamSize))
	return saved
}

// "Use the previous match's lineup" on a started match is an edit of its own
// lineup, taken as a snapshot: the lineup in force there at that moment is kept
// as the match's own (a pin), bout 1 takes its names on every device, and a
// later edit of the starting lineup renames nothing there.
func TestKachinukiOpener_UsePreviousOnAStartedMatchIsASnapshot(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-use-previous"
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
	startKachinuki(t, eng, c, "P1-0", time.Now().UnixMilli()-3000)
	postIn(t, eng, c, "P1-0", domain.ScoreEvent{ID: "x1", Session: "evs", Seq: 1, Stamp: time.Now().UnixMilli() - 2000, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"})
	matchLineup(t, store, c, "ta", "P1-0", "Ito", "m-ito")
	_, err := eng.ReseatKachinukiOpeners(c, "ta")
	require.NoError(t, err)
	assert.Equal(t, "Ito", openerMatch(t, store, c, "P1-0").SubResults[0].SideA)

	startingLineup(t, store, c, "ta", "Baba", "m-baba")
	require.NoError(t, eng.UsePreviousLineup(c, "ta", "P1-0"))
	_, err = eng.ReseatKachinukiOpeners(c, "ta")
	require.NoError(t, err)
	m := openerMatch(t, store, c, "P1-0")
	assert.Equal(t, "Baba", m.SubResults[0].SideA, "bout 1 takes the lineup in force at that moment")
	assert.Equal(t, []string{"M"}, m.SubResults[0].IpponsA, "its points stay on bout 1")
	own, err := eng.LineupInForce(c, "ta", "P1-0")
	require.NoError(t, err)
	assert.Equal(t, "P1-0", own.Source.MatchID, "kept as the match's own")
	assert.True(t, own.Lineup.IsStartPin())

	startingLineup(t, store, c, "ta", "Kato", "m-kato")
	changed, err := eng.ReseatKachinukiOpeners(c, "ta")
	require.NoError(t, err)
	assert.Empty(t, changed, "a snapshot: a later edit of the starting lineup renames nothing")
	assert.Equal(t, "Baba", openerMatch(t, store, c, "P1-0").SubResults[0].SideA)

	// On a match that has not started it only takes the lineup away.
	require.NoError(t, store.SavePoolMatches(c, []state.MatchResult{openerMatch(t, store, c, "P1-0"),
		{ID: "P1-1", SideA: "Taka", SideB: "Kuma", SideAID: "ta", SideBID: "tk", Status: state.MatchStatusScheduled}}))
	matchLineup(t, store, c, "ta", "P1-1", "Ito", "m-ito")
	require.NoError(t, eng.UsePreviousLineup(c, "ta", "P1-1"))
	assert.Zero(t, ownLineupsOf(t, store, c, "P1-1"))
}

// The same on a started match whose bout 1 has no row yet: the next strike makes
// the row with the snapshot's fighter, never with nobody.
func TestKachinukiOpener_UsePreviousBeforeTheFirstStrike(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-use-previous-first"
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
	startKachinuki(t, eng, c, "P1-0", time.Now().UnixMilli()-3000)
	require.NoError(t, eng.UsePreviousLineup(c, "ta", "P1-0"))
	r := postIn(t, eng, c, "P1-0", domain.ScoreEvent{ID: "x1", Session: "evs", Seq: 1, Stamp: time.Now().UnixMilli() - 10, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "K"})
	require.Equal(t, domain.ScoreOutcomeApplied, r.Answers[0].Outcome)
	m := openerMatch(t, store, c, "P1-0")
	assert.Equal(t, []string{"Aoki", "Fujii"}, []string{m.SubResults[0].SideA, m.SubResults[0].SideB})
}

// Send back to the queue is ONE behaviour: a scheduled match that holds the
// bouts it was fought on is a started match, so bout 1 follows its own lineup
// or the one it started with, never the carried lineup, and starting it again
// takes no lineup (it is no start). Both for a match started under this rule
// and for one started before it.
func TestKachinukiOpener_SentBackFollowsOnlyItsOwnLineup(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("started before the rule=", legacy), func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			c := fmt.Sprint("op-sendback-", legacy)
			first := state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled}
			if legacy {
				first.Status, first.SubResults = state.MatchStatusRunning, openerRows()
				first.SubResults[0].IpponsA, first.SubResults[0].Winner, first.SubResults[0].WinnerMemberID, first.SubResults[0].Decision = []string{"M"}, "", "", ""
			}
			openerComp(t, store, c, first)
			startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
			startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
			if !legacy {
				startKachinuki(t, eng, c, "P1-0", time.Now().UnixMilli()-5000)
				postIn(t, eng, c, "P1-0", domain.ScoreEvent{ID: "x1", Session: "evs", Seq: 1, Stamp: time.Now().UnixMilli() - 4000, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"})
			}
			pins := ownLineupsOf(t, store, c, "P1-0")
			require.NoError(t, eng.RevertMatchToQueue(c, "P1-0"))
			startingLineup(t, store, c, "ta", "Kato", "m-kato")
			changed, err := eng.ReseatKachinukiOpeners(c, "ta")
			require.NoError(t, err)
			assert.Empty(t, changed, "a match sent back keeps bout 1's fighter")
			assert.Equal(t, "Aoki", openerMatch(t, store, c, "P1-0").SubResults[0].SideA)
			startKachinuki(t, eng, c, "P1-0", time.Now().UnixMilli())
			m := openerMatch(t, store, c, "P1-0")
			assert.Equal(t, state.MatchStatusRunning, m.Status)
			assert.Equal(t, "Aoki", m.SubResults[0].SideA, "starting it again renames nothing")
			assert.Equal(t, []string{"M"}, m.SubResults[0].IpponsA, "its points stay with its fighter")
			assert.Equal(t, pins, ownLineupsOf(t, store, c, "P1-0"), "starting it again takes no lineup")
		})
	}
}

// A pin the store cannot stage fails the write that started the match: a
// started match never silently lacks the lineup it started with.
type failLineupSet struct{ state.StoreTx }

func (failLineupSet) SetTeamLineup(string, domain.TeamLineup, int) error {
	return errors.New("lineups.yaml could not be staged")
}

func TestKachinukiOpener_APinThatCannotBeKeptFailsTheStart(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-pin-fails"
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	err := store.WithTransaction(c, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(failLineupSet{tx}, c, "P1-0",
			&state.MatchResult{Status: state.MatchStatusRunning, ModifiedAt: time.Now().UnixMilli() - 100, Changed: []string{state.GroupResult}}, ForceOptions{StartOnly: true})
		return err
	})
	require.Error(t, err)
	assert.Equal(t, state.MatchStatusScheduled, openerMatch(t, store, c, "P1-0").Status, "the start does not stand without its lineup")
}

// B2: a save of an EARLIER match's own lineup stamped before a later match's
// start reaches the lineup that match started with, whenever it arrives: by
// stamp order the match carried it at its start. One stamped after does not.
func TestKachinukiOpener_AnEarlierMatchsSaveMadeBeforeTheStartReachesIt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-earlier"
	openerComp(t, store, c,
		state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Kuma", SideAID: "ta", SideBID: "tk", Status: state.MatchStatusScheduled},
		state.MatchResult{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	const start = int64(1_000_000)
	startKachinuki(t, eng, c, "P1-1", start)
	comp, err := store.LoadCompetition(c)
	require.NoError(t, err)
	p1 := domain.PositionNumbered(1)
	save := func(name string, stamp int64) []string {
		var moved []string
		require.NoError(t, store.WithTransaction(c, func(tx state.StoreTx) error {
			saved := carryTestMatchSave(t, tx, comp, "ta", "P1-0", p1, name, "m-"+name, stamp)
			var err error
			moved, err = eng.FollowLineupSave(tx, comp, saved, []domain.Position{p1})
			return err
		}))
		return moved
	}
	assert.Empty(t, save("Mori", start+10), "made after the start")
	assert.Equal(t, []string{"P1-1"}, save("Kato", start-10), "made before the start, arriving after it")
	in, err := eng.LineupInForce(c, "ta", "P1-1")
	require.NoError(t, err)
	assert.Equal(t, "Kato", in.Lineup.Positions[p1])
	assert.Equal(t, "P1-0", in.Lineup.StartedFrom, "the pin now names the lineup the match carried")
}

// numberOrderMatch saves one kachinuki match between two teams the store seeds
// members for by number (numberedTeams), with no lineup saved anywhere, and
// returns the competition, the teams' ids and their members 1 to 3.
func numberOrderMatch(t *testing.T, id string, m state.MatchResult) (*Engine, *state.Store, string, []string, []string) {
	t.Helper()
	eng, store, comp := setupKachinukiComp(t, id, 3, func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	m.ID, m.SideA, m.SideAID, m.SideB, m.SideBID = "Pool A-0", "Ryu", aID, "Tora", bID
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{m}))
	return eng, store, comp.ID, teamMemberIDs(t, store, comp.ID, aID, 3), teamMemberIDs(t, store, comp.ID, bID, 3)
}

// boutOneMembers is the member ids bout 1 of the match holds, side A then B.
func boutOneMembers(t *testing.T, store *state.Store, compID string) []string {
	t.Helper()
	m := openerMatch(t, store, compID, "Pool A-0")
	require.NotEmpty(t, m.SubResults)
	return []string{m.SubResults[0].SideAMemberID, m.SubResults[0].SideBMemberID}
}

// bc-kno1 (operator decision K1 (a)): a competition with no lineup saved at all
// is seated, its teams opening with member 1 (named or not, Q1), and a second
// reseat finds nothing to change and writes nothing, the blank members carrying
// an id and no name included.
func TestKachinukiOpener_NoLineupAnywhereOpensWithMemberOne(t *testing.T) {
	eng, store, c, a, b := numberOrderMatch(t, "op-number-none", state.MatchResult{Status: state.MatchStatusScheduled})
	t0 := time.Now().UnixMilli() - 5000
	startKachinuki(t, eng, c, "Pool A-0", t0)
	r := postIn(t, eng, c, "Pool A-0", domain.ScoreEvent{ID: "n1", Session: "evs", Seq: 1, Stamp: t0 + 10, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"})
	require.Equal(t, domain.ScoreOutcomeApplied, r.Answers[0].Outcome)
	assert.Equal(t, []string{a[0], b[0]}, boutOneMembers(t, store, c), "bout 1 is each team's member 1")
	m := openerMatch(t, store, c, "Pool A-0")
	assert.Equal(t, []string{"M"}, m.SubResults[0].IpponsA, "the strike stays on bout 1")

	lines, err := store.LoadScoreLog(c, "Pool A-0")
	require.NoError(t, err)
	changed, err := eng.ReseatKachinukiOpeners(c, "")
	require.NoError(t, err)
	assert.Empty(t, changed, "a reseat that finds bout 1 seated changes nothing")
	after, err := store.LoadScoreLog(c, "Pool A-0")
	require.NoError(t, err)
	assert.Len(t, after, len(lines), "and writes nothing")
}

// bc-kno1 with K1 (c): a match that started with nothing of its own (before the
// pin rule) opens by member number over an EMPTY lineup, never the lineup the
// team carries, so the opener and the advance's queue agree.
func TestKachinukiOpener_AStartedMatchWithNothingOfItsOwnOpensByNumber(t *testing.T) {
	eng, store, c, a, b := numberOrderMatch(t, "op-number-started", state.MatchResult{Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "Aoki", SideB: "Fujii", IpponsA: []string{"M"}, IpponsB: []string{}}}})
	m := openerMatch(t, store, c, "Pool A-0")
	startingLineup(t, store, c, m.SideAID, "", a[2])
	changed, err := eng.ReseatKachinukiOpeners(c, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"Pool A-0"}, changed)
	assert.Equal(t, []string{a[0], b[0]}, boutOneMembers(t, store, c), "member 1 of each team, not the carried lineup's member 3")
	m = openerMatch(t, store, c, "Pool A-0")
	assert.Equal(t, []string{"", ""}, []string{m.SubResults[0].SideA, m.SubResults[0].SideB}, "the blank members carry no name")
	assert.Equal(t, []string{"M"}, m.SubResults[0].IpponsA, "the points stay on bout 1")

	q := storedKachinukiQueues(t, eng, c, "Pool A-0")
	assert.Equal(t, a, queueMemberIDs(q.queueA), "the queue opens with the same member")
	changed, err = eng.ReseatKachinukiOpeners(c, "")
	require.NoError(t, err)
	assert.Empty(t, changed)
}

// K1 (a): a pick on bout 1 is a save of the match's own lineup at its first
// place, so it survives every reseat, both for a picked member and for a name
// typed with no member id.
func TestKachinukiOpener_ABoutOnePickSavedToTheLineupSurvivesAReseat(t *testing.T) {
	eng, store, c, a, b := numberOrderMatch(t, "op-number-pick", state.MatchResult{Status: state.MatchStatusScheduled})
	t0 := time.Now().UnixMilli() - 5000
	startKachinuki(t, eng, c, "Pool A-0", t0)
	postIn(t, eng, c, "Pool A-0", domain.ScoreEvent{ID: "p1", Session: "evs", Seq: 1, Stamp: t0 + 10, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "K"})
	require.Equal(t, []string{a[0], b[0]}, boutOneMembers(t, store, c))
	aID := openerMatch(t, store, c, "Pool A-0").SideAID

	matchLineup(t, store, c, aID, "Pool A-0", "", a[2])
	_, err := eng.ReseatKachinukiOpeners(c, aID)
	require.NoError(t, err)
	assert.Equal(t, []string{a[2], b[0]}, boutOneMembers(t, store, c), "the picked member fights bout 1")
	changed, err := eng.ReseatKachinukiOpeners(c, "")
	require.NoError(t, err)
	assert.Empty(t, changed, "and stays there")

	matchLineup(t, store, c, aID, "Pool A-0", "Hoshi", "")
	_, err = eng.ReseatKachinukiOpeners(c, aID)
	require.NoError(t, err)
	m := openerMatch(t, store, c, "Pool A-0")
	assert.Equal(t, []string{"Hoshi", ""}, []string{m.SubResults[0].SideA, m.SubResults[0].SideAMemberID}, "a typed name with no member fights bout 1")
	changed, err = eng.ReseatKachinukiOpeners(c, "")
	require.NoError(t, err)
	assert.Empty(t, changed, "and is not renamed back to a member by number")
	assert.Equal(t, []string{"K"}, openerMatch(t, store, c, "Pool A-0").SubResults[0].IpponsB)
}

// Q4 (b): bout 1 of a FINISHED match fought with no lineup is corrected by a
// save of the match's own lineup, its points, winner and result staying put.
func TestKachinukiOpener_AFinishedNoLineupMatchIsCorrectedByItsOwnLineup(t *testing.T) {
	eng, store, c, a, b := numberOrderMatch(t, "op-number-finished", state.MatchResult{})
	m := openerMatch(t, store, c, "Pool A-0")
	m.Status, m.Winner, m.WinnerID, m.Decision = state.MatchStatusCompleted, "Tora", m.SideBID, "kachinuki-exhaustion"
	m.SubResults = []state.SubMatchResult{{Position: 1, SideAMemberID: a[0], SideBMemberID: b[0], IpponsA: []string{}, IpponsB: []string{"M", "K"},
		Winner: "Tora", WinnerMemberID: b[0], Decision: "fought"}}
	require.NoError(t, store.SavePoolMatches(c, []state.MatchResult{m}))
	changed, err := eng.ReseatKachinukiOpeners(c, "")
	require.NoError(t, err)
	require.Empty(t, changed, "bout 1 already holds the members by number")

	matchLineup(t, store, c, m.SideAID, "Pool A-0", "", a[1])
	changed, err = eng.ReseatKachinukiOpeners(c, m.SideAID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Pool A-0"}, changed)
	got := openerMatch(t, store, c, "Pool A-0")
	assert.Equal(t, []string{a[1], b[0]}, []string{got.SubResults[0].SideAMemberID, got.SubResults[0].SideBMemberID}, "bout 1 is renamed")
	assert.Equal(t, []string{"M", "K"}, got.SubResults[0].IpponsB, "its points stay")
	assert.Equal(t, b[0], got.SubResults[0].WinnerMemberID)
	assert.Equal(t, state.MatchStatusCompleted, got.Status)
	assert.Equal(t, "Tora", got.Winner)
	assert.Equal(t, "kachinuki-exhaustion", got.Decision)
}

// Q5 (a): team-members.yaml that cannot be read seats nobody by number. The tap
// still lands, bout 1 keeps what it holds, and the ERROR names the file.
func TestKachinukiOpener_UnreadableMembersSeatNobodyAndTheWriteStands(t *testing.T) {
	eng, store, c, _, _ := numberOrderMatch(t, "op-number-unread", state.MatchResult{Status: state.MatchStatusScheduled})
	t0 := time.Now().UnixMilli() - 5000
	startKachinuki(t, eng, c, "Pool A-0", t0)
	makeTeamMembersUnreadable(t, store, c)
	var r *ScoreEventsResult
	logs := captureLog(t, func() {
		r = postIn(t, eng, c, "Pool A-0", domain.ScoreEvent{ID: "u1", Session: "evs", Seq: 1, Stamp: t0 + 10, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"})
	})
	require.Equal(t, domain.ScoreOutcomeApplied, r.Answers[0].Outcome, "the write goes on")
	assert.Equal(t, []string{"", ""}, boutOneMembers(t, store, c), "nobody is seated by number")
	assert.Equal(t, []string{"M"}, openerMatch(t, store, c, "Pool A-0").SubResults[0].IpponsA)
	assert.Contains(t, logs, "engine: ERROR: engine.kachinukiOpener compId="+c+": team-members.yaml load error:")
}

// A write whose bout 1 the seat then overrode is answered as recorded when it is
// sent again with its writeId (the outbox's resend after a lost answer): nothing
// is written, no history line is added, and the answer is the stored match, the
// seat's names on bout 1 and not the payload's.
func TestKachinukiOpener_AResendByWriteIDOverASeatedBoutOneIsRecorded(t *testing.T) {
	eng, store, c, a, b := numberOrderMatch(t, "op-resend-seated", state.MatchResult{Status: state.MatchStatusScheduled})
	at := time.Now().UnixMilli() - 5000
	write := func() *state.MatchResult {
		m := openerMatch(t, store, c, "Pool A-0")
		return &state.MatchResult{
			ID: "Pool A-0", SideA: m.SideA, SideAID: m.SideAID, SideB: m.SideB, SideBID: m.SideBID,
			Status: state.MatchStatusRunning, ModifiedAt: at, WriteID: "w-seated",
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "Aoki", SideB: "Fujii", IpponsA: []string{"M"}, IpponsB: []string{}}},
		}
	}
	first := write()
	_, err := eng.RecordMatchResultWithIneligibility(c, "Pool A-0", first)
	require.NoError(t, err)
	require.Equal(t, []string{a[0], b[0]}, boutOneMembers(t, store, c), "the seat overrode the payload's bout 1")
	assert.Equal(t, []string{a[0], b[0]}, []string{first.SubResults[0].SideAMemberID, first.SubResults[0].SideBMemberID}, "and the first answer says so")
	lines, err := store.LoadScoreLog(c, "Pool A-0")
	require.NoError(t, err)
	history := historyLen(t, store, c, "Pool A-0")

	again := write()
	_, err = eng.RecordMatchResultWithIneligibility(c, "Pool A-0", again)
	require.NoError(t, err)
	assert.Equal(t, openerMatch(t, store, c, "Pool A-0").SubResults, again.SubResults, "answered as stored")
	after, err := store.LoadScoreLog(c, "Pool A-0")
	require.NoError(t, err)
	assert.Len(t, after, len(lines), "nothing is written")
	assert.Equal(t, history, historyLen(t, store, c, "Pool A-0"), "and no history line is added")
}
